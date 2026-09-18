package proxypool

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// ErrProxyPoolExhausted reports that every pooled proxy failed for one
// request; the wrapped cause is the last attempt's error.
var ErrProxyPoolExhausted = errors.New("proxy pool exhausted")

// checkConcurrency bounds parallel health checks. Each check egresses
// through its own proxy, so per-IP rate limits of the geo endpoint apply
// per proxy, never globally.
const checkConcurrency = 16

// Check probes one proxy through the generic check URL and records the
// outcome. Dead list-sourced proxies are culled immediately; manual entries
// are kept but marked not alive.
func (s *Service) Check(ctx context.Context, id string) (*models.Proxy, error) {
	p, err := s.repo.Get(id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.DialTimeout)
	defer cancel()
	start := time.Now()
	err = probeThrough(ctx, p.URL, s.CheckURL)
	latency := time.Since(start).Milliseconds()

	p.Alive = err == nil
	p.LatencyMs = latency
	p.LastCheckAt = util.Now()
	if err != nil && p.Source != ManualSource {
		_ = s.repo.Delete(id)
		return nil, err
	}
	if err == nil && p.Country == "" {
		// Exit-country detection happens once, when geography is unknown:
		// re-checks verify liveness only and never re-probe geography.
		// A failed probe keeps the empty value; liveness is unaffected.
		if country, derr := DetectExitCountry(context.WithoutCancel(ctx), p.URL); derr == nil && country != "" {
			p.Country = country
		}
	}
	if uerr := s.repo.Put(p.ID, p); uerr != nil {
		return nil, uerr
	}
	return p, err
}

// CheckAll probes every pooled proxy with a bounded worker pool. Geo
// detection rides along inside Check; both run checkConcurrency-wide.
func (s *Service) CheckAll(ctx context.Context) {
	all, err := s.repo.List()
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, checkConcurrency)
	for _, p := range all {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			_, _ = s.Check(ctx, id)
		}(p.ID)
	}
	wg.Wait()
}

// RecordOutcome stores per-provider health after a real routed request.
func (s *Service) RecordOutcome(proxyID, providerType string, ok bool, latencyMs int64) {
	_ = s.repo.Update(proxyID, func(p *models.Proxy) error {
		if p.ProviderHealth == nil {
			p.ProviderHealth = map[string]models.ProxyHealth{}
		}
		p.ProviderHealth[providerType] = models.ProxyHealth{
			OK:        ok,
			LatencyMs: latencyMs,
			CheckedAt: util.Now(),
		}
		return nil
	})
}

// Select returns the best proxy for a provider given plugin preferences.
// Ranking: the freshest known-good proxy for this provider (sticky — repeat
// traffic keeps one working exit instead of re-walking the pool) > location
// match > known-good > latency. A proxy known-bad for this provider is
// demoted, never excluded: it stays a fallback when nothing better is pooled.
func (s *Service) Select(prefs models.ProxyPreferences, providerType string) *models.Proxy {
	return bestScored(s.ranked(prefs, providerType))
}

type scoredProxy struct {
	p     *models.Proxy
	score int
}

func (s *Service) ranked(prefs models.ProxyPreferences, providerType string) []scoredProxy {
	all, err := s.repo.List()
	if err != nil {
		return nil
	}
	stickyID := ""
	var stickyAt time.Time
	for _, p := range all {
		if h, ok := p.ProviderHealth[providerType]; ok && h.OK && h.CheckedAt.After(stickyAt) {
			stickyAt = h.CheckedAt
			stickyID = p.ID
		}
	}
	best := []scoredProxy{}
	for _, p := range all {
		if !p.Alive {
			continue
		}
		matched := prefs.Location == "" || p.Country == prefs.Location
		score := 0
		if p.ID == stickyID {
			score += 500
		}
		if matched && prefs.Location != "" {
			score += 100
		}
		if h, ok := p.ProviderHealth[providerType]; ok {
			if !h.OK {
				score -= 1000
			} else {
				score += 10
			}
		}
		best = append(best, scoredProxy{p, score})
	}
	sort.Slice(best, func(i, j int) bool {
		if best[i].score != best[j].score {
			return best[i].score > best[j].score
		}
		return best[i].p.LatencyMs < best[j].p.LatencyMs
	})
	return best
}

func bestScored(best []scoredProxy) *models.Proxy {
	if len(best) == 0 {
		return nil
	}
	return best[0].p
}

// SelectManual returns the first alive proxy from the given IDs.
// Proxies known-bad for this provider sink to the end of the order
// instead of being skipped: an explicit manual pick is still honored
// when nothing better answers.
func (s *Service) SelectManual(ids []string, providerType string) *models.Proxy {
	var demoted *models.Proxy
	for _, id := range ids {
		p, err := s.repo.Get(id)
		if err != nil || p == nil || !p.Alive {
			continue
		}
		if h, ok := p.ProviderHealth[providerType]; ok && !h.OK {
			if demoted == nil {
				demoted = p
			}
			continue
		}
		return p
	}
	return demoted
}
