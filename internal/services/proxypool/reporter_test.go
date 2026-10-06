package proxypool

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	proxypoollib "github.com/TheSlopMachine/proxypool"
)

func TestFormatBanReasons(t *testing.T) {
	got := formatBanReasons(map[proxypoollib.FailReason]int{
		proxypoollib.FailRefused: 3,
		proxypoollib.FailTimeout: 10,
		proxypoollib.FailDNS:     0,
		proxypoollib.FailTLS:     3,
		proxypoollib.FailNone:    99,
	})
	if got != "timeout=10 refused=3 tls=3" {
		t.Fatalf("formatBanReasons = %q", got)
	}
}

func TestSlogReporterLevels(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	reporter := newSlogReporter(logger)
	reporter.ReportProxy(proxypoollib.ProxyReport{URL: "http://127.0.0.1:8080"})
	if buf.Len() != 0 {
		t.Fatal("ReportProxy must be silent below debug level")
	}

	reporter.ReportStats(proxypoollib.PoolStats{
		Mode: proxypoollib.ModeBackground,
		Net:  proxypoollib.NetSnapshot{State: proxypoollib.NetDegraded},
	})
	if !strings.Contains(buf.String(), "proxy pool stats") {
		t.Fatal("ReportStats must emit info")
	}

	buf.Reset()
	reporter.ReportEvent(proxypoollib.PoolEvent{
		Kind:   "breaker_open",
		Fields: map[string]string{"from": "good", "to": "down"},
	})
	if !strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("breaker_open must be warning, got %q", buf.String())
	}

	buf.Reset()
	reporter.ReportEvent(proxypoollib.PoolEvent{
		Kind:   "net_change",
		Fields: map[string]string{"from": "good", "to": string(proxypoollib.NetDown)},
	})
	if !strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("net_change to down must be warning, got %q", buf.String())
	}
}
