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
	luaSvc.SetProxyResolver(func(goCtx context.Context, rec *luaplugin.PluginRecord, providerConfig map[string]any) ([]luaplugin.ProxyPick, error) {
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
		out := make([]luaplugin.ProxyPick, 0, len(picks))
		for _, p := range picks {
			if dropExhaustedProxy(exhaustedSvc, rec.ID, typeKey, p.ID) {
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

// dropExhaustedProxy reports whether the proxy combination (plugin, type,
// proxy) matches a stored limit key. A nil store disables filtering;
// lookup failures fail open so a struggling store never blocks traffic.
func dropExhaustedProxy(exhaustedSvc *exhausted.Service, pluginID, typeKey, proxyID string) bool {
	if exhaustedSvc == nil {
		return false
	}
	hit, err := exhaustedSvc.LimitedAny(exhausted.Segments{Plugin: pluginID, Provider: typeKey, Proxy: proxyID})
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
