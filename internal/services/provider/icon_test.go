package provider

import (
	"context"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

const iconTypePluginSource = `--- @plugin Icon Type Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("icon-type", {
  icon = "https://example.com/plugin.svg",
  complete = function(ctx, request)
    return nil, { message = "unreachable" }
  end,
})
`

func TestIconPrefersOwnThenPluginType(t *testing.T) {
	ctx := context.Background()
	database := testutil.SetupTestSQLite(t)
	luaService, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("new plugin service: %v", err)
	}
	if _, err := luaService.InstallSQLite(ctx, []byte(iconTypePluginSource), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	service := NewGorm(database)
	service.SetLuaService(luaService)
	cases := map[string]struct {
		instance models.ProviderInstance
		want     string
	}{
		"own icon wins":     {models.ProviderInstance{TypeKey: "icon-type", IconURL: "https://example.com/own.svg"}, "https://example.com/own.svg"},
		"plugin type icon":  {models.ProviderInstance{TypeKey: "icon-type"}, "https://example.com/plugin.svg"},
		"unknown type none": {models.ProviderInstance{TypeKey: "missing-type"}, ""},
	}
	for name, testCase := range cases {
		if got := service.Icon(&testCase.instance); got != testCase.want {
			t.Errorf("%s: got %q, want %q", name, got, testCase.want)
		}
	}
}

func TestIconWithoutPluginService(t *testing.T) {
	service := NewGorm(nil)
	if got := service.Icon(&models.ProviderInstance{TypeKey: "custom", IconURL: "https://example.com/own.svg"}); got != "https://example.com/own.svg" {
		t.Fatalf("got %q", got)
	}
	if got := service.Icon(&models.ProviderInstance{TypeKey: "custom"}); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
