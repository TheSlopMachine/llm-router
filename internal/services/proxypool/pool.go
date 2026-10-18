// Package proxypool adapts the proxypool library to router policy and storage.
package proxypool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/models"
	proxypoollib "github.com/TheSlopMachine/proxypool"
)

var (
	ErrNoProxies        = errors.New("proxy pool has no usable proxy")
	ErrInvalidBanReason = errors.New("invalid proxy ban reason")
)

const (
	markDeadEarlyEOFReason          = "early_eof"
	markDeadConnectionResetReason   = "connection_reset"
	markDeadConnectionRefusedReason = "connection_refused"
	markDeadUnexpectedEOFReason     = "unexpected_eof"
	markDeadBrokenPipeReason        = "broken_pipe"
	markDeadDNSResolutionReason     = "dns_resolution"
	markDeadAddressParseReason      = "address_parse"
	markDeadTLSCertificateReason    = "tls_certificate_verification"
	markDeadTLSHandshakeReason      = "tls_handshake"
)

// Proxy is the stable router-facing view of a validated library entry.
type Proxy struct {
	ID          string        `json:"id"`
	URL         string        `json:"url"`
	Location    string        `json:"location"`
	Latency     time.Duration `json:"latency"`
	Score       float64       `json:"score"`
	LastChecked time.Time     `json:"last_checked"`
}

// Candidate is one proxy endpoint produced by a Lua proxy-source feed.
type Candidate struct {
	URL     string
	Country string
}

// SourceInfo reports feed diagnostics for registered Lua proxy sources.
type SourceInfo struct {
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	LastFetchAt time.Time `json:"last_fetch_at,omitempty"`
	Total       int       `json:"total"`
	Unsupported int       `json:"unsupported"`
	LastError   string    `json:"last_error,omitempty"`
}

type NetStatus struct {
	State     string    `json:"state"`
	RTTMs     int64     `json:"rtt_ms"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type SourceStat struct {
	Source     string         `json:"source"`
	Alive      int            `json:"alive"`
	Suspect    int            `json:"suspect"`
	Banned     int            `json:"banned"`
	Queued     int            `json:"queued"`
	BanReasons map[string]int `json:"ban_reasons"`
}

type LaneStat struct {
	Lane     string `json:"lane"`
	Inflight int    `json:"inflight"`
	Queued   int    `json:"queued"`
}

// DemandStat reports one registered country demand.
type DemandStat struct {
	Countries       []string  `json:"countries"`
	State           string    `json:"state"`
	Waiters         int       `json:"waiters"`
	Alive           int       `json:"alive"`
	Target          int       `json:"target"`
	Served          int64     `json:"served"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
	ExpiresAt       time.Time `json:"expires_at,omitempty"`
	ReplenishUntil  time.Time `json:"replenish_until,omitempty"`
	LastSatisfiedAt time.Time `json:"last_satisfied_at,omitempty"`
}

type Status struct {
	Mode         string         `json:"mode"`
	Net          NetStatus      `json:"net"`
	Limit        int            `json:"limit"`
	Inflight     int            `json:"inflight"`
	Total        int            `json:"total"`
	Alive        int            `json:"alive"`
	Suspect      int            `json:"suspect"`
	Banned       int            `json:"banned"`
	Queued       int            `json:"queued"`
	Lanes        []LaneStat     `json:"lanes"`
	Sources      []SourceStat   `json:"sources"`
	BanReasons   map[string]int `json:"ban_reasons"`
	Demands      []DemandStat   `json:"demands"`
	LastIngestAt time.Time      `json:"last_ingest_at,omitempty"`
	LastError    string         `json:"last_error,omitempty"`
	// ForcedForeground is true while a manual force speeds up candidate checks.
	ForcedForeground bool `json:"forced_foreground"`
	// Enabled is the proxy subsystem switch; PoolEnabled is the free pool
	// switch. Discovery runs only while both are on.
	Enabled     bool `json:"enabled"`
	PoolEnabled bool `json:"pool_enabled"`
}

