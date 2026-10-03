package luaplugin

import (
	"context"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func installCfgProbe(t *testing.T) *Service {
	t.Helper()
	svc, err := New(testutil.SetupTestDB(t), nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	src := `--- @plugin Cfg Probe
--- @author tester
--- @version 1.0.0
--- @router_version 0.7.0
--- @allow_host example.com

llm_router.register("cfgprobe", {
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
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install cfg probe: %v", err)
	}
	return svc
}

func TestAuthCtxCarriesProviderConfig(t *testing.T) {
	svc := installCfgProbe(t)
	ctx := context.Background()
	cfg := map[string]any{"base_url": "https://example.com/v1"}

	res, err := svc.AuthInitiate(ctx, "cfgprobe", "flow1", cfg)
	if err != nil {
		t.Fatalf("auth initiate: %v", err)
	}
	if res.Credentials["base_url"] != "https://example.com/v1" {
		t.Errorf("initiate config wrong: %+v", res.Credentials)
	}

	res, err = svc.AuthStep(ctx, "cfgprobe", "flow1", "next", map[string]any{}, cfg)
	if err != nil {
		t.Fatalf("auth step: %v", err)
	}
	if res.Credentials["base_url"] != "https://example.com/v1" {
		t.Errorf("step config wrong: %+v", res.Credentials)
	}
	if res.Credentials["action"] != "next" {
		t.Errorf("step action wrong: %+v", res.Credentials)
	}
}

func TestAuthCtxNilConfig(t *testing.T) {
	svc := installCfgProbe(t)
	ctx := context.Background()

	res, err := svc.AuthInitiate(ctx, "cfgprobe", "flow1", nil)
	if err != nil {
		t.Fatalf("auth initiate: %v", err)
	}
	if res.Credentials["base_url"] != "" {
		t.Errorf("nil config must yield empty value: %+v", res.Credentials)
	}
}
