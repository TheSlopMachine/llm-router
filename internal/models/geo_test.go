package models

import (
	"testing"
)

func TestParseGeoConfig(t *testing.T) {
	dflt, err := ParseGeoConfig(nil)
	if err != nil || dflt.Mode != GeoModeFailFast || dflt.MaxProxies != DefaultGeoMaxProxies {
		t.Fatalf("default: %+v %v", dflt, err)
	}
	retry, err := ParseGeoConfig(map[string]any{"geo": map[string]any{"mode": "retry_same_key", "max_proxies": 5}})
	if err != nil || retry.Mode != GeoModeRetrySameKey || retry.MaxProxies != 5 {
		t.Fatalf("retry: %+v %v", retry, err)
	}
	if _, err := ParseGeoConfig(map[string]any{"geo": map[string]any{"mode": "bogus"}}); err == nil {
		t.Fatal("unknown mode must fail")
	}
	clamped, err := ParseGeoConfig(map[string]any{"geo": map[string]any{"mode": "retry_same_key", "max_proxies": 99}})
	if err != nil || clamped.MaxProxies != MaxGeoMaxProxies {
		t.Fatalf("clamp: %+v %v", clamped, err)
	}
}
