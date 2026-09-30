package proxypool

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	proxypoollib "github.com/TheSlopMachine/proxypool"
)

// Rank returns library-ranked proxies allowed by the provider policy.
func (s *Service) Rank(whitelist []string, mode string, ids []string, _ string) ([]Pick, error) {
	if mode == models.ProxyModeDisabled || (mode == models.ProxyModeManual && len(ids) == 0) {
		return nil, nil
	}
	s.Touch()
	if err := s.cache.peekError(); err != nil {
		return nil, err
	}
	infos := s.healthyProxies()
	byID := make(map[string]proxypoollib.ProxyInfo, len(infos))
	allow := make(map[string]bool, len(whitelist))
	for _, location := range whitelist {
		if location = NormalizeCountryCode(location); location != "" {
			allow[location] = true
		}
	}
	for _, info := range infos {
		byID[proxyID(info.URL)] = info
	}
	if mode == models.ProxyModeManual {
		picks := make([]Pick, 0, len(ids))
		for _, id := range ids {
			info, ok := byID[id]
			if !ok {
				continue
			}
			picks = append(picks, Pick{ID: id, URL: info.URL, Location: NormalizeCountryCode(info.Location)})
		}
		if len(picks) == 0 {
			return nil, fmt.Errorf("provider proxy: no usable proxy among %d selected", len(ids))
		}
		return picks, nil
	}
	if mode != models.ProxyModeAuto {
		return nil, fmt.Errorf("provider proxy: unsupported mode %q", mode)
	}
	ordered := make([]proxypoollib.ProxyInfo, 0, len(infos))
	for _, info := range infos {
		if len(allow) > 0 && !allow[NormalizeCountryCode(info.Location)] {
			continue
		}
		ordered = append(ordered, info)
	}
	// The library orders by latency; keep a deterministic URL tie-breaker.
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Latency != ordered[j].Latency {
			return ordered[i].Latency < ordered[j].Latency
		}
		return ordered[i].URL < ordered[j].URL
	})
	picks := make([]Pick, 0, len(ordered))
	for _, info := range ordered {
		picks = append(picks, Pick{ID: proxyID(info.URL), URL: info.URL, Location: NormalizeCountryCode(info.Location)})
	}
	return picks, nil
}

// RankWait waits for an in-progress library refresh when no proxy is ready.
func (s *Service) RankWait(ctx context.Context, whitelist []string, ids []string, provider string) ([]Pick, error) {
	for {
		picks, err := s.Rank(whitelist, models.ProxyModeAuto, ids, provider)
		if err != nil || len(picks) > 0 {
			return picks, err
		}
		s.mu.Lock()
		refreshing := s.refreshing
		changed := s.changed
		s.mu.Unlock()
		if !refreshing {
			return nil, ErrNoProxies
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

func (s *Service) LimitedAny(id string, keys []string) (bool, error) {
	for _, key := range keys {
		limited, err := s.IsLimited(id, key, time.Now())
		if err != nil {
			return false, err
		}
		if limited {
			return true, nil
		}
	}
	return false, nil
}
