package server

import (
	"context"
	"fmt"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
)

// wireProxy connects the Lua HTTP client to the proxy pool: ordered picks
// for plugin calls, filtered by exhausted proxy combinations after ranking.
// Multi-type plugins resolve through the requesting type key: the caller
// scopes the plugin record before invoking the resolver.
func wireProxy(luaSvc *luaplugin.Service, proxySvc *proxypool.Service, exhaustedSvc *exhausted.Service) {
	luaSvc.SetProxyResolver(func(goCtx context.Context, rec *luaplugin.PluginRecord, providerConfig map[string]any, known exhausted.Segments) ([]luaplugin.ProxyPick, error) {
		proxyCfg, err := models.ParseProxyConfig(providerConfig)
		if err != nil {
			return nil, err
		}
		typeKey := ""
		if len(rec.TypeKeys) > 0 {
			typeKey = rec.TypeKeys[0]
		}
		var picks []proxypool.Pick
		if proxyCfg.Mode == models.ProxyModeAuto {
			// Auto waits for ready or no-proxies instead of silently
			// going direct.
			picks, err = proxySvc.RankWait(goCtx, rec.ProxyLocations, proxyCfg.IDs, typeKey)
		} else {
			picks, err = proxySvc.Rank(rec.ProxyLocations, proxyCfg.Mode, proxyCfg.IDs, typeKey)
		}
		if err != nil {
			return nil, err
		}
		known.Plugin, known.Provider = rec.ID, typeKey
		out := make([]luaplugin.ProxyPick, 0, len(picks))
		for _, p := range picks {
			candidate := known
			candidate.Proxy = p.ID
			if dropExhaustedProxy(exhaustedSvc, candidate) {
				continue
			}
			out = append(out, luaplugin.ProxyPick{ID: p.ID, URL: p.URL})
		}
		if proxyCfg.Mode == models.ProxyModeManual && len(picks) > 0 && len(out) == 0 {
			return nil, fmt.Errorf("provider proxy: no usable proxy among %d selected", len(picks))
		}
		return out, nil
	})
}

// dropExhaustedProxy reports whether candidate — plugin, type, proxy, and
// whatever account/model the caller already fixed for this attempt —
// matches a stored limit key at any narrowing. A nil store disables
// filtering; lookup failures fail open so a struggling store never blocks
// traffic.
func dropExhaustedProxy(exhaustedSvc *exhausted.Service, candidate exhausted.Segments) bool {
	if exhaustedSvc == nil {
		return false
	}
	hit, err := exhaustedSvc.LimitedAny(candidate)
	if err != nil {
		return false
	}
	return hit != ""
}

// proxyTickInterval converts the configured rotation period.
func proxyTickInterval(cfg models.RouterConfiguration) time.Duration {
	if cfg.UpdateIntervalMinutes < 1 {
		return time.Duration(models.DefaultUpdateIntervalMinutes) * time.Minute
	}
	return time.Duration(cfg.UpdateIntervalMinutes) * time.Minute
}