// Service owns the adapter boundary, persistence and pool lifecycle.
// Proxy health and lifecycle remain in the proxypool library. Provider
// request routing never consults this service: plugins select proxies
// through Query and assign proxy_url per request.
type Service struct {
	pool     *proxypoollib.ProxyPool
	cache    *gormCache
	source   *pluginSource
	logger   *slog.Logger
	reporter *slogReporter
	database *db.Database

	mu      sync.Mutex
	rootCtx context.Context
	running bool

	keys  func() ([]string, error)
	fetch func(context.Context, string) ([]Candidate, error)

	sourceMu sync.Mutex
	sources  map[string]SourceInfo

	// activityMu serializes starting and stopping the library scheduler.
	activityMu sync.Mutex

	// settingsMu guards the operator switches and the pending import queue.
	settingsMu   sync.RWMutex
	disabled     bool // whole proxy subsystem switched off
	poolDisabled bool // free pool switched off
	maxChecks    int
	imported     []proxypoollib.TaggedURL // imported proxies awaiting the next ingest
}

func (s *Service) currentLogger() *slog.Logger {
	if s == nil {
		return slog.Default()
	}
	s.mu.Lock()
	logger := s.logger
	s.mu.Unlock()
	if logger == nil {
		return slog.Default()
	}
	return logger
}

func redactProxyURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "invalid"
	}
	return parsed.Redacted()
}

// New constructs the library-backed pool and restores its persistent cache
// from SQLite. The built-in free-pool row seeds idempotently into the
// durable database so provider proxy references to "auto" satisfy the
// foreign key; proxy states load from the cache file.
func New(database *db.Database) (*Service, error) {
	if database == nil || database.Handle == nil || database.Cache == nil {
		return nil, fmt.Errorf("proxypool sqlite handle is not configured")
	}
	cache, err := newGormCache(context.Background(), database.Cache)
	if err != nil {
		return nil, err
	}
	if err := EnsureBuiltinFreePool(context.Background(), database); err != nil {
		return nil, fmt.Errorf("seed built-in free pool: %w", err)
	}
	s := &Service{
		cache:    cache,
		database: database,
		logger:   slog.Default(),
		sources:  map[string]SourceInfo{},
	}
	pool, err := proxypoollib.NewPool(cache)
	if err != nil {
		return nil, fmt.Errorf("init proxy pool: %w", err)
	}
	s.pool = pool
	s.pool.SetLimits(limitsForMaxChecks(0))
	if stored, err := loadStoredSettings(context.Background(), database); err != nil {
		s.logger.Warn("Reading proxy settings failed, using defaults", "error", err)
	} else {
		s.disabled = stored.disabled
		s.poolDisabled = stored.poolDisabled
		s.maxChecks = stored.maxChecks
		if stored.maxChecks > 0 {
			s.pool.SetLimits(limitsForMaxChecks(stored.maxChecks))
		}
	}
	s.reporter = newSlogReporter(s.logger)
	s.pool.RegisterReporter(s.reporter)
	s.source = &pluginSource{service: s}
	s.pool.RegisterProxySource(s.source)
	return s, nil
}

// SetLogger installs structured service logging.
func (s *Service) SetLogger(logger *slog.Logger) {
	if logger == nil {
		return
	}
	s.mu.Lock()
	s.logger = logger
	reporter := s.reporter
	s.mu.Unlock()
	if reporter != nil {
		reporter.SetLogger(logger)
	}
}

// SetSourceHandlers connects registered Lua proxy-source feeds.
func (s *Service) SetSourceHandlers(keys func() ([]string, error), fetch func(context.Context, string) ([]Candidate, error)) {
	s.mu.Lock()
	s.keys = keys
	s.fetch = fetch
	s.mu.Unlock()
}

// RequestIngest requests an immediate source ingest when the service is running.
func (s *Service) RequestIngest() bool {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if !running || !s.discoveryEnabled() {
		return false
	}
	s.pool.RequestIngest()
	return true
}

// limitsForMaxChecks maps the operator cap to library limits. Zero keeps the
// adaptive defaults. A positive cap becomes the upper bound of the adaptive
// range; the lower bound and the starting point never exceed it.
func limitsForMaxChecks(maxChecks int) (lowest, initial, highest int) {
	if maxChecks <= 0 {
		return 10, 50, 500
	}
	return min(10, maxChecks), min(50, maxChecks), maxChecks
}

// storedSettings are the persisted proxy operator settings.
type storedSettings struct {
	maxChecks    int
	disabled     bool
	poolDisabled bool
}

