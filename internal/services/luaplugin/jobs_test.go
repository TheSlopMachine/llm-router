package luaplugin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	bolt "go.etcd.io/bbolt"
)

const jobsPluginSource = `--- @plugin Jobs Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.7.0
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

func installJobsPlugin(t *testing.T, svc *Service) {
	t.Helper()
	if _, err := svc.Install([]byte(jobsPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
}

func TestJobSpecsStored(t *testing.T) {
	svc := setupService(t)
	installJobsPlugin(t, svc)
	jobs := svc.Jobs("jobs-type")
	spec, ok := jobs["refresh"]
	if !ok {
		t.Fatalf("jobs: %v", jobs)
	}
	if spec.IntervalSeconds != 300 || !spec.RunOnStartup || spec.TimeoutMs != 5000 {
		t.Fatalf("spec: %+v", spec)
	}
	if !svc.CredentialsEnabled("jobs-type") {
		t.Fatal("credential_schema must enable credentials")
	}
	if !svc.ProxiesEnabled("jobs-type") {
		t.Fatal("proxy_schema must enable proxies")
	}
	if svc.CredentialsEnabled("no-such-type") || svc.ProxiesEnabled("no-such-type") {
		t.Fatal("unknown types must report disabled")
	}
}

func TestJobSpecsInvalid(t *testing.T) {
	cases := map[string]string{
		"missing run":      `refresh = { interval_seconds = 300 }`,
		"interval low":     `refresh = { interval_seconds = 30, run = function(ctx) return true end }`,
		"interval high":    `refresh = { interval_seconds = 90000, run = function(ctx) return true end }`,
		"run not function": `refresh = { interval_seconds = 300, run = 42 }`,
		"bad name":         `["has-dash"] = { interval_seconds = 300, run = function(ctx) return true end }`,
	}
	for name, jobsBody := range cases {
		svc := setupService(t)
		src := `--- @plugin Jobs Bad
--- @author tester
--- @version 1.0.0
--- @router_version 0.7.0
--- @allow_host example.com

llm_router.register("jobs-bad", {
  complete = function(ctx, request) return nil, { message = "no", code = "server_error" } end,
  jobs = { ` + jobsBody + ` },
})
`
		if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err == nil {
			t.Errorf("%s: expected install error", name)
		}
	}
}

func TestRunJobSuccess(t *testing.T) {
	svc := setupService(t)
	installJobsPlugin(t, svc)
	if err := svc.RunJob(context.Background(), "jobs-type", "refresh", "manual", "p1", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunJobUnknown(t *testing.T) {
	svc := setupService(t)
	installJobsPlugin(t, svc)
	if err := svc.RunJob(context.Background(), "jobs-type", "missing", "manual", "p1", nil); err == nil {
		t.Fatal("unknown job must fail")
	}
	if err := svc.RunJob(context.Background(), "no-such-type", "refresh", "manual", "p1", nil); err == nil {
		t.Fatal("unknown type must fail")
	}
}

func TestRunJobWritesCredentials(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin Jobs Writer
--- @author tester
--- @version 1.0.0
--- @router_version 0.7.0
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
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	updated := map[string]map[string]any{}
	svc.SetCredentialAccess(
		func(providerID string) ([]*models.Credential, error) {
			return []*models.Credential{{ID: "c1", ProviderID: "p1", Data: map[string]any{"refresh_token": "r"}}, {ID: "c2", ProviderID: "p1", Disabled: true}}, nil
		},
		func(id string) (*models.Credential, error) { return nil, nil },
		func(id string, data map[string]any) error { updated[id] = data; return nil },
	)
	if err := svc.RunJob(context.Background(), "jobs-writer", "refresh", "tick", "p1", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(updated) != 1 || updated["c1"]["access_token"] != "refreshed" {
		t.Fatalf("updated: %v", updated)
	}
}

func TestCredentialsTableGating(t *testing.T) {
	svc := setupService(t)
	installJobsPlugin(t, svc)
	// Request context: update must fail, list/get serve.
	svc.SetCredentialAccess(
		func(providerID string) ([]*models.Credential, error) {
			return []*models.Credential{{ID: "c1", Data: map[string]any{"api_key": "k"}}}, nil
		},
		func(id string) (*models.Credential, error) {
			return &models.Credential{ID: id, Data: map[string]any{}}, nil
		},
		func(id string, data map[string]any) error { return nil },
	)
	src := `--- @plugin Gate Probe
--- @author tester
--- @version 1.0.0
--- @router_version 0.7.0
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
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Complete(context.Background(), testMeta("gate-type", nil, "gate-type/m", nil),
		&models.ChatCompletionRequest{Model: "gate-type/m"})
	perr, ok := err.(*models.ProviderError)
	if !ok || !strings.Contains(perr.Message, "n=1") || !strings.Contains(perr.Message, "update_ok=false") {
		t.Fatalf("gating: %v", err)
	}
}

func TestStorageTTLExpiry(t *testing.T) {
	database := testutil.SetupTestDB(t)
	be := newStorageBackend(database)
	if err := be.set("p1", "s", "k", "v", 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got, err := be.get("p1", "s", "k"); err != nil || got != "v" {
		t.Fatalf("get: %v %v", got, err)
	}
	if err := be.set("p1", "s", "exp", "v", time.Second); err != nil {
		t.Fatalf("set ttl: %v", err)
	}
	if got, err := be.get("p1", "s", "exp"); err != nil || got != "v" {
		t.Fatalf("unexpired ttl row must read: %v %v", got, err)
	}
	if err := be.set("p1", "s", "neg", "v", -time.Second); err == nil {
		t.Fatal("negative ttl must fail")
	}
	// A raw expired envelope reads as missing and deletes the row.
	expired := `{"value":"stale","expires_at":"2000-01-01T00:00:00Z"}`
	if err := database.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(db.BucketPluginStorage).Put([]byte(storageKey("p1", "s", "gone")), []byte(expired))
	}); err != nil {
		t.Fatalf("seed expired: %v", err)
	}
	if got, err := be.get("p1", "s", "gone"); err != nil || got != nil {
		t.Fatalf("expired row must read missing: %v %v", got, err)
	}
	// Bare legacy rows (no envelope) keep serving without expiry.
	if err := database.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(db.BucketPluginStorage).Put([]byte(storageKey("p1", "s", "bare")), []byte(`"legacy"`))
	}); err != nil {
		t.Fatalf("seed bare: %v", err)
	}
	if got, err := be.get("p1", "s", "bare"); err != nil || got != "legacy" {
		t.Fatalf("bare row must serve: %v %v", got, err)
	}
}
