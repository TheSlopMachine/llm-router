package models

import (
	"testing"
)

func TestParseProxyRetryConfig(t *testing.T) {
	dflt, err := ParseProxyRetryConfig(nil)
	if err != nil || dflt.Mode != ProxyRetryFailFast || dflt.MaxAttempts != DefaultProxyRetryMaxAttempts {
		t.Fatalf("default: %+v %v", dflt, err)
	}
	retry, err := ParseProxyRetryConfig(map[string]any{"proxy_retry": map[string]any{"mode": "next_proxy", "max_attempts": 5}})
	if err != nil || retry.Mode != ProxyRetryNextProxy || retry.MaxAttempts != 5 {
		t.Fatalf("retry: %+v %v", retry, err)
	}
	if _, err := ParseProxyRetryConfig(map[string]any{"proxy_retry": map[string]any{"mode": "bogus"}}); err == nil {
		t.Fatal("unknown mode must fail")
	}
	clamped, err := ParseProxyRetryConfig(map[string]any{"proxy_retry": map[string]any{"mode": "next_proxy", "max_attempts": 99}})
	if err != nil || clamped.MaxAttempts != MaxProxyRetryMaxAttempts {
		t.Fatalf("clamp: %+v %v", clamped, err)
	}
}

func TestMigrateGeoToProxyRetry(t *testing.T) {
	cfg := map[string]any{"geo": map[string]any{"mode": "retry_same_key", "max_proxies": 5}}
	if !MigrateGeoToProxyRetry(cfg) {
		t.Fatal("expected migration to report a change")
	}
	if _, ok := cfg["geo"]; ok {
		t.Fatalf("geo key must be removed: %v", cfg)
	}
	got, err := ParseProxyRetryConfig(cfg)
	if err != nil || got.Mode != ProxyRetryNextProxy || got.MaxAttempts != 5 {
		t.Fatalf("migrated policy: %+v %v", got, err)
	}
	plain := map[string]any{"geo": map[string]any{"mode": "fail_fast"}}
	if !MigrateGeoToProxyRetry(plain) {
		t.Fatal("expected migration to report a change")
	}
	if got, _ := ParseProxyRetryConfig(plain); got.Mode != ProxyRetryFailFast || got.MaxAttempts != DefaultProxyRetryMaxAttempts {
		t.Fatalf("migrated fail_fast policy: %+v", got)
	}
	if MigrateGeoToProxyRetry(map[string]any{}) {
		t.Fatal("absent geo must report no change")
	}
	if MigrateGeoToProxyRetry(nil) {
		t.Fatal("nil config must report no change")
	}
}
