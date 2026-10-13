package proxypool

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"

	proxypoollib "github.com/TheSlopMachine/proxypool"
)

type slogReporter struct {
	mu     sync.RWMutex
	logger *slog.Logger
}

func newSlogReporter(logger *slog.Logger) *slogReporter {
	if logger == nil {
		logger = slog.Default()
	}
	return &slogReporter{logger: logger}
}

func (r *slogReporter) SetLogger(logger *slog.Logger) {
	if logger == nil {
		return
	}
	r.mu.Lock()
	r.logger = logger
	r.mu.Unlock()
}

func (r *slogReporter) ReportProxy(report proxypoollib.ProxyReport) {
	r.mu.RLock()
	logger := r.logger
	r.mu.RUnlock()
	if logger == nil || !logger.Enabled(context.Background(), slog.LevelDebug) {
		return
	}
	logger.Debug("Proxy check",
		"proxy_url", redactProxyURL(report.URL),
		"source", report.Source,
		"lane", report.Lane,
		"reason", string(report.FailReason),
		"latency_ms", report.Latency.Milliseconds(),
		"count", boolToCount(report.IsDead),
	)
}

func (r *slogReporter) ReportStats(stats proxypoollib.PoolStats) {
	r.mu.RLock()
	logger := r.logger
	r.mu.RUnlock()
	if logger == nil {
		return
	}
	logger.Info("Proxy pool stats",
		"count", stats.Alive+stats.Suspect+stats.Banned,
		"limit", stats.Limit,
		"operation", string(stats.Mode),
	)
	for _, lane := range stats.Lanes {
		logger.Info("Proxy lane stats",
			"lane", lane.Lane,
			"count", lane.Queued,
		)
	}
	for _, source := range stats.Sources {
		logger.Info("Proxy source stats",
			"source", source.Source,
			"count", source.Alive+source.Suspect+source.Banned,
			"reason", formatBanReasons(source.BanReasons),
		)
	}
	r.reportDemands(logger, stats.Demands)
}

func (r *slogReporter) reportDemands(logger *slog.Logger, demands []proxypoollib.DemandStat) {
	for _, demand := range demands {
		countries := "any"
		if len(demand.Countries) > 0 {
			countries = strings.Join(demand.Countries, ",")
		}
		logger.Info("Proxy demand stats",
			"countries", countries,
			"operation", demand.State,
			"count", demand.Waiters,
		)
	}
}

func (r *slogReporter) ReportEvent(event proxypoollib.PoolEvent) {
	r.mu.RLock()
	logger := r.logger
	r.mu.RUnlock()
	if logger == nil {
		return
	}
	warn := event.Kind == "breaker_open"
	if event.Kind == "net_change" && event.Fields != nil && event.Fields["to"] == string(proxypoollib.NetDown) {
		warn = true
	}
	attrs := []any{"operation", event.Kind, "count", len(event.Fields)}
	keys := make([]string, 0, len(event.Fields))
	for key := range event.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		attrs = append(attrs, key, event.Fields[key])
	}
	if warn {
		logger.Warn("Proxy pool event", attrs...)
		return
	}
	logger.Info("Proxy pool event", attrs...)
}

func formatBanReasons(reasons map[proxypoollib.FailReason]int) string {
	type item struct {
		reason string
		count  int
	}
	items := make([]item, 0, len(reasons))
	for reason, count := range reasons {
		if count <= 0 || reason == proxypoollib.FailNone {
			continue
		}
		items = append(items, item{reason: string(reason), count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].reason < items[j].reason
	})
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%s=%s", item.reason, strconv.Itoa(item.count)))
	}
	return strings.Join(parts, " ")
}

var _ proxypoollib.Reporter = (*slogReporter)(nil)
