package server

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
	"github.com/TheSlopMachine/llm-router/internal/services/geoban"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
)

// wireProxy connects the Lua HTTP client to the proxy pool: ordered picks
// for plugin calls, with exhausted proxy combinations deprioritized to the
// tail (earliest reset first) and indefinite geo flags still filtered after
// ranking. Multi-type plugins resolve through the requesting type key: the
// type key: the caller scopes the plugin record before invoking the resolver.
// known.Provider already carries the calling provider instance ID (set by the
// caller); this only fills in Plugin, which the resolver alone knows. Geo
// flags stay keyed by adapter type (see exec.go), so the type key below is
// deliberate, not a leftover.
// After a geo ban, unbanned picks from other regions float above unbanned
// picks sharing a banned region, so same-key geo retries land on another
// region instead of re-hitting the blocked country.
func wireProxy(luaSvc *luaplugin.Service, proxySvc *proxypool.Service, exhaustedSvc *exhausted.Service, geobanSvc *geoban.Service, logger *slog.Logger) {
	if proxySvc != nil {
		luaSvc.SetMarkDead(proxySvc.MarkDead)
	}
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
			regions, err := geobanSvc.BannedRegions(rec.ID, typeKey, func(proxyID string) string {
				return regionOf[proxyID]
			})
			if err != nil {
				return nil, fmt.Errorf("provider proxy: load geo bans: %w", err)
			}
			bannedRegions = regions
		}
		type rankedPick struct {
			pick        proxypool.Pick
			otherRegion bool
		}
		type heldPick struct {
			pick        proxypool.Pick
			otherRegion bool
			resetsAt    time.Time
		}
		ranked := make([]rankedPick, 0, len(picks))
		var held []heldPick
		deferredCount := 0
		droppedGeoban := 0
		for _, p := range picks {
			candidate := known
			candidate.Proxy = p.ID
			if geobanSvc != nil {
				banned, err := geobanSvc.IsBanned(rec.ID, typeKey, p.ID)
				if err != nil {
					return nil, fmt.Errorf("provider proxy: check geo ban: %w", err)
				}
				if banned {
					droppedGeoban++
					continue
				}
			}
			resetsAt, limited, err := checkProxyLimits(exhaustedSvc, proxySvc, candidate)
			if err != nil {
				return nil, err
			}
			otherRegion := p.Location == "" || !bannedRegions[p.Location]
			if limited {
				deferredCount++
				held = append(held, heldPick{pick: p, otherRegion: otherRegion, resetsAt: resetsAt})
				continue
			}
			ranked = append(ranked, rankedPick{pick: p, otherRegion: otherRegion})
		}
		sort.SliceStable(held, func(i, j int) bool {
			return held[i].resetsAt.Before(held[j].resetsAt)
		})
		for _, h := range held {
			ranked = append(ranked, rankedPick{pick: h.pick, otherRegion: h.otherRegion})
		}
		if logger != nil {
			logger.Debug("proxy: filtered picks",
				"plugin_id", rec.ID, "type", typeKey,
				"total", len(picks), "kept", len(ranked),
				"deferred_limited", deferredCount, "dropped_geoban", droppedGeoban)
		}
		if proxyCfg.Mode == models.ProxyModeAuto {
			// Stable partition: other-region picks first, same latency order
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
		if len(picks) > 0 && len(out) == 0 {
			return nil, fmt.Errorf("provider proxy: %w (%d selected)", luaplugin.ErrNoProxyRoute, len(picks))
		}
		return out, nil
	})
}

// checkProxyLimits reports the reset time when joint request limits cover the
// candidate proxy. Exhausted joint keys and proxy-scoped limits both
// deprioritize; the later reset wins when both apply. Storage errors stop
// routing instead of allowing a proxy whose limit state could not be read.
func checkProxyLimits(exhaustedSvc *exhausted.Service, proxySvc *proxypool.Service, candidate exhausted.Segments) (time.Time, bool, error) {
	var resetsAt time.Time
	limited := false
	if exhaustedSvc != nil {
		expiry, hit, err := exhaustedSvc.MatchExpiry(candidate)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("provider proxy: check exhausted key: %w", err)
		}
		if hit {
			resetsAt, limited = expiry, true
		}
	}
	if proxySvc == nil {
		return resetsAt, limited, nil
	}
	expiry, hit, err := proxySvc.LimitExpiry(candidate.Proxy, exhausted.SubKeys(candidate), time.Now())
	if err != nil {
		return time.Time{}, false, fmt.Errorf("provider proxy: check proxy limit: %w", err)
	}
	if hit && (!limited || resetsAt.Before(expiry)) {
		resetsAt, limited = expiry, true
	}
	return resetsAt, limited, nil
}
