package router

import (
	"context"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	"log/slog"
)

const healthProbePluginSource = `--- @plugin Health Probe
--- @author tester
--- @version 1.0.0
--- @router_version 0.3.0
--- @allow_host example.com

llm_router.register("health-probe-type", {
  complete = function(ctx, credential, request)
    return nil, { type = "upstream", message = "unused" }
  end,

  check_health = function(ctx, credential)
    local key = credential.data.api_key or ""
    if key == "dead-key" then
      return { status = "unhealthy", message = "key revoked" }
    elseif key == "flaky-key" then
      return { status = "unknown", message = "try later" }
    end
    return { status = "healthy" }
  end,

  validate_credentials = function(data)
    return true
  end,
})
`

func setupHealthProbeRouter(t *testing.T) (*Service, string, *models.Credential, *models.Credential, *models.Credential) {
	t.Helper()
	database := testutil.SetupTestDB(t)

	providerSvc := provider.NewService(database)
	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("new plugin service: %v", err)
	}
	providerSvc.SetLuaService(luaSvc)
	if _, err := luaSvc.Install([]byte(healthProbePluginSource), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install health plugin: %v", err)
	}
	inst, err := providerSvc.Create(provider.CreateOptions{Name: "Health", TypeKey: "health-probe-type"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	credSvc := credential.New(database, providerSvc)
	add := func(label, key string) *models.Credential {
		t.Helper()
		cred, err := credSvc.Add(credential.AddOptions{ProviderID: inst.ID, Label: label, Data: map[string]any{"api_key": key}})
		if err != nil {
			t.Fatalf("add cred %s: %v", label, err)
		}
		return cred
	}
	live := add("live", "live-key")
	dead := add("dead", "dead-key")
	flaky := add("flaky", "flaky-key")
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)
	routerSvc := New(providerSvc, credSvc, modelInfoSvc, exhausted.New(database), videojobs.New(database), nil, nil, slog.Default())
	return routerSvc, inst.ID, live, dead, flaky
}

func TestRouterService_TestCredentialHealthy(t *testing.T) {
	svc, providerID, live, _, _ := setupHealthProbeRouter(t)
	res := svc.TestCredential(context.Background(), providerID, live.ID, "")
	if !res.OK {
		t.Fatalf("healthy must pass: %+v", res)
	}
	got, err := svc.credSvc.Get(live.ID)
	if err != nil || got.Disabled {
		t.Fatalf("healthy must stay enabled: %+v %v", got, err)
	}
}

func TestRouterService_TestCredentialUnhealthyDisables(t *testing.T) {
	svc, providerID, _, dead, _ := setupHealthProbeRouter(t)
	res := svc.TestCredential(context.Background(), providerID, dead.ID, "")
	if res.OK || res.Code != "unhealthy" {
		t.Fatalf("unhealthy must fail with code: %+v", res)
	}
	got, err := svc.credSvc.Get(dead.ID)
	if err != nil || !got.Disabled || got.DisabledBy != "healthcheck" {
		t.Fatalf("unhealthy must disable by healthcheck: %+v %v", got, err)
	}
}

func TestRouterService_TestCredentialUnknownKeeps(t *testing.T) {
	svc, providerID, _, _, flaky := setupHealthProbeRouter(t)
	res := svc.TestCredential(context.Background(), providerID, flaky.ID, "")
	if res.OK || res.Code != "unknown" {
		t.Fatalf("unknown must fail without disable: %+v", res)
	}
	got, err := svc.credSvc.Get(flaky.ID)
	if err != nil || got.Disabled {
		t.Fatalf("unknown must stay enabled: %+v %v", got, err)
	}
}

func TestRouterService_TestCredentialUnsupported(t *testing.T) {
	svc, _, _, _, _ := setupHealthProbeRouter(t)
	if _, err := svc.providerSvc.LuaService().Install([]byte(exhEchoPluginSource), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install echo plugin: %v", err)
	}
	inst, err := svc.providerSvc.Create(provider.CreateOptions{Name: "Plain", TypeKey: "exh-type"})
	if err != nil {
		t.Fatalf("create plain provider: %v", err)
	}
	cred, err := svc.credSvc.Add(credential.AddOptions{ProviderID: inst.ID, Label: "p", Data: map[string]any{"api_key": "key-p"}})
	if err != nil {
		t.Fatalf("add cred: %v", err)
	}
	res := svc.TestCredential(context.Background(), inst.ID, cred.ID, "")
	if res.OK || res.Code != "unsupported" {
		t.Fatalf("handler-less type must report unsupported: %+v", res)
	}
	got, err := svc.credSvc.Get(cred.ID)
	if err != nil || got.Disabled {
		t.Fatalf("unsupported must stay enabled: %+v %v", got, err)
	}
}
