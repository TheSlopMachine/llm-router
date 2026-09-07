package luaplugin

import (
	"context"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

const testPluginSource = `--- @plugin Test Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.0.4
--- @description Test plugin
--- @allow_host example.com

llm_router.register("test-type", {
  complete = function(ctx, credential, request)
    return {
      id = "chatcmpl-test",
      object = "chat.completion",
      created = 1700000000,
      model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = "hello" }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,

  validate_credentials = function(data)
    if data.api_key == nil or data.api_key == "" then
      return false, { type = "invalid_request", message = "api_key required" }
    end
    return true
  end,

  get_model_infos = function(ctx, credential, provider_config)
    return { { name = "model-a", display_name = "Model A" } }
  end,

  credential_schema = function()
    return {
      { type = "input", name = "api_key", input_type = "password", label = "API Key", required = true },
      { type = "button", text = "Save", form_action = "submit" },
    }
  end,
})
`

func setupService(t *testing.T) *Service {
	t.Helper()
	svc, err := New(testutil.SetupTestDB(t), nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svc
}

func TestParseManifest(t *testing.T) {
	m, err := ParseManifest([]byte(testPluginSource))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Plugin != "Test Plugin" || m.Author != "tester" || m.Version != "1.0.0" {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if len(m.AllowHosts) != 1 || m.AllowHosts[0] != "example.com" {
		t.Fatalf("allow hosts: %v", m.AllowHosts)
	}
	if m.Unsafe {
		t.Fatal("should not be unsafe")
	}
}

func TestParseManifestWildcard(t *testing.T) {
	src := "--- @plugin P\n--- @author a\n--- @version 1.0.0\n--- @router_version 0.0.4\n--- @allow_host *\n"
	m, err := ParseManifest([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !m.Unsafe {
		t.Fatal("wildcard must set unsafe")
	}
}

func TestParseManifestMissing(t *testing.T) {
	for _, src := range []string{
		"--- @plugin P\n--- @author a\n",
		"print('no header')\n",
		"--- @plugin P\n--- @author a\n--- @version 1.0.0\n--- @router_version 0.0.4\n",
	} {
		if _, err := ParseManifest([]byte(src)); err == nil {
			t.Fatalf("expected error for %q", src)
		}
	}
}

func TestCheckRouterVersion(t *testing.T) {
	m := &Manifest{RouterVersion: "0.0.4"}
	if err := CheckRouterVersion(m, "0.0.4"); err != nil {
		t.Fatalf("equal versions: %v", err)
	}
	m.RouterVersion = "99.0.0"
	if err := CheckRouterVersion(m, "0.0.4"); err == nil {
		t.Fatal("expected rejection of newer router requirement")
	}
	m.RouterVersion = "0.0.3"
	if err := CheckRouterVersion(m, "0.0.4"); err != nil {
		t.Fatalf("older requirement must pass: %v", err)
	}
}

func TestInstallAndComplete(t *testing.T) {
	svc := setupService(t)
	rec, err := svc.Install([]byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if rec.ID != "manual/tester/Test-Plugin" {
		t.Fatalf("id: %q", rec.ID)
	}
	if len(rec.TypeKeys) != 1 || rec.TypeKeys[0] != "test-type" {
		t.Fatalf("type keys: %v", rec.TypeKeys)
	}
	if _, err := svc.Lookup("test-type"); err != nil {
		t.Fatalf("lookup: %v", err)
	}

	resp, err := svc.Complete(context.Background(), "test-type",
		&models.Credential{ID: "c1", Data: map[string]any{"api_key": "k"}},
		&models.ChatCompletionRequest{Model: "test-type/model-a", Messages: []models.ChatMessage{{Role: "user", Content: "hi"}}},
		nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestValidateCredentials(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(testPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	ok, err := svc.ValidateCredentials("test-type", map[string]any{"api_key": "secret"})
	if err != nil || !ok {
		t.Fatalf("valid creds rejected: ok=%v err=%v", ok, err)
	}
	ok, err = svc.ValidateCredentials("test-type", map[string]any{})
	if err == nil || ok {
		t.Fatalf("invalid creds accepted: ok=%v err=%v", ok, err)
	}
}

func TestSandboxDeniesUnsafeGlobals(t *testing.T) {
	svc := setupService(t)
	bad := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @router_version 0.0.4
--- @allow_host example.com

local x = os.execute("echo hi")
llm_router.register("bad", { complete = function() end })
`
	if _, err := svc.Install([]byte(bad), PluginOrigin{Manual: true}); err == nil {
		t.Fatal("expected install failure for os.execute access")
	} else if !strings.Contains(err.Error(), "nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestErrorContract(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @router_version 0.0.4
--- @allow_host example.com

llm_router.register("err-type", {
  complete = function(ctx, credential, request)
    return nil, { type = "rate_limit", message = "slow down" }
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Complete(context.Background(), "err-type",
		&models.Credential{ID: "c1"}, &models.ChatCompletionRequest{Model: "x/y"}, nil)
	perr, ok := err.(*models.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T (%v)", err, err)
	}
	if perr.Type != models.ErrorTypeRateLimit {
		t.Fatalf("type: %v", perr.Type)
	}
}

func TestRuntimeCrashIsInternal(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @router_version 0.0.4
--- @allow_host example.com

llm_router.register("crash-type", {
  complete = function(ctx, credential, request)
    local x = nil
    return x.field
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Complete(context.Background(), "crash-type",
		&models.Credential{ID: "c1"}, &models.ChatCompletionRequest{Model: "x/y"}, nil)
	perr, ok := err.(*models.PluginInternalError)
	if !ok {
		t.Fatalf("expected PluginInternalError, got %T (%v)", err, err)
	}
	if !perr.Retryable() {
		t.Fatal("plugin crash must be retryable")
	}
	if len(svc.Crashes("manual/a/P")) == 0 {
		t.Fatal("crash must be recorded")
	}
}

func TestRollback(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(testPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install v1: %v", err)
	}
	v2 := strings.Replace(testPluginSource, "@version 1.0.0", "@version 2.0.0", 1)
	rec2, err := svc.Install([]byte(v2), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install v2: %v", err)
	}
	if rec2.Version != "2.0.0" || len(rec2.History) != 1 {
		t.Fatalf("unexpected v2 record: %+v", rec2)
	}
	rolled, err := svc.Rollback(rec2.ID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolled.Version != "1.0.0" {
		t.Fatalf("version after rollback: %q", rolled.Version)
	}
}

func TestEnableDisable(t *testing.T) {
	svc := setupService(t)
	rec, err := svc.Install([]byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := svc.Disable(rec.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := svc.Lookup("test-type"); err == nil {
		t.Fatal("disabled plugin must not resolve")
	}
	if _, err := svc.Enable(rec.ID); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := svc.Lookup("test-type"); err != nil {
		t.Fatalf("enabled plugin must resolve: %v", err)
	}
}
