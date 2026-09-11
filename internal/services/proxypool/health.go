package proxypool

import (
	"context"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

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
	if uerr := s.repo.Put(p.ID, p); uerr != nil {
		return nil, uerr
	}
	return p, err
}

// CheckAll probes every pooled proxy sequentially.
func (s *Service) CheckAll(ctx context.Context) {
	all, err := s.repo.List()
	if err != nil {
		return
	}
	for _, p := range all {
		if ctx.Err() != nil {
			return
		}
		_, _ = s.Check(ctx, p.ID)
	}
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
// Preference order: location match > known-good for this provider > alive > latency.
// Returns nil when no usable proxy exists.
func (s *Service) Select(prefs models.ProxyPreferences, providerType string) *models.Proxy {
	all, err := s.repo.List()
	if err != nil {
		return nil
	}
	type scored struct {
		p     *models.Proxy
		score int
	}
	best := []scored{}
	for _, p := range all {
		if !p.Alive {
			continue
		}
		score := 0
		if prefs.Location != "" && p.Country == prefs.Location {
			score += 100
		}
		if h, ok := p.ProviderHealth[providerType]; ok {
			if !h.OK {
				continue // known-bad for this provider
			}
			score += 10
		}
		best = append(best, scored{p, score})
	}
	if len(best) == 0 {
		return nil
	}
	sort.Slice(best, func(i, j int) bool {
		if best[i].score != best[j].score {
			return best[i].score > best[j].score
		}
		return best[i].p.LatencyMs < best[j].p.LatencyMs
	})
	return best[0].p
}

// SelectManual returns the first alive proxy from the given IDs.
func (s *Service) SelectManual(ids []string, providerType string) *models.Proxy {
	for _, id := range ids {
		p, err := s.repo.Get(id)
		if err != nil || p == nil || !p.Alive {
			continue
		}
		if h, ok := p.ProviderHealth[providerType]; ok && !h.OK {
			continue
		}
		return p
	}
	return nil
}
