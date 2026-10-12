package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
)

const localeDashboardSource = `--- @plugin Locale Dashboard
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("locdash", {
  complete = function() end,
  locales = {
    ["en"] = { ["misc.secret"] = "Secret key" },
    ["ru"] = { ["misc.secret"] = "Секретный ключ" },
  },
  credential_schema = {
    { type = "secret", name = "api_key", label = "misc.secret" },
  },
  auth_initiate = function(ctx)
    return { render = { { type = "text", text = "misc.secret" } } }
  end,
  auth_step = function(ctx, input)
    return { render = { { type = "text", text = "misc.secret" } } }
  end,
})
`

func seedLocaleDashboard(t *testing.T) (*Handler, string) {
	t.Helper()
	h, providerService, database := newProvidersUIHandler(t)
	ctx := context.Background()
	luaService, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	if _, err := luaService.InstallSQLite(ctx, []byte(localeDashboardSource), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	providerService.SetLuaService(luaService)
	h.luaService = luaService
	if err := providerService.EnsureSeeded(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	providers, err := providerService.List(ctx)
	if err != nil {
		t.Fatalf("list providers: %v", err)
	}
	identifier := ""
	for _, instance := range providers {
		if instance.TypeKey == "locdash" {
			identifier = instance.ID
		}
	}
	if identifier == "" {
		t.Fatal("locdash provider missing")
	}
	return h, identifier
}

func decodeSchemaNodes(t *testing.T, recorder *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var body struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode schema: %v: %s", err, recorder.Body.String())
	}
	return body.Nodes
}

func TestDashboardSchemaLocaleQuery(t *testing.T) {
	h, identifier := seedLocaleDashboard(t)
	request := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers/"+identifier+"/credential-schema?locale=ru", nil)
	request.SetPathValue("id", identifier)
	recorder := httptest.NewRecorder()
	h.apiProviderCredentialSchema(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, body %s", recorder.Code, recorder.Body.String())
	}
	nodes := decodeSchemaNodes(t, recorder)
	if len(nodes) != 1 || nodes[0]["label"] != "Секретный ключ" {
		t.Fatalf("ru nodes wrong: %v", nodes)
	}
}

func TestDashboardSchemaLocaleHeaderFallback(t *testing.T) {
	h, identifier := seedLocaleDashboard(t)
	request := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers/"+identifier+"/credential-schema", nil)
	request.SetPathValue("id", identifier)
	request.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")
	recorder := httptest.NewRecorder()
	h.apiProviderCredentialSchema(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, body %s", recorder.Code, recorder.Body.String())
	}
	nodes := decodeSchemaNodes(t, recorder)
	if len(nodes) != 1 || nodes[0]["label"] != "Секретный ключ" {
		t.Fatalf("header ru nodes wrong: %v", nodes)
	}
}

func TestDashboardAuthLocaleQuery(t *testing.T) {
	h, identifier := seedLocaleDashboard(t)
	payload := `{"provider_id":"` + identifier + `"}`
	request := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/auth/initiate?locale=ru", strings.NewReader(payload))
	recorder := httptest.NewRecorder()
	h.authInitiate(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, body %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode auth: %v", err)
	}
	if len(body.Nodes) != 1 || body.Nodes[0]["text"] != "Секретный ключ" {
		t.Fatalf("ru auth nodes wrong: %v", body.Nodes)
	}
}
