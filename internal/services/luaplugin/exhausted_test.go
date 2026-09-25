package luaplugin

import (
	"context"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

const markPluginSource = `--- @plugin Mark Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.1.1
--- @allow_host example.com

llm_router.register("mark-type", {
  complete = function(ctx, credential, request)
    if request.user == "scoped" then
      return nil, { type = "rate_limit", message = "slow", retry_after = os.time() + 3600, scope = { "account" } }
    elseif request.user == "full" then
      return nil, { type = "rate_limit", message = "slow", retry_after = os.time() + 3600 }
    elseif request.user == "upstream" then
      return nil, { type = "upstream", message = "boom" }
    end
    return nil, { type = "rate_limit", message = "slow" }
  end,
})
`

func setupMarkService(t *testing.T) (*Service, *exhausted.Service, string) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	svc, err := New(database, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := svc.Install([]byte(markPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	exSvc := exhausted.New(database)
	svc.SetExhaustedStore(exSvc)
	rec, err := svc.Lookup("mark-type")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	return svc, exSvc, rec.ID
}

func markComplete(t *testing.T, svc *Service, mode string) error {
	t.Helper()
	user := mode
	req := &models.ChatCompletionRequest{
		Model:    "mark-type/m",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
		User:     &user,
	}
	_, err := svc.Complete(context.Background(), testMeta("mark-type",
		&models.Credential{ID: "c1"}, "mark-type/m", nil), req)
	return err
}

func TestMarkDirectSanity(t *testing.T) {
	_, exSvc, pluginID := setupMarkService(t)
	key, err := exhausted.KeyFromScope(pluginID, "mark-type", "c1", "mark-type/m", "", []string{"account"})
	if err != nil {
		t.Fatalf("scope key: %v", err)
	}
	if err := exSvc.Mark(key, time.Now().Add(time.Hour), "direct"); err != nil {
		t.Fatalf("direct mark: %v", err)
	}
	hit, err := exSvc.LimitedAny(exhausted.Segments{Plugin: pluginID, Provider: "mark-type", Account: "c1", Model: "mark-type/other"})
	if err != nil || hit == "" {
		t.Fatalf("direct mark must match: %q, %v", hit, err)
	}
}

func TestMarkScopedKey(t *testing.T) {
	svc, exSvc, pluginID := setupMarkService(t)
	if err := markComplete(t, svc, "scoped"); !isProviderType(err, models.ErrorTypeRateLimit) {
		t.Fatalf("fixture must return rate_limit, got %v", err)
	}

	key, err := exhausted.KeyFromScope(pluginID, "mark-type", "c1", "mark-type/m", "", []string{"account"})
	if err != nil {
		t.Fatalf("scope key: %v", err)
	}
	if limited, err := exSvc.Limited(key); err != nil || !limited {
		t.Fatalf("exact scoped key must limit: %v, %v", limited, err)
	}
	same := exhausted.Segments{Plugin: pluginID, Provider: "mark-type", Account: "c1", Model: "mark-type/other"}
	if hit, err := exSvc.LimitedAny(same); err != nil || hit == "" {
		t.Fatalf("account scope must match other models: %q, %v", hit, err)
	}
	other := exhausted.Segments{Plugin: pluginID, Provider: "mark-type", Account: "c2", Model: "mark-type/m"}
	if hit, err := exSvc.LimitedAny(other); err != nil || hit != "" {
		t.Fatalf("other account must pass: %q, %v", hit, err)
	}
}

func TestMarkFullKeyWithoutScope(t *testing.T) {
	svc, exSvc, pluginID := setupMarkService(t)
	markComplete(t, svc, "full")

	full := exhausted.Segments{Plugin: pluginID, Provider: "mark-type", Account: "c1", Model: "mark-type/m"}
	if hit, err := exSvc.LimitedAny(full); err != nil || hit == "" {
		t.Fatalf("full combination must match: %q, %v", hit, err)
	}
	proxyOnly := exhausted.Segments{Plugin: pluginID, Provider: "mark-type", Proxy: "px-1"}
	if hit, err := exSvc.LimitedAny(proxyOnly); err != nil || hit != "" {
		t.Fatalf("proxy candidate must not match account/model key: %q, %v", hit, err)
	}
}

func TestMarkNonRateTypesIgnored(t *testing.T) {
	svc, exSvc, pluginID := setupMarkService(t)
	markComplete(t, svc, "upstream")

	any := exhausted.Segments{Plugin: pluginID, Provider: "mark-type", Account: "c1", Model: "mark-type/m"}
	if hit, err := exSvc.LimitedAny(any); err != nil || hit != "" {
		t.Fatalf("upstream must not mark: %q, %v", hit, err)
	}
}

func TestMarkRateWithoutHintDefaultsMinute(t *testing.T) {
	svc, exSvc, pluginID := setupMarkService(t)
	markComplete(t, svc, "bare")

	any := exhausted.Segments{Plugin: pluginID, Provider: "mark-type", Account: "c1", Model: "mark-type/m"}
	hit, err := exSvc.LimitedAny(any)
	if err != nil || hit == "" {
		t.Fatalf("hintless rate must mark: %q, %v", hit, err)
	}
}
