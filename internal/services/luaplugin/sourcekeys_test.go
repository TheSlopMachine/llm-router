package luaplugin

import (
	"context"
	"testing"
)

func sourcePlugin(name, source string) string {
	return `--- @plugin ` + name + `
--- @author tester
--- @version 1.0.0
--- @router_version 0.1.1
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

// TestRollbackRestoresProxySourceKeys guards the snapshot contract: proxy
// source keys survive an update that drops them and come back on rollback.
func TestRollbackRestoresProxySourceKeys(t *testing.T) {
	svc := setupService(t)
	v1 := sourcePlugin("Source RB", "rbsrc")
	rec, err := svc.Install([]byte(v1), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install v1: %v", err)
	}
	if len(rec.ProxySourceKeys) != 1 {
		t.Fatalf("v1 source keys: %v", rec.ProxySourceKeys)
	}
	v2 := `--- @plugin Source RB
--- @author tester
--- @version 2.0.0
--- @router_version 0.1.1
--- @allow_host example.com

llm_router.register("rb-type", {
  complete = function(ctx, credential, request)
    return nil, { type = "upstream", message = "v2" }
  end,
})
`
	rec2, err := svc.Install([]byte(v2), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install v2: %v", err)
	}
	if len(rec2.ProxySourceKeys) != 0 {
		t.Fatalf("v2 source keys: %v", rec2.ProxySourceKeys)
	}
	rolled, err := svc.Rollback(rec2.ID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if len(rolled.ProxySourceKeys) != 1 {
		t.Fatalf("rolled-back source keys: %v", rolled.ProxySourceKeys)
	}
}

// TestRebuildRejectsDuplicateTypeKeys locks the registry fail-closed: two
// stored records claiming one type key abort the rebuild instead of
// silently serving last-wins.
func TestRebuildRejectsDuplicateTypeKeys(t *testing.T) {
	svc := setupService(t)
	mkrec := func(id string) *PluginRecord {
		return &PluginRecord{ID: id, TypeKeys: []string{"dup-type"}}
	}
	if err := svc.repo.Put("manual/tester/A", mkrec("manual/tester/A")); err != nil {
		t.Fatalf("put A: %v", err)
	}
	if err := svc.repo.Put("manual/tester/B", mkrec("manual/tester/B")); err != nil {
		t.Fatalf("put B: %v", err)
	}
	if err := svc.rebuild(); err == nil {
		t.Fatal("duplicate type key must fail the rebuild")
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
