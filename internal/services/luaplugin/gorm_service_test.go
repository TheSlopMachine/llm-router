package luaplugin

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	stdErrors "errors"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	"github.com/TheSlopMachine/llm-router/internal/version"
	"gorm.io/gorm/clause"
)

func setupSQLitePluginService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	sqliteDatabase := testutil.SetupTestSQLite(t)
	service, err := New(sqliteDatabase, nil)
	if err != nil {
		t.Fatalf("new sqlite service: %v", err)
	}
	return service, context.Background()
}

const comprehensivePluginSource = `--- @plugin Comprehensive Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @description Comprehensive plugin
--- @allow_host example.com
--- @allow_host api.example.com

llm_router.register("comprehensive-type", {
  icon = "https://example.com/icon.svg",
  complete = function(ctx, request)
    return {
      id = "chatcmpl-comprehensive",
      object = "chat.completion",
      created = 1700000000,
      model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = "hello" }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,
  healthcheck_cooldown = 600,
  model_specs = {
    ["model-a"] = { display_name = "Model A", context_window = 4096 },
  },
  credential_schema = {
    { type = "secret", name = "api_key", label = "API Key" },
  },
  config_schema = {
    { type = "input", name = "endpoint", label = "Endpoint" },
  },
  jobs = {
    refresh = {
      interval_seconds = 300, run_on_startup = true, timeout_ms = 5000,
      run = function(ctx) return true end,
    },
  },
})

llm_router.register_proxy_source("feed-one", {
  fetch_proxies = function()
    return {}
  end,
})
`

