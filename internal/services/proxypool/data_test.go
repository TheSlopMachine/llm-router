package proxypool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// combinedProxyExport is a file in the format of the former combined proxy
// export: both split subsystems take their own part of it.
const combinedProxyExport = `{
	"version": 1,
	"settings": {"enabled": true, "pool_enabled": true, "max_checks": 0},
	"pools": [{"id": "mine", "name": "Mine", "entries": [{"url": "http://192.0.2.21:8080"}]}],
	"proxies": [{"url": "http://203.0.113.1:8080", "country": "de"}]
}`

func TestSplitProxyDataImportsCombinedExport(t *testing.T) {
	service, err := New(testutil.SetupTestSQLite(t))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()

	var pools models.ProxyPoolsBundle
	if err := json.Unmarshal([]byte(combinedProxyExport), &pools); err != nil {
		t.Fatalf("decode pools: %v", err)
	}
	if err := service.ImportPools(ctx, &pools); err != nil {
		t.Fatalf("import pools: %v", err)
	}
	var proxies models.DiscoveredProxiesBundle
	if err := json.Unmarshal([]byte(combinedProxyExport), &proxies); err != nil {
		t.Fatalf("decode proxies: %v", err)
	}
	if err := service.ImportProxies(ctx, &proxies); err != nil {
		t.Fatalf("import proxies: %v", err)
	}

	exported, err := service.ExportPools(ctx)
	if err != nil {
		t.Fatalf("export pools: %v", err)
	}
	if len(exported.Pools) != 1 || exported.Pools[0].ID != "mine" {
		t.Fatalf("exported pools: %+v", exported.Pools)
	}
	if queued := len(service.imported); queued != 1 {
		t.Fatalf("queued imported proxies: got %d, want 1", queued)
	}
}

func TestClearProxiesKeepsPoolsAndClearPoolsKeepsProxies(t *testing.T) {
	service, err := New(testutil.SetupTestSQLite(t))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()
	if _, err := service.SaveCustomPool(ctx, &models.CustomProxyPool{
		Name:    "Mine",
		Entries: []models.ProxyEntry{{URL: "http://192.0.2.21:8080"}},
	}); err != nil {
		t.Fatalf("save pool: %v", err)
	}
	queue := func() {
		t.Helper()
		bundle := &models.DiscoveredProxiesBundle{
			Version: models.ProxyDataVersion,
			Proxies: []models.ProxyDataEntry{{URL: "http://203.0.113.1:8080"}},
		}
		if err := service.ImportProxies(ctx, bundle); err != nil {
			t.Fatalf("import proxies: %v", err)
		}
	}

	queue()
	if err := service.ClearProxies(ctx); err != nil {
		t.Fatalf("clear proxies: %v", err)
	}
	if queued := len(service.imported); queued != 0 {
		t.Fatalf("queued proxies after clearing proxies: got %d, want 0", queued)
	}
	pools, err := service.ListCustomPools(ctx)
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 1 {
		t.Fatalf("pools after clearing proxies: got %d, want 1", len(pools))
	}

	queue()
	if err := service.ClearPools(ctx); err != nil {
		t.Fatalf("clear pools: %v", err)
	}
	pools, err = service.ListCustomPools(ctx)
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 0 {
		t.Fatalf("pools after clearing pools: got %d, want 0", len(pools))
	}
	if queued := len(service.imported); queued != 1 {
		t.Fatalf("queued proxies after clearing pools: got %d, want 1", queued)
	}
}

func TestSplitProxyDataRejectsUnknownVersion(t *testing.T) {
	service, err := New(testutil.SetupTestSQLite(t))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()
	if err := service.ImportPools(ctx, &models.ProxyPoolsBundle{Version: 2}); err == nil {
		t.Fatal("pools import of version 2 succeeded, want an error")
	}
	if err := service.ImportProxies(ctx, &models.DiscoveredProxiesBundle{Version: 2}); err == nil {
		t.Fatal("proxies import of version 2 succeeded, want an error")
	}
}