// loadStoredSettings reads the persisted proxy settings. A missing settings
// row means defaults: no cap, everything on.
func loadStoredSettings(ctx context.Context, database *db.Database) (storedSettings, error) {
	var row record.InstanceSetting
	result := database.Handle.WithContext(ctx).
		Select("proxy_max_checks", "proxy_disabled", "proxy_pool_disabled").
		Where("id = ?", 1).Limit(1).Find(&row)
	if result.Error != nil {
		return storedSettings{}, fmt.Errorf("read instance settings: %w", db.Translate(result.Error))
	}
	return storedSettings{maxChecks: row.ProxyMaxChecks, disabled: row.ProxyDisabled, poolDisabled: row.ProxyPoolDisabled}, nil
}

// SetMaxChecks applies the operator cap on parallel proxy checks. Zero
// restores the adaptive default range. It does not persist the value.
func (s *Service) SetMaxChecks(maxChecks int) error {
	if maxChecks < 0 || maxChecks > models.MaxProxyChecks {
		return fmt.Errorf("proxy max checks must be between 0 and %d", models.MaxProxyChecks)
	}
	s.settingsMu.Lock()
	s.maxChecks = maxChecks
	s.settingsMu.Unlock()
	s.pool.SetLimits(limitsForMaxChecks(maxChecks))
	s.currentLogger().Info("Proxy max checks applied", "max_checks", maxChecks)
	return nil
}

// ApplyConfiguration applies the proxy fields of a saved router configuration.
// Nil fields keep the current value. Switching discovery on or off happens in
// the background so the caller never waits for in-flight checks to finish.
func (s *Service) ApplyConfiguration(configuration models.RouterConfiguration) error {
	if configuration.ProxyMaxChecks != nil {
		if err := s.SetMaxChecks(*configuration.ProxyMaxChecks); err != nil {
			return err
		}
	}
	changed := false
	s.settingsMu.Lock()
	if configuration.ProxyEnabled != nil && s.disabled == *configuration.ProxyEnabled {
		s.disabled = !*configuration.ProxyEnabled
		changed = true
	}
	if configuration.ProxyPoolEnabled != nil && s.poolDisabled == *configuration.ProxyPoolEnabled {
		s.poolDisabled = !*configuration.ProxyPoolEnabled
		changed = true
	}
	subsystemOn := !s.disabled
	poolOn := !s.poolDisabled
	s.settingsMu.Unlock()
	if changed {
		s.currentLogger().Info("Proxy switches applied", "enabled", subsystemOn, "pool_enabled", poolOn)
		go s.syncScheduler()
	}
	return nil
}

// poolEnabledFlag reports the free pool switch on its own.
func (s *Service) poolEnabledFlag() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return !s.poolDisabled
}

// subsystemEnabled reports the whole-subsystem switch.
func (s *Service) subsystemEnabled() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return !s.disabled
}

// discoveryEnabled reports whether the free pool searches and checks proxies.
func (s *Service) discoveryEnabled() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return !s.disabled && !s.poolDisabled
}

// poolAvailable reports whether requests for the named pool may receive
// proxies. A disabled subsystem or free pool sends the caller direct.
func (s *Service) poolAvailable(pool string) bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	if s.disabled {
		return false
	}
	return pool != models.DefaultProxyPool || !s.poolDisabled
}

// syncScheduler starts or stops the library scheduler to match the switches.
// It is a no-op before Start. Stopping keeps every stored proxy.
func (s *Service) syncScheduler() {
	s.activityMu.Lock()
	defer s.activityMu.Unlock()
	s.mu.Lock()
	running := s.running
	ctx := s.rootCtx
	s.mu.Unlock()
	if !running || ctx == nil {
		return
	}
	if s.discoveryEnabled() {
		s.pool.Start(ctx)
		return
	}
	s.pool.Stop()
	s.flushCache()
}

// ForceForeground makes candidate checks run at foreground speed until the
// candidate queue is drained. It returns the number of queued candidates;
// zero means nothing was started (empty queue or pool not running).
func (s *Service) ForceForeground() int {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if !running || !s.discoveryEnabled() {
		return 0
	}
	return s.pool.ForceForeground()
}

