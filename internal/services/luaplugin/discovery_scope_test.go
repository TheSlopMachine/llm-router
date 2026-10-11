package luaplugin

import (
	"context"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// Discovery must scope credentials.list() to the requesting provider:
// a credential stored under another provider ID is invisible. Locks the
// providerID threading through GetModelInfos into the sandbox.
const scopePluginSource = `--- @plugin Scope Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("scope-type", {
  complete = function(ctx, request)
    return nil, { message = "no", code = "server_error" }
  end,
  credential_schema = {
    { type = "secret", name = "api_key", label = "API Key" },
  },
  get_model_infos = function(ctx)
    local ids = {}
    for _, c in ipairs(llm_router.credentials.list()) do
      table.insert(ids, c.id)
    end
    table.sort(ids)
    return { { name = "seen:" .. table.concat(ids, ",") } }
  end,
})
`

func TestGetModelInfosScopesCredentialsByProvider(t *testing.T) {
	service := setupService(t)
	if _, err := service.InstallSQLite(context.Background(), []byte(scopePluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	rows := map[string][]*models.Credential{
		"provider-a": {
			{ID: "a2", ProviderID: "provider-a", Data: map[string]any{"api_key": "k2"}},
			{ID: "a1", ProviderID: "provider-a", Data: map[string]any{"api_key": "k1"}},
		},
		"provider-b": {
			{ID: "b1", ProviderID: "provider-b", Data: map[string]any{"api_key": "k3"}},
		},
	}
	var seenScopes []string
	service.SetCredentialAccess(
		func(ctx context.Context, providerID string) ([]*models.Credential, error) {
			seenScopes = append(seenScopes, providerID)
			return rows[providerID], nil
		},
		func(ctx context.Context, id string) (*models.Credential, error) { return nil, nil },
		func(ctx context.Context, id string, data map[string]any) error { return nil },
	)

	modelInfos, err := service.GetModelInfos(t.Context(), "provider-a", "scope-type", nil)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if len(modelInfos) != 1 || modelInfos[0].Name != "seen:a1,a2" {
		t.Fatalf("provider-a sees: %+v", modelInfos)
	}

	modelInfos, err = service.GetModelInfos(t.Context(), "provider-b", "scope-type", nil)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if len(modelInfos) != 1 || modelInfos[0].Name != "seen:b1" {
		t.Fatalf("provider-b sees: %+v", modelInfos)
	}

	if len(seenScopes) != 2 || seenScopes[0] != "provider-a" || seenScopes[1] != "provider-b" {
		t.Fatalf("credential scopes queried: %v", seenScopes)
	}
}
