package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/agent"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	agentsadapter "github.com/TheSlopMachine/llm-router/providers/agents"
)

func newProvidersUIHandler(t *testing.T) (*Handler, *provider.Service, *db.DB) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	return &Handler{providerSvc: providerSvc}, providerSvc, database
}

func seedUIRows(t *testing.T, svc *provider.Service, database *db.DB) {
	t.Helper()
	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	const src = `--- @plugin Seed Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.0.4
--- @allow_host example.com

llm_router.register("zen", {
  complete = function() end,
})
`
	if _, err := luaSvc.Install([]byte(src), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	svc.SetLuaService(luaSvc)
	if err := svc.EnsureSeeded(); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func decodeProvidersList(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return out
}

func TestProvidersListExcludesHidden(t *testing.T) {
	h, svc, database := newProvidersUIHandler(t)
	seedUIRows(t, svc, database)

	req := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers", nil)
	rec := httptest.NewRecorder()
	h.apiProvidersList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, body %s", rec.Code, rec.Body.String())
	}
	out := decodeProvidersList(t, rec)
	if len(out) != 1 {
		t.Fatalf("expected 1 visible provider, got %d: %v", len(out), out)
	}
	if out[0]["id"] != "zen" {
		t.Fatalf("expected zen row, got %v", out[0]["id"])
	}
	if out[0]["is_ui_readonly"] != true {
		t.Fatalf("view must carry is_ui_readonly: %v", out[0])
	}
}

func TestProvidersUpdateGuards(t *testing.T) {
	h, svc, database := newProvidersUIHandler(t)
	seedUIRows(t, svc, database)

	call := func(id, body string) int {
		req := httptest.NewRequest(http.MethodPut, "/api/llm-router/dashboard/providers/"+id, strings.NewReader(body))
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		h.apiProvidersUpdate(rec, req)
		return rec.Code
	}

	if code := call("zen", `{"name":"Zen 2"}`); code != http.StatusForbidden {
		t.Errorf("readonly update: got %d, want 403", code)
	}
	if code := call("agents", `{"name":"Agents 2"}`); code != http.StatusNotFound {
		t.Errorf("hidden update: got %d, want 404", code)
	}
	if code := call("missing", `{"name":"X"}`); code != http.StatusNotFound {
		t.Errorf("missing update: got %d, want 404", code)
	}
}

func TestProvidersDeleteGuards(t *testing.T) {
	h, svc, database := newProvidersUIHandler(t)
	seedUIRows(t, svc, database)

	call := func(id string) int {
		req := httptest.NewRequest(http.MethodDelete, "/api/llm-router/dashboard/providers/"+id, nil)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		h.apiProvidersDelete(rec, req)
		return rec.Code
	}

	if code := call("zen"); code != http.StatusForbidden {
		t.Errorf("readonly delete: got %d, want 403", code)
	}
	if code := call("agents"); code != http.StatusNotFound {
		t.Errorf("hidden delete: got %d, want 404", code)
	}
	if _, err := svc.Get("zen"); err != nil {
		t.Errorf("readonly row must survive blocked delete: %v", err)
	}
	if _, err := svc.Get("agents"); err != nil {
		t.Errorf("hidden row must survive blocked delete: %v", err)
	}
}

func TestProviderSchemasHidden404(t *testing.T) {
	h, svc, database := newProvidersUIHandler(t)
	seedUIRows(t, svc, database)

	req := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers/agents/credential-schema", nil)
	req.SetPathValue("id", "agents")
	rec := httptest.NewRecorder()
	h.apiProviderCredentialSchema(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("hidden schema: got %d, want 404", rec.Code)
	}
}

func TestAdapterTypesCarryCreatableFlag(t *testing.T) {
	h, svc, database := newProvidersUIHandler(t)
	seedUIRows(t, svc, database)
	svc.RegisterGoAdapter(testutil.NewMockAdapter("mock"))
	svc.RegisterGoAdapter(&agentsadapter.Adapter{})

	req := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/adapter-types", nil)
	rec := httptest.NewRecorder()
	h.apiAdapterTypes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, body %s", rec.Code, rec.Body.String())
	}
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode adapter-types: %v", err)
	}
	byKey := map[string]any{}
	for _, e := range out {
		k, _ := e["type_key"].(string)
		byKey[k] = e["creatable"]
	}
	if byKey["agents"] != false {
		t.Errorf("agents must be non-creatable: %v", byKey)
	}
	for _, k := range []string{"mock", "zen"} {
		if byKey[k] != true {
			t.Errorf("type %q must be creatable: %v", k, byKey)
		}
	}
}

func TestAvailableModelsIncludesAgentsWithoutCredentials(t *testing.T) {
	h, svc, database := newProvidersUIHandler(t)
	seedUIRows(t, svc, database)
	svc.RegisterGoAdapter(testutil.NewMockAdapter("mock"))
	if _, err := svc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}

	credSvc := credential.New(database, svc)
	modelInfoSvc := modelinfo.New(database, svc, credSvc, 1*time.Hour)
	agentSvc := agent.New(database, svc, modelInfoSvc)
	h.credSvc = credSvc
	h.modelInfoSvc = modelInfoSvc
	h.agentSvc = agentSvc

	a := &models.Agent{
		Name:    "Helper",
		IsDraft: true,
		Models:  []models.AgentModel{{ModelID: "mock/test-model", Priority: 1}},
	}
	if err := agentSvc.Create(a); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if a.ID != "helper" {
		t.Fatalf("agent id: got %q, want %q", a.ID, "helper")
	}

	rows, err := credSvc.ListByProvider("agents")
	if err != nil {
		t.Fatalf("list agents credentials: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected zero agents credentials, got %d", len(rows))
	}

	items, err := h.availableModels(context.Background())
	if err != nil {
		t.Fatalf("available models failed: %v", err)
	}
	found := false
	for _, item := range items {
		if item.FullModelID == "agents/helper" {
			found = true
			if item.ProviderID != "agents" || item.ModelName != "helper" || item.DisplayName != "Helper" {
				t.Errorf("agents entry fields wrong: %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("agents/helper missing from available models: %+v", items)
	}
}