// Start begins the pool scheduler and the persistence flush loop.
// The method is non-blocking and idempotent.
func (s *Service) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		s.currentLogger().Debug("Proxy pool already running")
		return
	}
	s.rootCtx = ctx
	s.running = true
	s.mu.Unlock()

	s.currentLogger().Debug("Starting proxy pool")
	s.activityMu.Lock()
	if s.discoveryEnabled() {
		s.pool.Start(ctx)
	} else {
		s.currentLogger().Info("Proxy discovery is switched off, pool stays idle")
	}
	s.activityMu.Unlock()
	go s.flushLoop(ctx)
}

// flushLoop persists buffered cache changes and stops the library pool on shutdown.
func (s *Service) flushLoop(ctx context.Context) {
	s.currentLogger().Debug("Started proxy cache flush loop")
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.flushCache()
		case <-ctx.Done():
			s.flushCache()
			s.pool.Stop()
			s.mu.Lock()
			s.running = false
			s.rootCtx = nil
			s.mu.Unlock()
			s.currentLogger().Debug("Stopped proxy cache flush loop")
			return
		}
	}
}

func (s *Service) flushCache() {
	if err := s.cache.Flush(); err != nil {
		s.cache.remember(err)
	}
	logger := s.currentLogger()
	if err := s.cache.takeError(); err != nil && logger != nil {
		logger.Error("Proxy cache persistence failed", "error", err)
	}
}

// Query returns proxy endpoints for plugin selection. Pool "auto" reads
// the library free pool in rank order; empty pool means direct and returns
// no endpoints; any other name reads a custom pool by ID or name. Country
// filters case-insensitively; limit caps the result (<=0 means all).
func (s *Service) Query(pool, country string, limit int) ([]models.ProxyView, error) {
	if !s.poolAvailable(pool) {
		s.currentLogger().Debug("Querying proxies", "pool", pool, "reason", "switched off", "count", 0)
		return []models.ProxyView{}, nil
	}
	if err := s.cache.peekError(); err != nil {
		s.currentLogger().Error("Querying proxies failed", "pool", pool, "country", country, "limit", limit, "error", err)
		return nil, err
	}
	if pool == "" {
		s.currentLogger().Debug("Querying proxies", "pool", pool, "country", country, "limit", limit, "count", 0)
		return []models.ProxyView{}, nil
	}
	if pool == models.DefaultProxyPool {
		return s.queryAuto(country, limit), nil
	}
	return s.queryCustom(context.Background(), pool, country, limit)
}

func (s *Service) queryAuto(country string, limit int) []models.ProxyView {
	infos := s.healthyProxies()
	output := make([]models.ProxyView, 0, len(infos))
	matchedRank := -1
	for rank, proxyInfo := range infos {
		if country != "" && !strings.EqualFold(proxyInfo.Location, country) {
			continue
		}
		output = append(output, models.ProxyView{ID: proxyID(proxyInfo.URL), URL: proxyInfo.URL, Country: proxyInfo.Location, Pool: models.DefaultProxyPool})
		matchedRank = rank
		if limit > 0 && len(output) >= limit {
			break
		}
	}
	s.currentLogger().Debug("Querying proxies", "pool", models.DefaultProxyPool, "country", country, "limit", limit, "count", len(output), "rank", matchedRank)
	return output
}

func (s *Service) queryCustom(ctx context.Context, pool, country string, limit int) ([]models.ProxyView, error) {
	custom, err := s.findCustomPool(ctx, pool)
	if err != nil {
		s.currentLogger().Error("Querying proxies failed", "pool", pool, "country", country, "limit", limit, "error", err)
		return nil, err
	}
	output := make([]models.ProxyView, 0, len(custom.Entries))
	for _, entry := range custom.Entries {
		if entry.URL == "" {
			continue
		}
		if country != "" && !strings.EqualFold(entry.Country, country) {
			continue
		}
		output = append(output, models.ProxyView{ID: proxyID(entry.URL), URL: entry.URL, Country: entry.Country, Pool: custom.ID})
		if limit > 0 && len(output) >= limit {
			break
		}
	}
	s.currentLogger().Debug("Querying proxies", "pool", pool, "country", country, "limit", limit, "count", len(output))
	return output, nil
}

