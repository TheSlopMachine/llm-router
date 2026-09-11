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
	// CheckURL is the lightweight endpoint used for generic health checks.
	CheckURL string
	// DialTimeout bounds a single health-check dial.
	DialTimeout time.Duration
}

// New constructs the proxy pool service.
func New(database *db.DB) *Service {
	return &Service{
		repo:        repository.New[models.Proxy](database, db.BucketProxies, "proxy"),
		CheckURL:    "https://www.gstatic.com/generate_204",
		DialTimeout: 10 * time.Second,
	}
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
	p.Country = strings.ToUpper(strings.TrimSpace(country))
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

// SyncFromSource replaces all list-sourced entries of one source with the
// fresh candidates. Health memory survives for proxies present in both sets.
func (s *Service) SyncFromSource(sourceKey string, candidates []models.ProxyCandidate) (int, error) {
	source := ListSource(sourceKey)
	existing, err := s.repo.ListFiltered(func(p *models.Proxy) bool { return p.Source == source })
	if err != nil {
		return 0, err
	}
	byID := make(map[string]*models.Proxy, len(existing))
	for _, p := range existing {
		byID[p.ID] = p
	}
	seen := map[string]bool{}
	added := 0
	for _, c := range candidates {
		p, err := candidateToProxy(c, source)
		if err != nil {
			continue
		}
		seen[p.ID] = true
		if old, ok := byID[p.ID]; ok {
			p.Alive = old.Alive
			p.LatencyMs = old.LatencyMs
			p.LastCheckAt = old.LastCheckAt
			p.ProviderHealth = old.ProviderHealth
			p.CreatedAt = old.CreatedAt
		} else {
			added++
		}
		if err := s.repo.Put(p.ID, p); err != nil {
			return added, err
		}
	}
	for id := range byID {
		if !seen[id] {
			_ = s.repo.Delete(id)
		}
	}
	return added, nil
}

// proxyID is deterministic: the same URL maps to the same record, so manual
// re-adds dedup and list syncs preserve health memory.
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
		Country:  strings.ToUpper(strings.TrimSpace(c.Country)),
		Source:   source,
	}, nil
}
