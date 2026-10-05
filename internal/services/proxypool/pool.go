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
	ErrNoProxies = errors.New("proxy pool has no usable proxy")
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

// Status describes pool activity and the next scheduled refresh.
type Status struct {
	Total           int           `json:"total"`
	Active          int           `json:"active"`
	Refreshing      bool          `json:"refreshing"`
	LastRefreshAt   time.Time     `json:"last_refresh_at,omitempty"`
	LastRefreshTime time.Duration `json:"last_refresh_duration"`
	NextRefreshAt   time.Time     `json:"next_refresh_at,omitempty"`
	RefreshInterval time.Duration `json:"refresh_interval"`
	LastError       string        `json:"last_error,omitempty"`
}

// Service owns the adapter boundary, persistence and refresh schedule.
// Proxy health and lifecycle remain in the proxypool library. Provider
// request routing never consults this service: plugins select proxies
// through Query and assign proxy_url per request.
type Service struct {
	pool  *proxypoollib.ProxyPool
	cache *dbCache
	src   *pluginSource
	log   *slog.Logger
	db    *db.DB
	pools *repository.Repository[models.CustomProxyPool]

	mu           sync.Mutex
	refreshMu    sync.Mutex
	refreshing   bool
	refreshWake  chan struct{}
	changed      chan struct{}
	rootCtx      context.Context
	lastUse      time.Time
	lastRefresh  time.Time
	lastDuration time.Duration
	lastError    string
	nextRefresh  time.Time
	interval     time.Duration

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
		cache:       cache,
		db:          database,
		pools:       repository.New[models.CustomProxyPool](database, db.BucketCustomPools, "custom proxy pool"),
		log:         slog.Default(),
		refreshWake: make(chan struct{}, 1),
		changed:     make(chan struct{}),
		interval:    time.Minute,
		sources:     map[string]SourceInfo{},
	}
	s.pool = proxypoollib.NewPool()
	s.pool.SetConcurrency(100)
	s.pool.RegisterCacheSource(cache)
	s.src = &pluginSource{service: s}
	s.pool.RegisterProxySource(s.src)
	return s, nil
}

// SetLogger installs structured service logging.
func (s *Service) SetLogger(logger *slog.Logger) {
	if logger != nil {
		s.log = logger
	}
}

// SetSourceHandlers connects registered Lua proxy-source feeds.
func (s *Service) SetSourceHandlers(keys func() ([]string, error), fetch func(context.Context, string) ([]Candidate, error)) {
	s.mu.Lock()
	s.keys = keys
	s.fetch = fetch
	s.mu.Unlock()
}

// RequestRefresh coalesces a background refresh request.
func (s *Service) RequestRefresh() bool {
	s.mu.Lock()
	if s.refreshing {
		s.mu.Unlock()
		return false
	}
	s.refreshing = true
	s.notifyLocked()
	s.mu.Unlock()
	go func() {
		started := time.Now()
		s.refreshMu.Lock()
		s.pool.Refresh()
		if ferr := s.cache.Flush(); ferr != nil {
			s.cache.remember(ferr)
		}
		cacheErr := s.cache.takeError()
		s.refreshMu.Unlock()

		s.mu.Lock()
		s.refreshing = false
		s.lastRefresh = started
		s.lastDuration = time.Since(started)
		s.lastError = ""
		if cacheErr != nil {
			s.lastError = cacheErr.Error()
			if s.log != nil {
				s.log.Error("proxy refresh persistence failed", "error", cacheErr)
			}
		}
		s.notifyLocked()
		s.mu.Unlock()
	}()
	return true
}

// Start begins the initial refresh and adaptive refresh schedule.
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	s.rootCtx = ctx
	s.mu.Unlock()
	s.RequestRefresh()
	go s.schedule(ctx)
}

// Touch records active pool use and wakes the scheduler after an idle period.
func (s *Service) Touch() {
	now := time.Now()
	s.mu.Lock()
	idle := s.lastUse.IsZero() || now.Sub(s.lastUse) >= 8*time.Minute
	s.lastUse = now
	s.mu.Unlock()
	if idle {
		select {
		case s.refreshWake <- struct{}{}:
		default:
		}
		s.RequestRefresh()
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
	infos := s.pool.ListProxies(proxypoollib.ProxyFilter{})
	healthy := make([]proxypoollib.ProxyInfo, 0, len(infos))
	for _, info := range infos {
		// New entries are visible to ListProxies before their first check ends.
		if !info.LastChecked.IsZero() {
			healthy = append(healthy, info)
		}
	}
	return healthy
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

// MarkDead excludes the proxy with the given canonical URL until an
// escalating ban expires. Unknown URLs return false. The library owns
// dead status and revival; the mark persists through the bbolt cache.
// Called only from the plugin HTTP client on structural TLS faults:
// plugins never mark proxies directly.
func (s *Service) MarkDead(url, reason string) bool {
	if s == nil || s.pool == nil || url == "" {
		return false
	}
	marked := s.pool.MarkDead(url, reason)
	if !marked {
		return false
	}
	if ferr := s.cache.Flush(); ferr != nil {
		s.cache.remember(ferr)
	}
	if err := s.cache.takeError(); err != nil && s.log != nil {
		s.log.Warn("proxy mark dead persistence failed", "error", err)
	}
	return true
}

// Status returns pool counts and scheduler state.
func (s *Service) Status() (Status, error) {
	if err := s.cache.peekError(); err != nil {
		return Status{}, err
	}
	all := s.cache.All()
	active := len(s.healthyProxies())
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{
		Total:           len(all),
		Active:          active,
		Refreshing:      s.refreshing,
		LastRefreshAt:   s.lastRefresh,
		LastRefreshTime: s.lastDuration,
		NextRefreshAt:   s.nextRefresh,
		RefreshInterval: s.interval,
		LastError:       s.lastError,
	}, nil
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

func (s *Service) notifyLocked() {
	close(s.changed)
	s.changed = make(chan struct{})
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
