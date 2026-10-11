package luaplugin

import (
	"context"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func installConfigurationProbe(t *testing.T) *Service {
	t.Helper()
	service, err := New(testutil.SetupTestSQLite(t), nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	src := `--- @plugin Configuration Probe
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("configprobe", {
  complete = function() end,
  auth_initiate = function(ctx)
    local v = ""
    if ctx.provider_config ~= nil then v = ctx.provider_config.base_url or "" end
    return { credentials = { base_url = v } }
  end,
  auth_step = function(ctx, input)
    local v = ""
    if ctx.provider_config ~= nil then v = ctx.provider_config.base_url or "" end
    return { credentials = { base_url = v, action = input.action } }
  end,
})
`
	if _, err := service.InstallSQLite(context.Background(), []byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install configuration probe: %v", err)
	}
	return service
}

func TestAuthCtxCarriesProviderConfig(t *testing.T) {
	service := installConfigurationProbe(t)
	ctx := context.Background()
	providerConfig := map[string]any{"base_url": "https://example.com/v1"}

	response, err := service.AuthInitiate(ctx, "configprobe", "flow1", providerConfig)
	if err != nil {
		t.Fatalf("auth initiate: %v", err)
	}
	if response.Credentials["base_url"] != "https://example.com/v1" {
		t.Errorf("initiate config wrong: %+v", response.Credentials)
	}

	response, err = service.AuthStep(ctx, "configprobe", "flow1", "next", map[string]any{}, providerConfig)
	if err != nil {
		t.Fatalf("auth step: %v", err)
	}
	if response.Credentials["base_url"] != "https://example.com/v1" {
		t.Errorf("step config wrong: %+v", response.Credentials)
	}
	if response.Credentials["action"] != "next" {
		t.Errorf("step action wrong: %+v", response.Credentials)
	}
}

func TestAuthCtxNilConfig(t *testing.T) {
	service := installConfigurationProbe(t)
	ctx := context.Background()

	response, err := service.AuthInitiate(ctx, "configprobe", "flow1", nil)
	if err != nil {
		t.Fatalf("auth initiate: %v", err)
	}
	if response.Credentials["base_url"] != "" {
		t.Errorf("nil config must yield empty value: %+v", response.Credentials)
	}
}
