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
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
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
	LastIngestAt time.Time      `json:"last_ingest_at,omitempty"`
	LastError    string         `json:"last_error,omitempty"`
}

// Service owns the adapter boundary, persistence and pool lifecycle.
// Proxy health and lifecycle remain in the proxypool library. Provider
// request routing never consults this service: plugins select proxies
// through Query and assign proxy_url per request.
type Service struct {
	pool     *proxypoollib.ProxyPool
	cache    *dbCache
	src      *pluginSource
	log      *slog.Logger
	reporter *slogReporter
	db       *db.DB
	pools    *repository.Repository[models.CustomProxyPool]

	mu      sync.Mutex
	rootCtx context.Context
	running bool

	keys  func() ([]string, error)
	fetch func(context.Context, string) ([]Candidate, error)

	sourceMu sync.Mutex
	sources  map[string]SourceInfo
}

// New constructs the library-backed pool and restores its persistent cache.
func New(database *db.DB) (*Service, error) {
	cache, err := newDBCache(database)
	if err != nil {
		return nil, err
	}
	s := &Service{
		cache:   cache,
		db:      database,
		pools:   repository.New[models.CustomProxyPool](database, db.BucketCustomPools, "custom proxy pool"),
		log:     slog.Default(),
		sources: map[string]SourceInfo{},
	}
	s.pool = proxypoollib.NewPool()
	s.pool.SetLimits(10, 50, 500)
	s.reporter = newSlogReporter(s.log)
	s.pool.RegisterReporter(s.reporter)
	s.pool.RegisterCacheSource(cache)
	s.src = &pluginSource{service: s}
	s.pool.RegisterProxySource(s.src)
	return s, nil
}

// SetLogger installs structured service logging.
func (s *Service) SetLogger(logger *slog.Logger) {
	if logger == nil {
		return
	}
	s.mu.Lock()
	s.log = logger
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
	if !running {
		return false
	}
	s.pool.RequestIngest()
	return true
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
		return
	}
	s.rootCtx = ctx
	s.running = true
	s.mu.Unlock()

	s.pool.Start(ctx)
	go s.flushLoop(ctx)
}

// flushLoop persists buffered cache changes and stops the library pool on shutdown.
func (s *Service) flushLoop(ctx context.Context) {
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
			return
		}
	}
}

func (s *Service) flushCache() {
	if err := s.cache.Flush(); err != nil {
		s.cache.remember(err)
	}
	s.mu.Lock()
	logger := s.log
	s.mu.Unlock()
	if err := s.cache.takeError(); err != nil && logger != nil {
		logger.Error("proxy cache persistence failed", "error", err)
	}
}

// Query returns proxy endpoints for plugin selection. Pool "auto" reads
// the library free pool in rank order; empty pool means direct and returns
// no endpoints; any other name reads a custom pool by ID or name. Country
// filters case-insensitively; limit caps the result (<=0 means all).
func (s *Service) Query(pool, country string, limit int) ([]models.ProxyView, error) {
	if err := s.cache.peekError(); err != nil {
		return nil, err
	}
	if pool == "" {
		return []models.ProxyView{}, nil
	}
	if pool == models.DefaultProxyPool {
		return s.queryAuto(country, limit), nil
	}
	return s.queryCustom(pool, country, limit)
}

