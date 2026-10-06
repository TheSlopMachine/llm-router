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
	logger.Debug("proxy check",
		"timestamp", report.Timestamp,
		"url", report.URL,
		"source", report.Source,
		"lane", report.Lane,
		"fail_reason", report.FailReason,
		"is_dead", report.IsDead,
		"score", report.Score,
		"penalty", report.Penalty,
		"revive_at", report.ReviveAt,
		"latency_ms", report.Latency.Milliseconds(),
		"died", report.Died,
		"revived", report.Revived,
	)
}

func (r *slogReporter) ReportStats(stats proxypoollib.PoolStats) {
	r.mu.RLock()
	logger := r.logger
	r.mu.RUnlock()
	if logger == nil {
		return
	}
	logger.Info("proxy pool stats",
		"mode", stats.Mode,
		"net", stats.Net.State,
		"rtt_ms", stats.Net.RTT.Milliseconds(),
		"limit", stats.Limit,
		"inflight", stats.Inflight,
		"alive", stats.Alive,
		"suspect", stats.Suspect,
		"banned", stats.Banned,
		"queued", stats.Queued,
	)
	for _, source := range stats.Sources {
		logger.Info("proxy source stats",
			"source", source.Source,
			"alive", source.Alive,
			"suspect", source.Suspect,
			"banned", source.Banned,
			"queued", source.Queued,
			"ban_reasons", formatBanReasons(source.BanReasons),
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
	level := slog.LevelInfo
	if event.Kind == "breaker_open" {
		level = slog.LevelWarn
	}
	if event.Kind == "net_change" && event.Fields != nil && event.Fields["to"] == string(proxypoollib.NetDown) {
		level = slog.LevelWarn
	}
	attrs := []any{"kind", event.Kind}
	keys := make([]string, 0, len(event.Fields))
	for key := range event.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		attrs = append(attrs, key, event.Fields[key])
	}
	logger.Log(context.Background(), level, "proxy pool event", append([]any{"time", event.Time}, attrs...)...)
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
