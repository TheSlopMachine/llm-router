package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
	"github.com/TheSlopMachine/llm-router/internal/services/geoban"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
)

// wireProxy connects the Lua HTTP client to the proxy pool: ordered picks
// for plugin calls, filtered by exhausted proxy combinations and indefinite
// geo flags after ranking. Multi-type plugins resolve through the requesting
// type key: the caller scopes the plugin record before invoking the resolver.
// known.Provider already carries the calling provider instance ID (set by the
// caller); this only fills in Plugin, which the resolver alone knows. Geo
// flags stay keyed by adapter type (see exec.go), so the type key below is
// deliberate, not a leftover.
// After a geo ban, unbanned picks from other regions float above unbanned
// picks sharing a banned region, so same-key geo retries land on another
// region instead of re-hitting the blocked country.
func wireProxy(luaSvc *luaplugin.Service, proxySvc *proxypool.Service, exhaustedSvc *exhausted.Service, geobanSvc *geoban.Service, logger *slog.Logger) {
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
		known.Plugin = rec.ID
		regionOf := make(map[string]string, len(picks))
		for _, p := range picks {
			regionOf[p.ID] = p.Location
		}
		bannedRegions := map[string]bool{}
		if geobanSvc != nil {
			if regions, err := geobanSvc.BannedRegions(rec.ID, typeKey, func(proxyID string) string {
				return regionOf[proxyID]
			}); err == nil {
				bannedRegions = regions
			}
		}
		type rankedPick struct {
			pick        proxypool.Pick
			otherRegion bool
		}
		ranked := make([]rankedPick, 0, len(picks))
		droppedExhausted := 0
		droppedGeoban := 0
		for _, p := range picks {
			candidate := known
			candidate.Proxy = p.ID
			if dropExhaustedProxy(exhaustedSvc, candidate) {
				droppedExhausted++
				continue
			}
			if geobanSvc != nil {
				if banned, err := geobanSvc.IsBanned(rec.ID, typeKey, p.ID); err == nil && banned {
					droppedGeoban++
					continue
				}
			}
			ranked = append(ranked, rankedPick{pick: p, otherRegion: p.Location == "" || !bannedRegions[p.Location]})
		}
		if logger != nil {
			logger.Debug("proxy: filtered picks",
				"plugin_id", rec.ID, "type", typeKey,
				"total", len(picks), "kept", len(ranked),
				"dropped_exhausted", droppedExhausted, "dropped_geoban", droppedGeoban)
		}
		if proxyCfg.Mode == models.ProxyModeAuto {
			// Stable partition: other-region picks first, same speed order
			// within each group. Manual order stays sacred.
			var other, same []rankedPick
			for _, rp := range ranked {
				if rp.otherRegion {
					other = append(other, rp)
				} else {
					same = append(same, rp)
				}
			}
			if logger != nil {
				logger.Debug("proxy: sorted regions",
					"plugin_id", rec.ID, "type", typeKey,
					"other_region", len(other), "same_region", len(same))
			}
			ranked = append(other, same...)
		}
		out := make([]luaplugin.ProxyPick, 0, len(ranked))
		for _, rp := range ranked {
			out = append(out, luaplugin.ProxyPick{ID: rp.pick.ID, URL: rp.pick.URL, Region: rp.pick.Location})
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