// Require serves proxies.require. The free pool waits for a matching live
// proxy up to request.Timeout; an empty pool means direct; a custom pool answers
// from its own entries without waiting.
func (s *Service) Require(ctx context.Context, request models.ProxyRequire) (models.ProxyRequireResult, error) {
	if !s.poolAvailable(request.Pool) {
		s.currentLogger().Debug("Requiring proxies", "pool", request.Pool, "reason", "switched off", "count", 0)
		if request.Pool == "" {
			return models.ProxyRequireResult{Direct: true}, nil
		}
		return models.ProxyRequireResult{Disabled: true}, nil
	}
	if err := s.cache.peekError(); err != nil {
		s.currentLogger().Error("Requiring proxies failed", "pool", request.Pool, "limit", request.Limit, "error", err)
		return models.ProxyRequireResult{}, err
	}
	switch request.Pool {
	case "":
		s.currentLogger().Debug("Requiring proxies", "pool", request.Pool, "limit", request.Limit, "count", 0)
		return models.ProxyRequireResult{Direct: true}, nil
	case models.DefaultProxyPool:
		s.currentLogger().Debug("Requiring proxies", "pool", request.Pool, "limit", request.Limit, "count", len(request.Countries))
		result, err := s.pool.Require(ctx, proxypoollib.RequireOptions{
			Countries: request.Countries,
			Exclude:   request.Exclude,
			Limit:     request.Limit,
			Want:      request.Want,
			Timeout:   request.Timeout,
		})
		if err != nil {
			s.currentLogger().Error("Requiring proxies failed", "pool", request.Pool, "limit", request.Limit, "error", err)
			return models.ProxyRequireResult{}, err
		}
		views := make([]models.ProxyView, 0, len(result.Proxies))
		for _, proxyInfo := range result.Proxies {
			views = append(views, models.ProxyView{ID: proxyID(proxyInfo.URL), URL: proxyInfo.URL, Country: proxyInfo.Location, Pool: models.DefaultProxyPool})
		}
		if result.TimedOut {
			s.currentLogger().Warn("Requiring proxies timed out", "pool", request.Pool, "limit", request.Limit, "count", len(views))
		} else {
			s.currentLogger().Debug("Requiring proxies", "pool", request.Pool, "limit", request.Limit, "count", len(views))
		}
		return models.ProxyRequireResult{Proxies: views, TimedOut: result.TimedOut}, nil
	default:
		views, err := s.requireCustom(ctx, request)
		if err != nil {
			return models.ProxyRequireResult{}, err
		}
		return models.ProxyRequireResult{Proxies: views}, nil
	}
}

// requireCustom filters one custom pool by the OR country list and exclusions.
func (s *Service) requireCustom(ctx context.Context, request models.ProxyRequire) ([]models.ProxyView, error) {
	custom, err := s.findCustomPool(ctx, request.Pool)
	if err != nil {
		s.currentLogger().Error("Requiring custom proxies failed", "pool", request.Pool, "limit", request.Limit, "error", err)
		return nil, err
	}
	excluded := make(map[string]struct{}, len(request.Exclude))
	for _, excludedURL := range request.Exclude {
		excluded[excludedURL] = struct{}{}
	}
	limit := max(request.Limit, 1)
	output := make([]models.ProxyView, 0, min(limit, len(custom.Entries)))
	for _, entry := range custom.Entries {
		if entry.URL == "" {
			continue
		}
		if _, skip := excluded[entry.URL]; skip {
			continue
		}
		if len(request.Countries) > 0 && !countryInList(request.Countries, entry.Country) {
			continue
		}
		output = append(output, models.ProxyView{ID: proxyID(entry.URL), URL: entry.URL, Country: entry.Country, Pool: custom.ID})
		if len(output) >= limit {
			break
		}
	}
	s.currentLogger().Debug("Requiring custom proxies", "pool", request.Pool, "limit", limit, "count", len(output))
	return output, nil
}

func countryInList(list []string, country string) bool {
	for _, candidate := range list {
		if strings.EqualFold(strings.TrimSpace(candidate), country) {
			return true
		}
	}
	return false
}

// List returns verified live proxies in library rank order.
func (s *Service) List() ([]*Proxy, error) {
	if err := s.cache.peekError(); err != nil {
		s.currentLogger().Error("Listing proxies failed", "error", err)
		return nil, err
	}
	infos := s.healthyProxies()
	output := make([]*Proxy, 0, len(infos))
	for _, proxyInfo := range infos {
		output = append(output, &Proxy{ID: proxyID(proxyInfo.URL), URL: proxyInfo.URL, Location: proxyInfo.Location, Latency: proxyInfo.Latency, Score: proxyInfo.Score, LastChecked: proxyInfo.LastChecked})
	}
	s.currentLogger().Debug("Listing proxies", "count", len(output))
	return output, nil
}

