// Package proxypool implements the proxy pool service.
//
// Responsibilities:
//   - Storage of manual and list-sourced proxies
//   - Health checks with immediate culling of dead list-sourced entries
//   - Per-provider health memory (a proxy region-locked by one provider
//     may still work for others)
//   - Best-proxy selection given plugin location preferences
package proxypool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// ManualSource marks user-registered proxies.
const ManualSource = "manual"

// ListSource builds the source tag for a list plugin.
func ListSource(typeKey string) string { return "list:" + typeKey }

var supportedProtocols = map[string]bool{"http": true, "https": true, "socks4": true, "socks5": true}

// Service manages the proxy pool.
type Service struct {
	repo *repository.Repository[models.Proxy]
	meta *repository.Repository[sourceFetchMeta]
	// CheckURL is the lightweight endpoint used for generic health checks.
	CheckURL string
	// DialTimeout bounds a single health-check dial.
	DialTimeout time.Duration

	pipeline *pipeline
}

// sourceFetchMeta persists the last fetch outcome per source so the
// dashboard totals survive restarts (the check pipeline itself is RAM-only).
type sourceFetchMeta struct {
	Total       int       `json:"total"`
	LastFetchAt time.Time `json:"last_fetch_at"`
}

// New constructs the proxy pool service. List-sourced DB rows that were
// never verified alive are culled: under the streaming pipeline only
// verified proxies may occupy the bucket.
func New(database *db.DB) *Service {
	s := &Service{
		repo:        repository.New[models.Proxy](database, db.BucketProxies, "proxy"),
		meta:        repository.New[sourceFetchMeta](database, db.BucketProxySourceMeta, "proxy_source_meta"),
		CheckURL:    "https://www.gstatic.com/generate_204",
		DialTimeout: 10 * time.Second,
		pipeline:    newPipeline(),
	}
	unverified, err := s.repo.ListFiltered(func(p *models.Proxy) bool {
		return p.Source != ManualSource && !p.Alive
	})
	if err == nil {
		for _, p := range unverified {
			_ = s.repo.Delete(p.ID)
		}
	}
	return s
}

// StartWorkers launches the streaming check pipeline. Call once at startup;
// the workers stop with ctx.
func (s *Service) StartWorkers(ctx context.Context) {
	s.startWorkers(ctx)
}

// ─────────────────────────────────────────────
// CRUD
// ─────────────────────────────────────────────

// AddManual registers a user-provided proxy URL.
func (s *Service) AddManual(rawURL, country string) (*models.Proxy, error) {
	p, err := parseProxyURL(rawURL, ManualSource)
	if err != nil {
		return nil, err
	}
	p.Country = NormalizeCountryCode(country)
	if existing, err := s.repo.Get(p.ID); err == nil && existing != nil {
		return nil, fmt.Errorf("proxy %s already registered", p.URL)
	}
	if err := s.repo.Put(p.ID, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Delete removes a proxy by ID.
func (s *Service) Delete(id string) error {
	return s.repo.Delete(id)
}

// Get returns one proxy.
func (s *Service) Get(id string) (*models.Proxy, error) {
	return s.repo.Get(id)
}

// List returns all proxies, manual first, then by latency.
func (s *Service) List() ([]*models.Proxy, error) {
	all, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool {
		mi, mj := all[i].Source == ManualSource, all[j].Source == ManualSource
		if mi != mj {
			return mi
		}
		if all[i].Alive != all[j].Alive {
			return all[i].Alive
		}
		return all[i].LatencyMs < all[j].LatencyMs
	})
	return all, nil
}

// proxyID is deterministic: the same URL maps to the same record, so manual
// re-adds dedup and re-checks preserve health memory.
func proxyID(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return "px-" + hex.EncodeToString(sum[:8])
}

func parseProxyURL(rawURL, source string) (*models.Proxy, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url: %w", err)
	}
	if !supportedProtocols[u.Scheme] {
		return nil, fmt.Errorf("unsupported proxy protocol %q (want http, https, socks4, socks5)", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("proxy url requires a host")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return nil, fmt.Errorf("proxy url requires a valid port")
	}
	return &models.Proxy{
		ID:        proxyID(u.String()),
		URL:       u.String(),
		Protocol:  u.Scheme,
		Host:      u.Hostname(),
		Port:      port,
		Source:    source,
		CreatedAt: util.Now(),
	}, nil
}

func candidateToProxy(c models.ProxyCandidate, source string) (*models.Proxy, error) {
	scheme := strings.ToLower(strings.TrimSpace(c.Protocol))
	if !supportedProtocols[scheme] {
		return nil, fmt.Errorf("unsupported protocol %q", c.Protocol)
	}
	if c.Host == "" || c.Port <= 0 {
		return nil, fmt.Errorf("candidate requires host and port")
	}
	rawURL := fmt.Sprintf("%s://%s:%d", scheme, c.Host, c.Port)
	return &models.Proxy{
		ID:       proxyID(rawURL),
		URL:      rawURL,
		Protocol: scheme,
		Host:     c.Host,
		Port:     c.Port,
		Country:  NormalizeCountryCode(c.Country),
		Source:   source,
	}, nil
}
