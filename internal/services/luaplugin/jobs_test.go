package luaplugin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
)

const jobsPluginSource = `--- @plugin Jobs Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("jobs-type", {
  complete = function(ctx, request)
    return nil, { message = "no", code = "server_error" }
  end,
  credential_schema = {
    { type = "secret", name = "api_key", label = "API Key" },
  },
  proxy_schema = {},
  jobs = {
    refresh = {
      interval_seconds = 300, run_on_startup = true, timeout_ms = 5000,
      run = function(ctx)
        return true
      end,
    },
  },
})
`

func installJobsPlugin(t *testing.T, service *Service) {
	t.Helper()
	if _, err := service.InstallSQLite(context.Background(), []byte(jobsPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
}

func TestJobSpecificationsStored(t *testing.T) {
	service := setupService(t)
	installJobsPlugin(t, service)
	jobs := service.Jobs("jobs-type")
	jobSpecification, ok := jobs["refresh"]
	if !ok {
		t.Fatalf("jobs: %v", jobs)
	}
	if jobSpecification.IntervalSeconds != 300 || !jobSpecification.RunOnStartup || jobSpecification.TimeoutMs != 5000 {
		t.Fatalf("job specification: %+v", jobSpecification)
	}
	if !service.CredentialsEnabled("jobs-type") {
		t.Fatal("credential_schema must enable credentials")
	}
	if !service.ProxiesEnabled("jobs-type") {
		t.Fatal("proxy_schema must enable proxies")
	}
	if service.CredentialsEnabled("no-such-type") || service.ProxiesEnabled("no-such-type") {
		t.Fatal("unknown types must report disabled")
	}
}

func TestJobSpecificationsInvalid(t *testing.T) {
	cases := map[string]string{
		"missing run":      `refresh = { interval_seconds = 300 }`,
		"interval low":     `refresh = { interval_seconds = 30, run = function(ctx) return true end }`,
		"interval high":    `refresh = { interval_seconds = 90000, run = function(ctx) return true end }`,
		"run not function": `refresh = { interval_seconds = 300, run = 42 }`,
		"bad name":         `["has-dash"] = { interval_seconds = 300, run = function(ctx) return true end }`,
	}
	for name, jobsBody := range cases {
		service := setupService(t)
		src := `--- @plugin Jobs Bad
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("jobs-bad", {
  complete = function(ctx, request) return nil, { message = "no", code = "server_error" } end,
  jobs = { ` + jobsBody + ` },
})
`
		if _, err := service.InstallSQLite(context.Background(), []byte(src), PluginOrigin{Manual: true}); err == nil {
			t.Errorf("%s: expected install error", name)
		}
	}
}

func TestRunJobSuccess(t *testing.T) {
	service := setupService(t)
	installJobsPlugin(t, service)
	if err := service.RunJob(context.Background(), "jobs-type", "refresh", "manual", "p1", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunJobUnknown(t *testing.T) {
	service := setupService(t)
	installJobsPlugin(t, service)
	if err := service.RunJob(context.Background(), "jobs-type", "missing", "manual", "p1", nil); err == nil {
		t.Fatal("unknown job must fail")
	}
	if err := service.RunJob(context.Background(), "no-such-type", "refresh", "manual", "p1", nil); err == nil {
		t.Fatal("unknown type must fail")
	}
}

func TestRunJobWritesCredentials(t *testing.T) {
	service := setupService(t)
	src := `--- @plugin Jobs Writer
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("jobs-writer", {
  complete = function(ctx, request) return nil, { message = "no", code = "server_error" } end,
  credential_schema = {
    { type = "secret", name = "api_key", label = "API Key" },
  },
  jobs = {
    refresh = {
      interval_seconds = 300,
      run = function(ctx)
        for _, c in ipairs(llm_router.credentials.list()) do
          local data = c.data or {}
          data.access_token = "refreshed"
          llm_router.credentials.update(c.id, data)
        end
        return true
      end,
    },
  },
})
`
	if _, err := service.InstallSQLite(context.Background(), []byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	updated := map[string]map[string]any{}
	service.SetCredentialAccess(
		func(ctx context.Context, providerID string) ([]*models.Credential, error) {
			return []*models.Credential{{ID: "c1", ProviderID: "p1", Data: map[string]any{"refresh_token": "r"}}, {ID: "c2", ProviderID: "p1", Disabled: true}}, nil
		},
		func(ctx context.Context, id string) (*models.Credential, error) { return nil, nil },
		func(ctx context.Context, id string, data map[string]any) error { updated[id] = data; return nil },
	)
	if err := service.RunJob(context.Background(), "jobs-writer", "refresh", "tick", "p1", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(updated) != 1 || updated["c1"]["access_token"] != "refreshed" {
		t.Fatalf("updated: %v", updated)
	}
}

func TestCredentialsTableGating(t *testing.T) {
	service := setupService(t)
	installJobsPlugin(t, service)
	// Request context: update must fail, list/get serve.
	service.SetCredentialAccess(
		func(ctx context.Context, providerID string) ([]*models.Credential, error) {
			return []*models.Credential{{ID: "c1", Data: map[string]any{"api_key": "k"}}}, nil
		},
		func(ctx context.Context, id string) (*models.Credential, error) {
			return &models.Credential{ID: id, Data: map[string]any{}}, nil
		},
		func(ctx context.Context, id string, data map[string]any) error { return nil },
	)
	src := `--- @plugin Gate Probe
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("gate-type", {
  complete = function(ctx, request)
    local n = #(llm_router.credentials.list())
    local ok, err = pcall(llm_router.credentials.update, "c1", {})
    return nil, { message = "n=" .. tostring(n) .. " update_ok=" .. tostring(ok) }
  end,
  credential_schema = {
    { type = "secret", name = "api_key", label = "API Key" },
  },
})
`
	if _, err := service.InstallSQLite(context.Background(), []byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := service.Complete(context.Background(), testMetadata("gate-type", nil, "gate-type/m", nil),
		&models.ChatCompletionRequest{Model: "gate-type/m"})
	perr, ok := err.(*models.ProviderError)
	if !ok || !strings.Contains(perr.Message, "n=1") || !strings.Contains(perr.Message, "update_ok=false") {
		t.Fatalf("gating: %v", err)
	}
}

func TestStorageTTLExpiry(t *testing.T) {
	service := setupService(t)
	ctx := context.Background()
	// Storage rows hang off the plugin row; seed the bare minimum so the
	// foreign key stays valid.
	now := time.Now().UTC()
	pluginRow := record.Plugin{ID: "p1", Source: []byte("--"), InstalledAt: now, UpdatedAt: now}
	if err := repository.NewGormRepository[record.Plugin](service.database, "id").Upsert(ctx, pluginRow); err != nil {
		t.Fatalf("seed plugin: %v", err)
	}
	if err := service.SetStorageSQLite(ctx, "p1", "s", "k", "v", 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got, err := service.GetStorageSQLite(ctx, "p1", "s", "k"); err != nil || got != "v" {
		t.Fatalf("get: %v %v", got, err)
	}
	if err := service.SetStorageSQLite(ctx, "p1", "s", "exp", "v", time.Second); err != nil {
		t.Fatalf("set ttl: %v", err)
	}
	if got, err := service.GetStorageSQLite(ctx, "p1", "s", "exp"); err != nil || got != "v" {
		t.Fatalf("unexpired ttl row must read: %v %v", got, err)
	}
	if err := service.SetStorageSQLite(ctx, "p1", "s", "neg", "v", -time.Second); err == nil {
		t.Fatal("negative ttl must fail")
	}
	// An expired row reads as missing and deletes the row.
	past := time.Now().Add(-time.Hour).UTC()
	expiredRow := record.PluginStorage{PluginID: "p1", Scope: "s", Key: "gone", Value: []byte(`"stale"`), ExpiresAt: &past}
	if err := repository.NewGormRepository[record.PluginStorage](service.cache, "plugin_id", "scope", "key").Upsert(ctx, expiredRow); err != nil {
		t.Fatalf("seed expired: %v", err)
	}
	if got, err := service.GetStorageSQLite(ctx, "p1", "s", "gone"); err != nil || got != nil {
		t.Fatalf("expired row must read missing: %v %v", got, err)
	}
}
