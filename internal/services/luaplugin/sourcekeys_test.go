package luaplugin

import (
	"context"
	"testing"
)

func sourcePlugin(name, source string) string {
	return `--- @plugin ` + name + `
--- @author tester
--- @version 1.0.0
--- @router_version 0.0.4
--- @allow_host example.com
--- @proxy_source true

llm_router.register_proxy_source("` + source + `", {
  fetch_proxies = function(ctx)
    return nil
  end,
})
`
}

// TestProxySourceKeysQualifyByRecord installs two different plugins claiming
// one source name: keys stay distinct per record instead of colliding.
func TestProxySourceKeysQualifyByRecord(t *testing.T) {
	svc := setupService(t)
	recA, err := svc.Install([]byte(sourcePlugin("Source A", "shared")), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install A: %v", err)
	}
	recB, err := svc.Install([]byte(sourcePlugin("Source B", "shared")), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install B: %v", err)
	}
	if recA.ID == recB.ID {
		t.Fatalf("records must differ: %q", recA.ID)
	}
	keys := svc.ProxySourceKeys()
	if len(keys) != 2 {
		t.Fatalf("keys: got %v", keys)
	}
	want := map[string]bool{
		QualifiedSourceKey(recA.ID, "shared"): true,
		QualifiedSourceKey(recB.ID, "shared"): true,
	}
	for _, k := range keys {
		if !want[k] {
			t.Fatalf("keys: got %v, want %v", keys, want)
		}
		delete(want, k)
	}
	if len(want) != 0 {
		t.Fatalf("keys missing: %v", want)
	}
}

// TestFetchProxiesRoutesExactKey serves each record's own fetch function
// under its qualified key: no first-wins crossover.
func TestFetchProxiesRoutesExactKey(t *testing.T) {
	svc := setupService(t)
	recA, err := svc.Install([]byte(sourcePlugin("Source A", "shared")), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install A: %v", err)
	}
	if _, err := svc.Install([]byte(sourcePlugin("Source B", "shared")), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install B: %v", err)
	}
	keyA := QualifiedSourceKey(recA.ID, "shared")
	got, err := svc.FetchProxies(context.Background(), keyA)
	if err != nil {
		t.Fatalf("fetch A: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("fetch A: got %v", got)
	}
	if _, err := svc.FetchProxies(context.Background(), "shared"); err == nil {
		t.Fatal("bare key must not resolve")
	}
}
