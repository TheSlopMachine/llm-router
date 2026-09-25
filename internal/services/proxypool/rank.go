package proxypool

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// Pick is one ranked proxy candidate.
type Pick struct {
	ID  string
	URL string
}

// Rank returns the ordered proxy list for a provider call: whitelist
// filtered, fastest first. Limit filtering lives downstream of ranking, in
// the exhausted store. Disabled mode returns nil (direct). Manual mode
// follows the ids order and fails loudly when nothing usable is pooled.
// An empty whitelist allows any location.
func (s *Service) Rank(whitelist []string, mode string, ids []string, provider string) ([]Pick, error) {
	if mode != models.ProxyModeDisabled {
		s.NoteDemand(whitelist)
	}
	switch mode {
	case models.ProxyModeManual:
		return s.rankManual(ids, provider)
	case models.ProxyModeAuto:
		return s.rankAuto(whitelist, provider), nil
	default:
		return nil, nil
	}
}

// RankWait is Rank for auto mode with waiting: empty picks while probe
// work runs or a fetch is still due block until a pick appears, the pool
// settles empty (ErrNoProxies), or the context aborts. Providers wait for
// ready or no-proxies instead of silently going direct.
func (s *Service) RankWait(ctx context.Context, whitelist []string, ids []string, provider string) ([]Pick, error) {
	for {
		picks, err := s.Rank(whitelist, models.ProxyModeAuto, ids, provider)
		if err != nil {
			return nil, err
		}
		if len(picks) > 0 {
			return picks, nil
		}
		if !s.Checking() && !s.NeedsSearch() {
			return nil, ErrNoProxies
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.changed():
		case <-time.After(recheckInterval):
		}
	}
}

// Choose returns the fastest usable proxy, or ("", "", nil) for direct.
func (s *Service) Choose(whitelist []string, mode string, ids []string, provider string) (string, string, error) {
	picks, err := s.Rank(whitelist, mode, ids, provider)
	if err != nil {
		return "", "", err
	}
	if len(picks) == 0 {
		return "", "", nil
	}
	return picks[0].ID, picks[0].URL, nil
}

// usable applies the location whitelist to one proxy. Limit state lives in
// the exhausted store and filters downstream of ranking, never here.
func usable(p *models.Proxy, allow map[string]bool) bool {
	return allow == nil || allow[p.Location]
}

func whitelistSet(whitelist []string) map[string]bool {
	if len(whitelist) == 0 {
		return nil
	}
	allow := map[string]bool{}
	for _, loc := range whitelist {
		if loc = NormalizeCountryCode(loc); loc != "" {
			allow[loc] = true
		}
	}
	return allow
}

func (s *Service) rankAuto(whitelist []string, provider string) []Pick {
	all, err := s.proxies.List()
	if err != nil {
		return nil
	}
	allow := whitelistSet(whitelist)
	ranked := make([]*models.Proxy, 0, len(all))
	for _, p := range all {
		if !usable(p, allow) {
			continue
		}
		ranked = append(ranked, p)
	}
	sort.Slice(ranked, func(i, j int) bool { return lessProxy(ranked[i], ranked[j]) })
	picks := make([]Pick, 0, len(ranked))
	for _, p := range ranked {
		picks = append(picks, Pick{ID: p.ID, URL: p.URL})
	}
	return picks
}

func (s *Service) rankManual(ids []string, provider string) ([]Pick, error) {
	picks := []Pick{}
	for _, id := range ids {
		p, err := s.proxies.Get(id)
		if err != nil || p == nil {
			continue
		}
		if !usable(p, nil) {
			continue
		}
		picks = append(picks, Pick{ID: p.ID, URL: p.URL})
	}
	if len(picks) == 0 {
		return nil, fmt.Errorf("provider proxy: no usable proxy among %d selected", len(ids))
	}
	return picks, nil
}