func (s *Service) healthyProxies() []proxypoollib.ProxyInfo {
	return s.pool.ListProxies(proxypoollib.ProxyFilter{})
}

// Get returns a cached proxy by its stable router ID, including dead entries.
func (s *Service) Get(id string) (*Proxy, error) {
	if err := s.cache.peekError(); err != nil {
		s.currentLogger().Error("Reading proxy failed", "error", err)
		return nil, err
	}
	for _, state := range s.cache.All() {
		if proxyID(state.URL) == id {
			return stateView(state), nil
		}
	}
	err := fmt.Errorf("proxy %q not found", id)
	s.currentLogger().Debug("Reading proxy failed", "reason", err.Error())
	return nil, err
}

// KnownIDs returns IDs for all cached proxies, including dead entries.
func (s *Service) KnownIDs() ([]string, error) {
	if err := s.cache.peekError(); err != nil {
		s.currentLogger().Error("Listing proxy identifiers failed", "error", err)
		return nil, err
	}
	states := s.cache.All()
	ids := make([]string, 0, len(states))
	for _, state := range states {
		ids = append(ids, proxyID(state.URL))
	}
	sort.Strings(ids)
	s.currentLogger().Debug("Listing proxy identifiers", "count", len(ids))
	return ids, nil
}

// MarkDead applies the router failure mapping before updating proxy health.
func (s *Service) MarkDead(proxyURL, reason string) bool {
	if s == nil || s.pool == nil || proxyURL == "" {
		return false
	}
	display := redactProxyURL(proxyURL)
	var outcome string
	var marked bool
	switch reason {
	case markDeadEarlyEOFReason:
		outcome = "suspect"
		marked = s.pool.Suspect(proxyURL, proxypoollib.FailEOF)
	case markDeadConnectionResetReason, markDeadUnexpectedEOFReason, markDeadBrokenPipeReason:
		outcome = "suspect"
		marked = s.pool.Suspect(proxyURL, proxypoollib.FailEOF)
	case markDeadConnectionRefusedReason:
		outcome = "suspect"
		marked = s.pool.Suspect(proxyURL, proxypoollib.FailRefused)
	case markDeadDNSResolutionReason, markDeadAddressParseReason, markDeadTLSCertificateReason, markDeadTLSHandshakeReason:
		outcome = "dead"
		marked = s.markDeadPersist(proxyURL, reason)
	default:
		outcome = "dead"
		marked = s.markDeadPersist(proxyURL, reason)
	}
	s.currentLogger().Debug("Marking proxy", "proxy_url", display, "reason", reason, "operation", outcome, "count", boolToCount(marked))
	return marked
}

func boolToCount(marked bool) int {
	if marked {
		return 1
	}
	return 0
}

func (s *Service) markDeadPersist(proxyURL, reason string) bool {
	return s.pool.MarkDead(proxyURL, reason)
}

// RecheckBanned queues manual rechecks for banned proxies matching the filters.
func (s *Service) RecheckBanned(reason, source string) (int, error) {
	if !s.discoveryEnabled() {
		return 0, nil
	}
	if reason != "" {
		failReason := proxypoollib.FailReason(reason)
		if !failReason.Valid() {
			s.currentLogger().Error("Rechecking banned proxies failed", "reason", reason, "error", ErrInvalidBanReason)
			return 0, ErrInvalidBanReason
		}
		checked := s.pool.RecheckBanned(proxypoollib.BanFilter{Reason: failReason, Source: source})
		s.currentLogger().Debug("Rechecking banned proxies", "reason", reason, "count", checked)
		return checked, nil
	}
	checked := s.pool.RecheckBanned(proxypoollib.BanFilter{Source: source})
	s.currentLogger().Debug("Rechecking banned proxies", "count", checked)
	return checked, nil
}

