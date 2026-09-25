package proxypool

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// Pick is one ranked proxy candidate.
type Pick struct {
	ID  string
	URL string
}

// Rank returns the ordered proxy list for a provider call: whitelist and
// pair-state filtered, fastest first. Disabled mode returns nil (direct).
// Manual mode follows the ids order and fails loudly when nothing usable
// is pooled. An empty whitelist allows any location.
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

func (s *Service) usable(p *models.Proxy, allow map[string]bool, provider string) bool {
	lim, err := s.limits.Get(limitID(p.ID, provider))
	if err != nil {
		lim = nil
	}
	return usableWith(p, allow, lim)
}

// usableWith applies whitelist and preloaded pair state to one proxy.
func usableWith(p *models.Proxy, allow map[string]bool, lim *models.ProxyLimit) bool {
	if allow != nil && !allow[p.Location] {
		return false
	}
	if lim == nil {
		return true
	}
	if lim.Blocked {
		return false
	}
	if lim.ResetsAt != nil && lim.ResetsAt.After(util.Now()) {
		return false
	}
	return true
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
	limits := s.limitsFor(provider, all)
	ranked := make([]*models.Proxy, 0, len(all))
	for _, p := range all {
		if !usableWith(p, allow, limits[limitID(p.ID, provider)]) {
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

// limitsFor preloads pair states for one provider across a proxy set: one
// bulk scan instead of N point reads in the ranking loop.
func (s *Service) limitsFor(provider string, all []*models.Proxy) map[string]*models.ProxyLimit {
	limits, err := s.limits.List()
	if err != nil {
		return nil
	}
	want := map[string]bool{}
	for _, p := range all {
		want[limitID(p.ID, provider)] = true
	}
	out := make(map[string]*models.ProxyLimit, len(want))
	for _, lim := range limits {
		if want[limitID(lim.ProxyID, lim.Provider)] {
			out[limitID(lim.ProxyID, lim.Provider)] = lim
		}
	}
	return out
}

func (s *Service) rankManual(ids []string, provider string) ([]Pick, error) {
	picks := []Pick{}
	for _, id := range ids {
		p, err := s.proxies.Get(id)
		if err != nil || p == nil {
			continue
		}
		if !s.usable(p, nil, provider) {
			continue
		}
		picks = append(picks, Pick{ID: p.ID, URL: p.URL})
	}
	if len(picks) == 0 {
		return nil, fmt.Errorf("provider proxy: no usable proxy among %d selected", len(ids))
	}
	return picks, nil
}

// ─────────────────────────────────────────────
// Live pair state
// ─────────────────────────────────────────────

func limitID(proxyID, provider string) string {
	return proxyID + "\x00" + provider
}

// RecordRateLimit records an upstream rate-limit for a proxy-provider pair.
// The pair is skipped until resetsAt passes.
func (s *Service) RecordRateLimit(proxyID, provider string, resetsAt time.Time) {
	if proxyID == "" {
		return
	}
	id := limitID(proxyID, provider)
	lim, err := s.limits.Get(id)
	if err != nil || lim == nil {
		lim = &models.ProxyLimit{ProxyID: proxyID, Provider: provider}
	}
	lim.ResetsAt = &resetsAt
	_ = s.limits.Put(id, lim)
	s.notify()
}

// RecordBlocked blocks a proxy for a provider after a geo-block answer.
// Blocks never expire on their own; the pair dies with the proxy.
func (s *Service) RecordBlocked(proxyID, provider, reason string) {
	if proxyID == "" {
		return
	}
	id := limitID(proxyID, provider)
	lim, err := s.limits.Get(id)
	if err != nil || lim == nil {
		lim = &models.ProxyLimit{ProxyID: proxyID, Provider: provider}
	}
	lim.Blocked = true
	lim.BlockReason = reason
	_ = s.limits.Put(id, lim)
	s.notify()
}