func (s *Service) queryAuto(country string, limit int) []models.ProxyView {
	infos := s.healthyProxies()
	out := make([]models.ProxyView, 0, len(infos))
	for _, p := range infos {
		if country != "" && !strings.EqualFold(p.Location, country) {
			continue
		}
		out = append(out, models.ProxyView{ID: proxyID(p.URL), URL: p.URL, Country: p.Location, Pool: models.DefaultProxyPool})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Service) queryCustom(pool, country string, limit int) ([]models.ProxyView, error) {
	custom, err := s.findCustomPool(pool)
	if err != nil {
		return nil, err
	}
	out := make([]models.ProxyView, 0, len(custom.Entries))
	for _, e := range custom.Entries {
		if e.URL == "" {
			continue
		}
		if country != "" && !strings.EqualFold(e.Country, country) {
			continue
		}
		out = append(out, models.ProxyView{ID: proxyID(e.URL), URL: e.URL, Country: e.Country, Pool: custom.ID})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// List returns verified live proxies in library rank order.
func (s *Service) List() ([]*Proxy, error) {
	if err := s.cache.peekError(); err != nil {
		return nil, err
	}
	infos := s.healthyProxies()
	out := make([]*Proxy, 0, len(infos))
	for _, p := range infos {
		out = append(out, &Proxy{ID: proxyID(p.URL), URL: p.URL, Location: p.Location, Latency: p.Latency, Score: p.Score, LastChecked: p.LastChecked})
	}
	return out, nil
}

func (s *Service) healthyProxies() []proxypoollib.ProxyInfo {
	return s.pool.ListProxies(proxypoollib.ProxyFilter{})
}

// Get returns a cached proxy by its stable router ID, including dead entries.
func (s *Service) Get(id string) (*Proxy, error) {
	if err := s.cache.peekError(); err != nil {
		return nil, err
	}
	for _, state := range s.cache.All() {
		if proxyID(state.URL) == id {
			return stateView(state), nil
		}
	}
	return nil, fmt.Errorf("proxy %q not found", id)
}

// KnownIDs returns IDs for all cached proxies, including dead entries.
func (s *Service) KnownIDs() ([]string, error) {
	if err := s.cache.peekError(); err != nil {
		return nil, err
	}
	states := s.cache.All()
	ids := make([]string, 0, len(states))
	for _, state := range states {
		ids = append(ids, proxyID(state.URL))
	}
	sort.Strings(ids)
	return ids, nil
}

// MarkDead applies the router failure mapping before updating proxy health.
func (s *Service) MarkDead(url, reason string) bool {
	if s == nil || s.pool == nil || url == "" {
		return false
	}
	switch reason {
	case markDeadEarlyEOFReason:
		return s.pool.Suspect(url, proxypoollib.FailEOF)
	case markDeadConnectionResetReason:
		return s.pool.Suspect(url, proxypoollib.FailEOF)
	case markDeadConnectionRefusedReason:
		return s.pool.Suspect(url, proxypoollib.FailRefused)
	case markDeadDNSResolutionReason, markDeadAddressParseReason, markDeadTLSCertificateReason, markDeadTLSHandshakeReason:
		return s.markDeadPersist(url, reason)
	default:
		return s.markDeadPersist(url, reason)
	}
}

func (s *Service) markDeadPersist(url, reason string) bool {
	return s.pool.MarkDead(url, reason)
}

// RecheckBanned queues manual rechecks for banned proxies matching the filters.
func (s *Service) RecheckBanned(reason, source string) (int, error) {
	if reason != "" {
		fr := proxypoollib.FailReason(reason)
		if !fr.Valid() {
			return 0, ErrInvalidBanReason
		}
		return s.pool.RecheckBanned(proxypoollib.BanFilter{Reason: fr, Source: source}), nil
	}
	return s.pool.RecheckBanned(proxypoollib.BanFilter{Source: source}), nil
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
		LastIngestAt: snapshot.LastIngestAt,
	}
	if err := s.cache.peekError(); err != nil {
		status.LastError = err.Error()
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
	return status, nil
}

// SourceInfos returns last-fetch diagnostics for currently registered feeds.
func (s *Service) SourceInfos() []SourceInfo {
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	out := make([]SourceInfo, 0, len(s.sources))
	for _, info := range s.sources {
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func stateView(state proxypoollib.ProxyState) *Proxy {
	return &Proxy{ID: proxyID(state.URL), URL: state.URL, Location: state.Location, Latency: state.Latency, Score: state.Score, LastChecked: state.LastCheckedAt}
}

func proxyID(proxyURL string) string {
	sum := sha256.Sum256([]byte(proxyURL))
	return "px-" + hex.EncodeToString(sum[:8])
}

func sourceDisplayName(key string) string {
	if idx := strings.LastIndex(key, "/"); idx >= 0 {
		return key[idx+1:]
	}
	return key
}

// candidateURL validates one Lua feed candidate: http/https/socks4/socks5
// endpoints with optional userinfo credentials. Anything else counts as
// unsupported.
func candidateURL(c Candidate) (string, bool) {
	u, err := url.Parse(c.URL)
	if err != nil || u.Hostname() == "" {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks4", "socks4a", "socks5":
		return u.String(), true
	default:
		return "", false
	}
}
