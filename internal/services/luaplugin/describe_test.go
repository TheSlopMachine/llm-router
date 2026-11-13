package luaplugin

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

const proxySourcePluginSource = `--- @plugin Source Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @description Proxy source plugin
--- @allow_host example.com

llm_router.register_proxy_source("source-type", {
  icon = "https://example.com/source.svg",
  fetch_proxies = function(ctx)
    return {}
  end,
})
`

const proxySourceWithoutIconSource = `--- @plugin Plain Source
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @description Proxy source plugin without icon
--- @allow_host example.com

llm_router.register_proxy_source("plain-source", {
  fetch_proxies = function(ctx)
    return {}
  end,
})
`

const loopingPluginSource = `--- @plugin Looping Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @description Plugin that never finishes loading
--- @allow_host example.com

while true do end
`

func parseTestManifest(t *testing.T, source string) *Manifest {
	t.Helper()
	manifest, err := ParseManifest([]byte(source))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return manifest
}

func TestInstallCapturesProxySourceIcon(t *testing.T) {
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(proxySourcePluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if got := record.Icons["source-type"]; got != "https://example.com/source.svg" {
		t.Fatalf("record icon: got %q", got)
	}
	if got := record.Icon(); got != "https://example.com/source.svg" {
		t.Fatalf("plugin icon: got %q", got)
	}
	if got, want := record.Subsystems(), []string{SubsystemProxies}; !reflect.DeepEqual(got, want) {
		t.Fatalf("subsystems: got %v, want %v", got, want)
	}
}

func TestInstallAcceptsProxySourceWithoutIcon(t *testing.T) {
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(proxySourceWithoutIconSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if got := record.Icon(); got != "" {
		t.Fatalf("plugin icon: got %q, want empty", got)
	}
}

func TestInstallRejectsBadProxySourceIcon(t *testing.T) {
	service := setupService(t)
	source := strings.Replace(proxySourcePluginSource, `https://example.com/source.svg`, `http://example.com/source.svg`, 1)
	if _, err := service.InstallSQLite(context.Background(), []byte(source), PluginOrigin{Manual: true}); err == nil {
		t.Fatal("expected install error for an http icon")
	}
}

func TestDescribeReportsSubsystemsAndIcon(t *testing.T) {
	service := setupService(t)
	cases := map[string]struct {
		source     string
		subsystems []string
		icon       string
	}{
		"provider":              {iconPluginSource, []string{SubsystemProviders}, "https://example.com/icon.svg"},
		"provider without icon": {testPluginSource, []string{SubsystemProviders}, ""},
		"proxy source":          {proxySourcePluginSource, []string{SubsystemProxies}, "https://example.com/source.svg"},
	}
	for name, testCase := range cases {
		description, err := service.Describe([]byte(testCase.source), parseTestManifest(t, testCase.source))
		if err != nil {
			t.Fatalf("%s: describe: %v", name, err)
		}
		if !reflect.DeepEqual(description.Subsystems, testCase.subsystems) {
			t.Errorf("%s: subsystems got %v, want %v", name, description.Subsystems, testCase.subsystems)
		}
		if description.Icon != testCase.icon {
			t.Errorf("%s: icon got %q, want %q", name, description.Icon, testCase.icon)
		}
	}
}

func TestDescribeCachesBySourceDigest(t *testing.T) {
	service := setupService(t)
	manifest := parseTestManifest(t, iconPluginSource)
	if _, err := service.Describe([]byte(iconPluginSource), manifest); err != nil {
		t.Fatalf("describe: %v", err)
	}
	if got := len(service.descriptions.entries); got != 1 {
		t.Fatalf("cache entries after first call: %d", got)
	}
	if _, err := service.Describe([]byte(iconPluginSource), manifest); err != nil {
		t.Fatalf("describe again: %v", err)
	}
	if got := len(service.descriptions.entries); got != 1 {
		t.Fatalf("cache entries after repeated call: %d", got)
	}
}

func TestDescribeStopsLoopingSource(t *testing.T) {
	service := setupService(t)
	if _, err := service.Describe([]byte(loopingPluginSource), parseTestManifest(t, loopingPluginSource)); err == nil {
		t.Fatal("expected a timeout error")
	}
}
