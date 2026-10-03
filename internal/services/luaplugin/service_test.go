package luaplugin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

const testPluginSource = `--- @plugin Test Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.7.0
--- @description Test plugin
--- @allow_host example.com

llm_router.register("test-type", {
  complete = function(ctx, request)
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
      return false, { message = "api_key required", code = "invalid_request_error" }
    end
    return true
  end,

  get_model_infos = function(ctx)
    return { { name = "model-a", display_name = "Model A" } }
  end,

  credential_schema = {
    { type = "input", name = "api_key", input_type = "password", label = "API Key", required = true },
    { type = "button", text = "Save", form_action = "submit" },
  },
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

const iconPluginSource = `--- @plugin Icon Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.7.0
--- @description Icon plugin
--- @allow_host example.com

llm_router.register("icon-type", {
  icon = "https://example.com/icon.svg",
  complete = function(ctx, request)
    return {
      id = "chatcmpl-icon",
      object = "chat.completion",
      created = 1700000000,
      model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = "hi" }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,
})
`

func TestInstallCapturesIcon(t *testing.T) {
	svc := setupService(t)
	rec, err := svc.Install([]byte(iconPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if got := rec.Icons["icon-type"]; got != "https://example.com/icon.svg" {
		t.Fatalf("record icon: got %q", got)
	}
	if got := svc.Icon("icon-type"); got != "https://example.com/icon.svg" {
		t.Fatalf("registry icon: got %q", got)
	}
}

func TestInstallRejectsBadIcon(t *testing.T) {
	svc := setupService(t)
	cases := map[string]string{
		"plain string":   `icon = "not a url"`,
		"wrong scheme":   `icon = "ftp://example.com/icon.svg"`,
		"http not https": `icon = "http://example.com/icon.svg"`,
		"non-string":     `icon = 42`,
		"oversized data": `icon = "data:image/svg+xml,` + strings.Repeat("a", 40<<10) + `"`,
		"data non-image": `icon = "data:text/plain,hello"`,
	}
	for name, iconLine := range cases {
		src := strings.Replace(iconPluginSource, `icon = "https://example.com/icon.svg",`, iconLine+",", 1)
		if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err == nil {
			t.Errorf("%s: expected install error", name)
		}
	}
}

func TestRollbackRestoresIcon(t *testing.T) {
	svc := setupService(t)
	rec, err := svc.Install([]byte(iconPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install v1: %v", err)
	}
	v2 := strings.Replace(iconPluginSource, "@version 1.0.0", "@version 2.0.0", 1)
	v2 = strings.Replace(v2, `icon = "https://example.com/icon.svg",`, `icon = "https://example.com/icon2.svg",`, 1)
	if _, err := svc.Install([]byte(v2), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install v2: %v", err)
	}
	if got := svc.Icon("icon-type"); got != "https://example.com/icon2.svg" {
		t.Fatalf("v2 icon: got %q", got)
	}
	rolled, err := svc.Rollback(rec.ID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := rolled.Icons["icon-type"]; got != "https://example.com/icon.svg" {
		t.Fatalf("rolled-back icon: got %q", got)
	}
	if got := svc.Icon("icon-type"); got != "https://example.com/icon.svg" {
		t.Fatalf("registry icon after rollback: got %q", got)
	}
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
	src := "--- @plugin P\n--- @author a\n--- @version 1.0.0\n--- @router_version 0.7.0\n--- @allow_host *\n"
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
		"--- @plugin P\n--- @author a\n--- @version 1.0.0\n--- @router_version 0.7.0\n",
	} {
		if _, err := ParseManifest([]byte(src)); err == nil {
			t.Fatalf("expected error for %q", src)
		}
	}
}

func TestCheckRouterVersion(t *testing.T) {
	m := &Manifest{RouterVersion: "0.7.0"}
	if err := CheckRouterVersion(m, "0.7.0"); err != nil {
		t.Fatalf("equal versions: %v", err)
	}
	m.RouterVersion = "99.0.0"
	if err := CheckRouterVersion(m, "0.7.0"); err == nil {
		t.Fatal("expected rejection of newer router requirement")
	}
	for _, old := range []string{"0.6.0", "0.5.3", "0.3.0", "0.1.1"} {
		m.RouterVersion = old
		if err := CheckRouterVersion(m, "0.7.0"); err == nil {
			t.Fatalf("pre-0.7.0 contract %s must be rejected", old)
		}
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

	resp, err := svc.Complete(context.Background(), testMeta("test-type",
		&models.Credential{ID: "c1", Data: map[string]any{"api_key": "k"}}, "test-type/model-a", nil),
		&models.ChatCompletionRequest{Model: "test-type/model-a", Messages: []models.ChatMessage{{Role: "user", Content: "hi"}}})
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
--- @router_version 0.7.0
--- @allow_host example.com

local x = os.execute("echo hi")
llm_router.register("bad", { complete = function() end })
`
	if _, err := svc.Install([]byte(bad), PluginOrigin{Manual: true}); err == nil {
		t.Fatal("expected install failure for os.execute access")
	} else if !strings.Contains(err.Error(), "non-function") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestErrorContract(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @router_version 0.7.0
--- @allow_host example.com

llm_router.register("err-type", {
  complete = function(ctx, request)
    return nil, { message = "slow down", code = "rate_limit", status = 429 }
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Complete(context.Background(), testMeta("err-type",
		&models.Credential{ID: "c1"}, "x/y", nil), &models.ChatCompletionRequest{Model: "x/y"})
	perr, ok := err.(*models.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T (%v)", err, err)
	}
	if perr.Code != "rate_limit" || perr.StatusCode != 429 || perr.Message != "slow down" {
		t.Fatalf("terminal: %+v", perr)
	}
}

func TestErrorContractDefaults(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @router_version 0.7.0
--- @allow_host example.com

llm_router.register("scope-type", {
  complete = function(ctx, request)
    return nil, { message = "out", code = "insufficient_quota", param = "model" }
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Complete(context.Background(), testMeta("scope-type",
		&models.Credential{ID: "c1"}, "x/y", nil), &models.ChatCompletionRequest{Model: "x/y"})
	perr, ok := err.(*models.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T (%v)", err, err)
	}
	if perr.Code != "insufficient_quota" || perr.Param != "model" || perr.StatusCode != 502 {
		t.Fatalf("defaults: %+v", perr)
	}
}

func TestErrorContractMissingMessageIsInternal(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @router_version 0.7.0
--- @allow_host example.com

llm_router.register("badscope-type", {
  complete = function(ctx, request)
    return nil, { code = "rate_limit" }
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Complete(context.Background(), testMeta("badscope-type",
		&models.Credential{ID: "c1"}, "x/y", nil), &models.ChatCompletionRequest{Model: "x/y"})
	var ierr *models.PluginInternalError
	if !errors.As(err, &ierr) {
		t.Fatalf("missing message must fail closed, got %T (%v)", err, err)
	}
}

func TestErrorContractPaymentRequired(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @router_version 0.7.0
--- @allow_host example.com

llm_router.register("pay-type", {
  complete = function(ctx, request)
    return nil, { message = "subscription required", code = "payment_required", status = 402 }
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Complete(context.Background(), testMeta("pay-type",
		&models.Credential{ID: "c1"}, "x/y", nil), &models.ChatCompletionRequest{Model: "x/y"})
	perr, ok := err.(*models.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T (%v)", err, err)
	}
	if perr.Code != "payment_required" || perr.StatusCode != 402 {
		t.Fatalf("payment: %+v", perr)
	}
}

func TestRuntimeCrashIsInternal(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @router_version 0.7.0
--- @allow_host example.com

llm_router.register("crash-type", {
  complete = function(ctx, request)
    local x = nil
    return x.field
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := svc.Complete(context.Background(), testMeta("crash-type",
		&models.Credential{ID: "c1"}, "x/y", nil), &models.ChatCompletionRequest{Model: "x/y"})
	perr, ok := err.(*models.PluginInternalError)
	if !ok {
		t.Fatalf("expected PluginInternalError, got %T (%v)", err, err)
	}
	if perr.Cause == "" {
		t.Fatal("plugin crash must carry a cause")
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

func TestLookupResolvesInstalled(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(testPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := svc.Lookup("test-type"); err != nil {
		t.Fatalf("installed plugin must resolve: %v", err)
	}
}
