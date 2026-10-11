package router

import (
	"context"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
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
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("health-probe-type", {
  complete = function(ctx, request)
    return nil, { message = "unused", code = "server_error" }
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

	providerService := provider.NewService(database)
	luaService, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("new plugin service: %v", err)
	}
	providerService.SetLuaService(luaService)
	if _, err := luaService.Install([]byte(healthProbePluginSource), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install health plugin: %v", err)
	}
	instance, err := providerService.Create(provider.CreateOptions{Name: "Health", TypeKey: "health-probe-type"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	credentialService := credential.New(database, providerService)
	add := func(label, key string) *models.Credential {
		t.Helper()
		credential, err := credentialService.Add(credential.AddOptions{ProviderID: instance.ID, Label: label, Data: map[string]any{"api_key": key}})
		if err != nil {
			t.Fatalf("add credential %s: %v", label, err)
		}
		return credential
	}
	live := add("live", "live-key")
	dead := add("dead", "dead-key")
	flaky := add("flaky", "flaky-key")
	modelInfoService := modelinfo.New(database, providerService, credentialService, 1*time.Hour)
	routerService := New(providerService, credentialService, modelInfoService, videojobs.New(database), nil, nil, slog.Default())
	return routerService, instance.ID, live, dead, flaky
}

func TestRouterService_TestCredentialHealthy(t *testing.T) {
	service, providerID, live, _, _ := setupHealthProbeRouter(t)
	result := service.TestCredential(context.Background(), providerID, live.ID, "")
	if !result.OK {
		t.Fatalf("healthy must pass: %+v", result)
	}
	got, err := service.credentialService.Get(live.ID)
	if err != nil || got.Disabled {
		t.Fatalf("healthy must stay enabled: %+v %v", got, err)
	}
}

func TestRouterService_TestCredentialUnhealthyDisables(t *testing.T) {
	service, providerID, _, dead, _ := setupHealthProbeRouter(t)
	if _, err := service.providerService.Update(providerID, provider.UpdateOptions{
		Name:   "Health",
		Config: map[string]any{"disable_failed_credentials": true},
	}); err != nil {
		t.Fatalf("enable automation: %v", err)
	}
	result := service.TestCredential(context.Background(), providerID, dead.ID, "")
	if result.OK || result.Code != "unhealthy" {
		t.Fatalf("unhealthy must fail with code: %+v", result)
	}
	got, err := service.credentialService.Get(dead.ID)
	if err != nil || !got.Disabled || got.DisabledBy != "healthcheck" {
		t.Fatalf("unhealthy must disable by healthcheck: %+v %v", got, err)
	}
}

func TestRouterService_TestCredentialUnhealthyKeepsWhenAutomationOff(t *testing.T) {
	service, providerID, _, dead, _ := setupHealthProbeRouter(t)
	result := service.TestCredential(context.Background(), providerID, dead.ID, "")
	if result.OK || result.Code != "unhealthy" {
		t.Fatalf("unhealthy must fail with code: %+v", result)
	}
	if result.Summary != "credential unhealthy" {
		t.Fatalf("summary must not claim a disable: %+v", result)
	}
	got, err := service.credentialService.Get(dead.ID)
	if err != nil || got.Disabled {
		t.Fatalf("automation off must keep the credential enabled: %+v %v", got, err)
	}
}

func TestRouterService_TestCredentialUnknownKeeps(t *testing.T) {
	service, providerID, _, _, flaky := setupHealthProbeRouter(t)
	result := service.TestCredential(context.Background(), providerID, flaky.ID, "")
	if result.OK || result.Code != "unknown" {
		t.Fatalf("unknown must fail without disable: %+v", result)
	}
	got, err := service.credentialService.Get(flaky.ID)
	if err != nil || got.Disabled {
		t.Fatalf("unknown must stay enabled: %+v %v", got, err)
	}
}

func TestRouterService_TestCredentialUnsupported(t *testing.T) {
	service, _, _, _, _ := setupHealthProbeRouter(t)
	if _, err := service.providerService.LuaService().Install([]byte(`--- @plugin Plain Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("plain-type", {
  complete = function(ctx, request)
    return nil, { message = "plain", code = "server_error" }
  end,
  validate_credentials = function(data) return true end,
})`), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install plain plugin: %v", err)
	}
	instance, err := service.providerService.Create(provider.CreateOptions{Name: "Plain", TypeKey: "plain-type"})
	if err != nil {
		t.Fatalf("create plain provider: %v", err)
	}
	credential, err := service.credentialService.Add(credential.AddOptions{ProviderID: instance.ID, Label: "p", Data: map[string]any{"api_key": "key-p"}})
	if err != nil {
		t.Fatalf("add credential: %v", err)
	}
	result := service.TestCredential(context.Background(), instance.ID, credential.ID, "")
	if result.OK || result.Code != "unsupported" {
		t.Fatalf("handler-less type must report unsupported: %+v", result)
	}
	got, err := service.credentialService.Get(credential.ID)
	if err != nil || got.Disabled {
		t.Fatalf("unsupported must stay enabled: %+v %v", got, err)
	}
}