// Status returns the structured dashboard snapshot from the library pool.
// Persistence errors remain visible in LastError so the dashboard can still
// render the live scheduler/network state.
func (s *Service) Status() (Status, error) {
	snapshot := s.pool.Snapshot()
	status := Status{
		Mode:         string(snapshot.Mode),
		Net:          NetStatus{State: string(snapshot.Net.State), RTTMs: snapshot.Net.RTT.Milliseconds(), UpdatedAt: snapshot.Net.UpdatedAt},
		Limit:        snapshot.Limit,
		Inflight:     snapshot.Inflight,
		Total:        snapshot.Alive + snapshot.Suspect + snapshot.Banned,
		Alive:        snapshot.Alive,
		Suspect:      snapshot.Suspect,
		Banned:       snapshot.Banned,
		Queued:       snapshot.Queued,
		Lanes:        make([]LaneStat, 0, len(snapshot.Lanes)),
		Sources:      make([]SourceStat, 0, len(snapshot.Sources)),
		BanReasons:   make(map[string]int, len(snapshot.BanReasons)),
		Demands:      make([]DemandStat, 0, len(snapshot.Demands)),
		LastIngestAt: snapshot.LastIngestAt,

		ForcedForeground: snapshot.ForcedForeground,
		Enabled:          s.subsystemEnabled(),
		PoolEnabled:      s.poolEnabledFlag(),
	}
	if err := s.cache.peekError(); err != nil {
		status.LastError = err.Error()
		s.currentLogger().Error("Reading proxy status failed", "count", status.Total, "error", err)
		return status, err
	}
	for _, lane := range snapshot.Lanes {
		status.Lanes = append(status.Lanes, LaneStat{Lane: lane.Lane, Inflight: lane.Inflight, Queued: lane.Queued})
	}
	for _, source := range snapshot.Sources {
		reasons := make(map[string]int, len(source.BanReasons))
		for reason, count := range source.BanReasons {
			reasons[string(reason)] = count
		}
		status.Sources = append(status.Sources, SourceStat{Source: source.Source, Alive: source.Alive, Suspect: source.Suspect, Banned: source.Banned, Queued: source.Queued, BanReasons: reasons})
	}
	for reason, count := range snapshot.BanReasons {
		status.BanReasons[string(reason)] = count
	}
	for _, demand := range snapshot.Demands {
		countries := demand.Countries
		if countries == nil {
			countries = []string{}
		}
		status.Demands = append(status.Demands, DemandStat{
			Countries: countries, State: demand.State, Waiters: demand.Waiters, Alive: demand.Alive, Target: demand.Target, Served: demand.Served,
			CreatedAt: demand.CreatedAt, ExpiresAt: demand.ExpiresAt, ReplenishUntil: demand.ReplenishUntil, LastSatisfiedAt: demand.LastSatisfiedAt,
		})
	}
	s.currentLogger().Debug("Reading proxy status", "count", status.Total, "limit", status.Limit)
	return status, nil
}

// SourceInfos returns last-fetch diagnostics for currently registered feeds.
func (s *Service) SourceInfos() []SourceInfo {
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	output := make([]SourceInfo, 0, len(s.sources))
	for _, sourceInfo := range s.sources {
		output = append(output, sourceInfo)
	}
	sort.Slice(output, func(i, j int) bool { return output[i].Key < output[j].Key })
	return output
}

func stateView(state proxypoollib.ProxyState) *Proxy {
	return &Proxy{ID: proxyID(state.URL), URL: state.URL, Location: state.Location, Latency: state.Latency, Score: state.Score, LastChecked: state.LastCheckedAt}
}

func proxyID(proxyURL string) string {
	sum := sha256.Sum256([]byte(proxyURL))
	return "px-" + hex.EncodeToString(sum[:8])
}

func sourceDisplayName(key string) string {
	if index := strings.LastIndex(key, "/"); index >= 0 {
		return key[index+1:]
	}
	return key
}

// candidateURL validates one Lua feed candidate: http/https/socks4/socks5
// endpoints with optional userinfo credentials. Anything else counts as
// unsupported.
func candidateURL(candidate Candidate) (string, bool) {
	parsedURL, err := url.Parse(candidate.URL)
	if err != nil || parsedURL.Hostname() == "" {
		return "", false
	}
	switch strings.ToLower(parsedURL.Scheme) {
	case "http", "https", "socks4", "socks4a", "socks5":
		return parsedURL.String(), true
	default:
		return "", false
	}
}