func TestInstallSQLiteRoundTripPreservesDeclarations(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	installed, err := service.InstallSQLite(ctx, []byte(comprehensivePluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install sqlite: %v", err)
	}
	if installed.ID != "manual/tester/Comprehensive-Plugin" {
		t.Fatalf("identifier: got %q", installed.ID)
	}
	fetched, err := service.GetSQLite(ctx, installed.ID)
	if err != nil {
		t.Fatalf("get sqlite: %v", err)
	}
	if len(fetched.TypeKeys) != 1 || fetched.TypeKeys[0] != "comprehensive-type" {
		t.Fatalf("type keys: %v", fetched.TypeKeys)
	}
	if len(fetched.AllowHosts) != 2 {
		t.Fatalf("allow hosts: %v", fetched.AllowHosts)
	}
	if fetched.Unsafe {
		t.Fatal("manual plugin with bare hosts must not be unsafe")
	}
	if fetched.Icons["comprehensive-type"] != "https://example.com/icon.svg" {
		t.Fatalf("icons: %v", fetched.Icons)
	}
	if fetched.HealthCooldown["comprehensive-type"] != 600 {
		t.Fatalf("health cooldown: %v", fetched.HealthCooldown)
	}
	specification := fetched.ModelSpecifications["comprehensive-type"]["model-a"]
	if specification.DisplayName != "Model A" || specification.ContextWindow != 4096 {
		t.Fatalf("model specifications: %+v", specification)
	}
	if len(fetched.ProxySourceKeys) != 1 || fetched.ProxySourceKeys[0] != "feed-one" {
		t.Fatalf("proxy source keys: %v", fetched.ProxySourceKeys)
	}
	if _, exists := fetched.Schemas["comprehensive-type"]["credential_schema"]; !exists {
		t.Fatalf("credential schema missing: %v", fetched.Schemas)
	}
	if _, exists := fetched.Schemas["comprehensive-type"]["config_schema"]; !exists {
		t.Fatalf("config schema missing: %v", fetched.Schemas)
	}
	jobSpecification := fetched.Jobs["comprehensive-type"]["refresh"]
	if jobSpecification.IntervalSeconds != 300 || !jobSpecification.RunOnStartup || jobSpecification.TimeoutMs != 5000 {
		t.Fatalf("job specification: %+v", jobSpecification)
	}
	if string(fetched.Source) != comprehensivePluginSource {
		t.Fatal("source must round-trip verbatim")
	}
	if fetched.InstalledAt.IsZero() || fetched.UpdatedAt.IsZero() {
		t.Fatal("timestamps must be set")
	}
	if fetched.InstalledAt.Location() != time.UTC || fetched.UpdatedAt.Location() != time.UTC {
		t.Fatalf("timestamps must be UTC: %v %v", fetched.InstalledAt, fetched.UpdatedAt)
	}
}

const sourceOnlyPluginSource = `--- @plugin Source Only
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register_proxy_source("feed-solo", {
  fetch_proxies = function()
    return {}
  end,
})
`

// TestProxySourceKeysIncludesSourceOnlyPlugins proves proxy sources
// survive startup: source-only plugins carry no type key, so the
// type-keyed in-memory registry never holds them. Keys must come from
// storage, both on the installing service and on a fresh service rebuilt
// from the rows (the startup path).
func TestProxySourceKeysIncludesSourceOnlyPlugins(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	service, err := New(database, nil)
	if err != nil {
		t.Fatalf("new sqlite service: %v", err)
	}
	ctx := context.Background()
	installed, err := service.InstallSQLite(ctx, []byte(sourceOnlyPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install source-only plugin: %v", err)
	}
	want := []string{QualifiedSourceKey(installed.ID, "feed-solo")}
	keys, err := service.ProxySourceKeys()
	if err != nil {
		t.Fatalf("proxy source keys: %v", err)
	}
	if len(keys) != 1 || keys[0] != want[0] {
		t.Fatalf("proxy source keys: got %v, want %v", keys, want)
	}
	rebooted, err := New(database, nil)
	if err != nil {
		t.Fatalf("rebuilt sqlite service: %v", err)
	}
	keys, err = rebooted.ProxySourceKeys()
	if err != nil {
		t.Fatalf("rebuilt proxy source keys: %v", err)
	}
	if len(keys) != 1 || keys[0] != want[0] {
		t.Fatalf("rebuilt proxy source keys: got %v, want %v", keys, want)
	}
}

func TestInstallSQLiteUpdatePushesHistory(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	installed, err := service.InstallSQLite(ctx, []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install v1: %v", err)
	}
	secondSource := strings.Replace(testPluginSource, "@version 1.0.0", "@version 2.0.0", 1)
	updated, err := service.InstallSQLite(ctx, []byte(secondSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install v2: %v", err)
	}
	if updated.Version != "2.0.0" {
		t.Fatalf("version: got %q", updated.Version)
	}
	if len(updated.History) != 1 {
		t.Fatalf("history length: got %d want 1", len(updated.History))
	}
	if updated.History[0].Version != "1.0.0" {
		t.Fatalf("history version: got %q", updated.History[0].Version)
	}
	fetched, err := service.GetSQLite(ctx, installed.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(fetched.History) != 1 || fetched.History[0].Version != "1.0.0" {
		t.Fatalf("persisted history: %+v", fetched.History)
	}
}

func TestInstallSQLiteHistoryCapsAtTen(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	identifier := ""
	for iteration := 0; iteration < 12; iteration++ {
		versioned := strings.Replace(testPluginSource, "@version 1.0.0", fmt.Sprintf("@version 1.0.%d", iteration), 1)
		installed, err := service.InstallSQLite(ctx, []byte(versioned), PluginOrigin{Manual: true})
		if err != nil {
			t.Fatalf("install %d: %v", iteration, err)
		}
		identifier = installed.ID
	}
	fetched, err := service.GetSQLite(ctx, identifier)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(fetched.History) != maxPluginHistoryEntries {
		t.Fatalf("history length: got %d want %d", len(fetched.History), maxPluginHistoryEntries)
	}
}

func TestRollbackSQLiteRestoresPrevious(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	installed, err := service.InstallSQLite(ctx, []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install v1: %v", err)
	}
	secondSource := strings.Replace(testPluginSource, "@version 1.0.0", "@version 2.0.0", 1)
	second, err := service.InstallSQLite(ctx, []byte(secondSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install v2: %v", err)
	}
	if second.Version != "2.0.0" {
		t.Fatalf("v2 version: %q", second.Version)
	}
	rolled, err := service.RollbackSQLite(ctx, installed.ID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolled.Version != "1.0.0" {
		t.Fatalf("rolled version: %q", rolled.Version)
	}
	fetched, err := service.GetSQLite(ctx, installed.ID)
	if err != nil {
		t.Fatalf("get after rollback: %v", err)
	}
	if fetched.Version != "1.0.0" {
		t.Fatalf("persisted version: %q", fetched.Version)
	}
	// Rollback toggles forward: the rolled-back-from version returns to history.
	rerolled, err := service.RollbackSQLite(ctx, installed.ID)
	if err != nil {
		t.Fatalf("second rollback: %v", err)
	}
	if rerolled.Version != "2.0.0" {
		t.Fatalf("toggle version: %q", rerolled.Version)
	}
}

func TestRollbackSQLiteWithoutHistoryFails(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	installed, err := service.InstallSQLite(ctx, []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := service.RollbackSQLite(ctx, installed.ID); err == nil {
		t.Fatal("rollback without history must fail")
	}
	if _, err := service.RollbackSQLite(ctx, "missing-plugin"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("rollback missing must report not found, got %v", err)
	}
}

func TestRollbackSQLiteRejectsIncompatiblePrevious(t *testing.T) {
	originalAPI := PluginAPIVersion
	defer func() { PluginAPIVersion = originalAPI }()
	PluginAPIVersion = version.Version{Major: 2}
	sqliteDatabase := testutil.SetupTestSQLite(t)
	service, err := New(sqliteDatabase, nil)
	if err != nil {
		t.Fatalf("new sqlite service: %v", err)
	}
	ctx := context.Background()
	firstSource := strings.Replace(testPluginSource, "@plugin_api 1.0", "@plugin_api 2.0", 1)
	installed, err := service.InstallSQLite(ctx, []byte(firstSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install v1: %v", err)
	}
	secondSource := strings.Replace(firstSource, "@version 1.0.0", "@version 2.0.0", 1)
	second, err := service.InstallSQLite(ctx, []byte(secondSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install v2: %v", err)
	}
	PluginAPIVersion = originalAPI
	if _, err := service.RollbackSQLite(ctx, installed.ID); err == nil {
		t.Fatal("rollback to a newer-major previous version must fail")
	} else {
		var compat *APICompatError
		if !stdErrors.As(err, &compat) || compat.Reason != "too_new" {
			t.Fatalf("expected too_new, got: %v", err)
		}
	}
	fetched, err := service.GetSQLite(ctx, installed.ID)
	if err != nil {
		t.Fatalf("get after rejected rollback: %v", err)
	}
	if fetched.Version != second.Version {
		t.Fatalf("rejected rollback must not mutate: version %q", fetched.Version)
	}
	if len(fetched.History) != 1 {
		t.Fatalf("rejected rollback must not mutate history: got %d entries", len(fetched.History))
	}
}

func TestRebuildSkipsIncompatiblePlugin(t *testing.T) {
	originalAPI := PluginAPIVersion
	defer func() { PluginAPIVersion = originalAPI }()
	sqliteDatabase := testutil.SetupTestSQLite(t)
	service, err := New(sqliteDatabase, nil)
	if err != nil {
		t.Fatalf("new sqlite service: %v", err)
	}
	ctx := context.Background()
	compatibleSource := strings.Replace(testPluginSource, `"test-type"`, `"keep-type"`, 1)
	if _, err := service.InstallSQLite(ctx, []byte(compatibleSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install compatible: %v", err)
	}
	PluginAPIVersion = version.Version{Major: 2}
	futureSource := strings.Replace(testPluginSource, "@plugin_api 1.0", "@plugin_api 2.0", 1)
	futureSource = strings.Replace(futureSource, "Test Plugin", "Future Plugin", 1)
	futureSource = strings.Replace(futureSource, `"test-type"`, `"future-type"`, 1)
	future, err := service.InstallSQLite(ctx, []byte(futureSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install future: %v", err)
	}
	PluginAPIVersion = originalAPI
	restarted, err := New(sqliteDatabase, nil)
	if err != nil {
		t.Fatalf("restart with an incompatible row must succeed: %v", err)
	}
	if _, err := restarted.Lookup("keep-type"); err != nil {
		t.Fatalf("compatible plugin must stay registered: %v", err)
	}
	if _, err := restarted.Lookup("future-type"); err == nil {
		t.Fatal("incompatible plugin must stay out of the registry")
	}
	if restarted.HasHandler("future-type", "complete") {
		t.Fatal("incompatible plugin must serve no handlers")
	}
	stored, err := restarted.GetSQLite(ctx, future.ID)
	if err != nil {
		t.Fatalf("incompatible plugin must stay inventoried: %v", err)
	}
	if stored.PluginAPI != "2.0" {
		t.Fatalf("inventoried plugin API: got %q", stored.PluginAPI)
	}
}

func TestGetSQLiteMissReportsNotFound(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	if _, err := service.GetSQLite(ctx, "missing-plugin"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("get missing must report not found, got %v", err)
	}
}

func TestListSQLiteOrdersByIdentifier(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	secondSource := strings.Replace(testPluginSource, `"test-type"`, `"aaa-type"`, 1)
	secondSource = strings.Replace(secondSource, "Test Plugin", "AAA Plugin", 1)
	if _, err := service.InstallSQLite(ctx, []byte(testPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install first: %v", err)
	}
	if _, err := service.InstallSQLite(ctx, []byte(secondSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install second: %v", err)
	}
	listed, err := service.ListSQLite(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("list length: got %d want 2", len(listed))
	}
	if listed[0].ID > listed[1].ID {
		t.Fatalf("list must order by identifier: %q before %q", listed[0].ID, listed[1].ID)
	}
}

func TestDeleteSQLiteCascadesAndReportsMissing(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	installed, err := service.InstallSQLite(ctx, []byte(comprehensivePluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := service.SetStorageSQLite(ctx, installed.ID, "scope", "key", "value", 0); err != nil {
		t.Fatalf("storage set: %v", err)
	}
	if err := service.DeleteSQLite(ctx, installed.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := service.GetSQLite(ctx, installed.ID); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("get after delete must report not found, got %v", err)
	}
	if err := service.DeleteSQLite(ctx, installed.ID); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("second delete must report not found, got %v", err)
	}
	// Storage rows cascade from the plugin delete.
	if value, err := service.GetStorageSQLite(ctx, installed.ID, "scope", "key"); err != nil || value != nil {
		t.Fatalf("cascaded storage must read missing: %v %v", value, err)
	}
}

func TestInstallSQLiteRejectsTypeKeyConflict(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	if _, err := service.InstallSQLite(ctx, []byte(testPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install first: %v", err)
	}
	conflicting := strings.Replace(testPluginSource, "Test Plugin", "Other Plugin", 1)
	conflicting = strings.Replace(conflicting, "@author tester", "@author other", 1)
	if _, err := service.InstallSQLite(ctx, []byte(conflicting), PluginOrigin{Manual: true}); !stdErrors.Is(err, ErrTypeKeyConflict) {
		t.Fatalf("conflict must report type key sentinel, got %v", err)
	}
}

func TestInstallSQLiteManualAndRepositoryOrigins(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	manual, err := service.InstallSQLite(ctx, []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("manual install: %v", err)
	}
	if !manual.Origin.Manual || manual.Origin.RepoID != "" {
		t.Fatalf("manual origin: %+v", manual.Origin)
	}
	// Repository installs reference an existing repository row so the
	// foreign key stays valid. Server wiring creates the repository before
	// installing; tests seed it explicitly.
	sqliteHandle := service.database
	repositoryStore := repository.NewGormRepository[record.PluginRepository](sqliteHandle, "id")
	repositoryRow := record.PluginRepository{
		ID:          "origin-repository",
		Kind:        "index",
		IndexURL:    "https://example.com/index.json",
		SourceURL:   "https://example.com",
		Title:       "Origin",
		Description: "Origin repository",
		AddedAt:     manual.InstalledAt,
	}
	if err := repositoryStore.Upsert(ctx, repositoryRow); err != nil {
		t.Fatalf("seed repository: %v", err)
	}
	repositoryOrigin := PluginOrigin{RepoID: "origin-repository", Path: "llm-router-plugins/test-plugin.lua"}
	repositorySource := strings.Replace(testPluginSource, `"test-type"`, `"repository-type"`, 1)
	repositoryInstalled, err := service.InstallSQLite(ctx, []byte(repositorySource), repositoryOrigin)
	if err != nil {
		t.Fatalf("repository install: %v", err)
	}
	if repositoryInstalled.Origin.Manual || repositoryInstalled.Origin.RepoID != "origin-repository" {
		t.Fatalf("repository origin: %+v", repositoryInstalled.Origin)
	}
	fetched, err := service.GetSQLite(ctx, repositoryInstalled.ID)
	if err != nil {
		t.Fatalf("get repository plugin: %v", err)
	}
	if fetched.Origin != repositoryOrigin {
		t.Fatalf("origin round-trip: got %+v want %+v", fetched.Origin, repositoryOrigin)
	}
}

func TestStorageSQLiteRoundTripPreservesShapes(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	installed, err := service.InstallSQLite(ctx, []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	pluginID := installed.ID
	cases := map[string]any{
		"string": "hello",
		"number": float64(42),
		"object": map[string]any{"nested": "value"},
		"array":  []any{"first", float64(2)},
	}
	for scopeKey, expected := range cases {
		if err := service.SetStorageSQLite(ctx, pluginID, "shapes", scopeKey, expected, 0); err != nil {
			t.Fatalf("set %s: %v", scopeKey, err)
		}
		fetched, err := service.GetStorageSQLite(ctx, pluginID, "shapes", scopeKey)
		if err != nil {
			t.Fatalf("get %s: %v", scopeKey, err)
		}
		if fmt.Sprintf("%v", fetched) != fmt.Sprintf("%v", expected) {
			t.Fatalf("shape %s: got %v want %v", scopeKey, fetched, expected)
		}
	}
}

func TestStorageSQLiteTTLExpiryAndValidation(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	installed, err := service.InstallSQLite(ctx, []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	pluginID := installed.ID
	if err := service.SetStorageSQLite(ctx, pluginID, "scope", "persistent", "value", 0); err != nil {
		t.Fatalf("set persistent: %v", err)
	}
	if value, err := service.GetStorageSQLite(ctx, pluginID, "scope", "persistent"); err != nil || value != "value" {
		t.Fatalf("persistent read: %v %v", value, err)
	}
	if err := service.SetStorageSQLite(ctx, pluginID, "scope", "temporary", "value", time.Second); err != nil {
		t.Fatalf("set temporary: %v", err)
	}
	if value, err := service.GetStorageSQLite(ctx, pluginID, "scope", "temporary"); err != nil || value != "value" {
		t.Fatalf("unexpired read: %v %v", value, err)
	}
	if err := service.SetStorageSQLite(ctx, pluginID, "scope", "negative", "value", -time.Second); err == nil {
		t.Fatal("negative TTL must fail")
	}
	if err := service.SetStorageSQLite(ctx, pluginID, "scope", "excessive", "value", 91*24*time.Hour); err == nil {
		t.Fatal("TTL beyond 90 days must fail")
	}
	if err := service.SetStorageSQLite(ctx, pluginID, "", "key", "value", 0); err == nil {
		t.Fatal("empty scope must fail")
	}
	if err := service.SetStorageSQLite(ctx, pluginID, "scope", "", "value", 0); err == nil {
		t.Fatal("empty key must fail")
	}
	// An expired row reads as missing and is removed in the same transaction.
	if err := service.SetStorageSQLite(ctx, pluginID, "scope", "expired", "stale", time.Second); err != nil {
		t.Fatalf("set expired candidate: %v", err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	expiredRow := record.PluginStorage{PluginID: pluginID, Scope: "scope", Key: "expired", Value: []byte(`"stale"`), ExpiresAt: &past}
	expiredRows := []record.PluginStorage{expiredRow}
	if err := repository.NewGormRepository[record.PluginStorage](service.cache, "plugin_id", "scope", "key").Upsert(ctx, expiredRows...); err != nil {
		t.Fatalf("seed expired row: %v", err)
	}
	if value, err := service.GetStorageSQLite(ctx, pluginID, "scope", "expired"); err != nil || value != nil {
		t.Fatalf("expired read must be missing: %v %v", value, err)
	}
	if value, err := service.GetStorageSQLite(ctx, pluginID, "scope", "expired"); err != nil || value != nil {
		t.Fatalf("expired row must stay deleted: %v %v", value, err)
	}
}

func TestStorageSQLiteDeleteAndDeletePlugin(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	installed, err := service.InstallSQLite(ctx, []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	pluginID := installed.ID
	if err := service.SetStorageSQLite(ctx, pluginID, "scope", "first", "one", 0); err != nil {
		t.Fatalf("set first: %v", err)
	}
	if err := service.SetStorageSQLite(ctx, pluginID, "scope", "second", "two", 0); err != nil {
		t.Fatalf("set second: %v", err)
	}
	if err := service.DeleteStorageSQLite(ctx, pluginID, "scope", "first"); err != nil {
		t.Fatalf("delete first: %v", err)
	}
	if value, err := service.GetStorageSQLite(ctx, pluginID, "scope", "first"); err != nil || value != nil {
		t.Fatalf("deleted entry must read missing: %v %v", value, err)
	}
	if err := service.DeleteStorageSQLite(ctx, pluginID, "scope", "missing"); err != nil {
		t.Fatalf("delete missing must not fail: %v", err)
	}
	if err := service.DeletePluginStorageSQLite(ctx, pluginID); err != nil {
		t.Fatalf("delete plugin storage: %v", err)
	}
	if value, err := service.GetStorageSQLite(ctx, pluginID, "scope", "second"); err != nil || value != nil {
		t.Fatalf("plugin storage must read missing after reset: %v %v", value, err)
	}
}

func TestStorageSQLitePruneExpiredUsesIndex(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	installed, err := service.InstallSQLite(ctx, []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	pluginID := installed.ID
	if err := service.SetStorageSQLite(ctx, pluginID, "scope", "persistent", "keep", 0); err != nil {
		t.Fatalf("set persistent: %v", err)
	}
	if err := service.SetStorageSQLite(ctx, pluginID, "scope", "temporary", "keep", time.Hour); err != nil {
		t.Fatalf("set temporary: %v", err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	staleRows := []record.PluginStorage{
		{PluginID: pluginID, Scope: "scope", Key: "stale-one", Value: []byte(`"old"`), ExpiresAt: &past},
		{PluginID: pluginID, Scope: "scope", Key: "stale-two", Value: []byte(`"old"`), ExpiresAt: &past},
	}
	if err := repository.NewGormRepository[record.PluginStorage](service.cache, "plugin_id", "scope", "key").Upsert(ctx, staleRows...); err != nil {
		t.Fatalf("seed stale rows: %v", err)
	}
	removed, err := service.PruneExpiredStorageSQLite(ctx)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 2 {
		t.Fatalf("pruned count: got %d want 2", removed)
	}
	if value, err := service.GetStorageSQLite(ctx, pluginID, "scope", "persistent"); err != nil || value != "keep" {
		t.Fatalf("persistent must survive prune: %v %v", value, err)
	}
	if value, err := service.GetStorageSQLite(ctx, pluginID, "scope", "temporary"); err != nil || value != "keep" {
		t.Fatalf("unexpired must survive prune: %v %v", value, err)
	}
}

func TestStorageSQLiteRoutesByTTL(t *testing.T) {
	service, ctx := setupSQLitePluginService(t)
	installed, err := service.InstallSQLite(ctx, []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	pluginID := installed.ID
	durableStore := repository.NewGormRepository[record.PluginStorage](service.database, "plugin_id", "scope", "key")
	cacheStore := repository.NewGormRepository[record.PluginStorage](service.cache, "plugin_id", "scope", "key")
	conditions := []clause.Expression{
		clause.Eq{Column: "plugin_id", Value: pluginID},
		clause.Eq{Column: "scope", Value: "route"},
		clause.Eq{Column: "key", Value: "entry"},
	}
	if err := service.SetStorageSQLite(ctx, pluginID, "route", "entry", "durable", 0); err != nil {
		t.Fatalf("set durable: %v", err)
	}
	if _, err := durableStore.Get(ctx, conditions...); err != nil {
		t.Fatalf("durable row must land in the database: %v", err)
	}
	if _, err := cacheStore.Get(ctx, conditions...); err == nil {
		t.Fatal("durable row must not land in the cache")
	}
	if err := service.SetStorageSQLite(ctx, pluginID, "route", "entry", "ephemeral", time.Hour); err != nil {
		t.Fatalf("set ttl: %v", err)
	}
	if _, err := cacheStore.Get(ctx, conditions...); err != nil {
		t.Fatalf("ttl row must land in the cache: %v", err)
	}
	if _, err := durableStore.Get(ctx, conditions...); err == nil {
		t.Fatal("ttl row must leave the database")
	}
	if value, err := service.GetStorageSQLite(ctx, pluginID, "route", "entry"); err != nil || value != "ephemeral" {
		t.Fatalf("migrated read: %v %v", value, err)
	}
	if err := service.SetStorageSQLite(ctx, pluginID, "route", "entry", "durable", 0); err != nil {
		t.Fatalf("set durable again: %v", err)
	}
	if _, err := durableStore.Get(ctx, conditions...); err != nil {
		t.Fatalf("row must migrate back to the database: %v", err)
	}
	if _, err := cacheStore.Get(ctx, conditions...); err == nil {
		t.Fatal("row must leave the cache")
	}
}

func TestSQLiteMethodsFailClosedWithoutHandle(t *testing.T) {
	service := &Service{}
	ctx := context.Background()
	if _, err := service.InstallSQLite(ctx, []byte(testPluginSource), PluginOrigin{Manual: true}); err == nil {
		t.Fatal("install without sqlite must fail")
	}
	if _, err := service.GetSQLite(ctx, "missing"); err == nil {
		t.Fatal("get without sqlite must fail")
	}
	if _, err := service.ListSQLite(ctx); err == nil {
		t.Fatal("list without sqlite must fail")
	}
	if err := service.DeleteSQLite(ctx, "missing"); err == nil {
		t.Fatal("delete without sqlite must fail")
	}
	if _, err := service.RollbackSQLite(ctx, "missing"); err == nil {
		t.Fatal("rollback without sqlite must fail")
	}
	if err := service.SetStorageSQLite(ctx, "plugin", "scope", "key", "value", 0); err == nil {
		t.Fatal("storage set without sqlite must fail")
	}
	if _, err := service.GetStorageSQLite(ctx, "plugin", "scope", "key"); err == nil {
		t.Fatal("storage get without sqlite must fail")
	}
}
