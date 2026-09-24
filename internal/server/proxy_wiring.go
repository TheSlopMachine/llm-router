package server

import (
	"context"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
)

// wireProxy connects the Lua HTTP client to the proxy pool: ordered picks
// for plugin calls and pair outcome feedback for rate limits and blocks.
// Multi-type plugins resolve through the requesting type key: the caller
// scopes the plugin record before invoking the resolver.
func wireProxy(luaSvc *luaplugin.Service, proxySvc *proxypool.Service) {
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
			out = append(out, luaplugin.ProxyPick{ID: p.ID, URL: p.URL})
		}
		return out, nil
	})
	luaSvc.SetProxyEventReporter(func(ev luaplugin.ProxyEvent) {
		if ev.RateLimited {
			proxySvc.RecordRateLimit(ev.ProxyID, ev.Provider, ev.ResetsAt)
		}
		if ev.Blocked {
			proxySvc.RecordBlocked(ev.ProxyID, ev.Provider, ev.BlockReason)
		}
	})
}

// proxyTickInterval converts the configured rotation period.
func proxyTickInterval(cfg models.RouterConfiguration) time.Duration {
	if cfg.UpdateIntervalMinutes < 1 {
		return time.Duration(models.DefaultUpdateIntervalMinutes) * time.Minute
	}
	return time.Duration(cfg.UpdateIntervalMinutes) * time.Minute
}
