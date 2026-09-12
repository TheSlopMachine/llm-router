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

func seedUIRows(t *testing.T, svc *provider.Service, database *db.DB) *luaplugin.Service {
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
	return luaSvc
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

func TestProvidersListHidesUnavailableBackend(t *testing.T) {
	h, svc, database := newProvidersUIHandler(t)
	luaSvc := seedUIRows(t, svc, database)

	listIDs := func() []string {
		req := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers", nil)
		rec := httptest.NewRecorder()
		h.apiProvidersList(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list status: got %d, body %s", rec.Code, rec.Body.String())
		}
		var ids []string
		for _, e := range decodeProvidersList(t, rec) {
			id, _ := e["id"].(string)
			ids = append(ids, id)
		}
		return ids
	}

	pluginID := ""
	records, err := luaSvc.List()
	if err != nil {
		t.Fatalf("list plugins: %v", err)
	}
	for _, rec := range records {
		for _, k := range rec.TypeKeys {
			if k == "zen" {
				pluginID = rec.ID
			}
		}
	}
	if pluginID == "" {
		t.Fatal("zen plugin record missing")
	}

	before := listIDs()
	found := false
	for _, id := range before {
		if id == "zen" {
			found = true
		}
	}
	if !found {
		t.Fatalf("zen must be listed while plugin installed: %v", before)
	}

	if err := luaSvc.Delete(pluginID); err != nil {
		t.Fatalf("delete plugin: %v", err)
	}
	if _, err := svc.Get("zen"); err != nil {
		t.Fatalf("orphaned provider row must survive plugin delete: %v", err)
	}
	for _, id := range listIDs() {
		if id == "zen" {
			t.Fatal("zen must be hidden while plugin removed")
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers/zen/models", nil)
	req.SetPathValue("id", "zen")
	rec := httptest.NewRecorder()
	h.apiProviderModels(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unavailable models: got %d, want 404, body %s", rec.Code, rec.Body.String())
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
		t.Fatalf("reinstall plugin: %v", err)
	}
	restored := false
	for _, id := range listIDs() {
		if id == "zen" {
			restored = true
		}
	}
	if !restored {
		t.Fatal("zen must be listed again after plugin reinstall")
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

func postAuthInitiate(t *testing.T, h *Handler, providerID string) *httptest.ResponseRecorder {
	t.Helper()
	body := strings.NewReader(`{"provider_id":"` + providerID + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/auth/initiate", body)
	rec := httptest.NewRecorder()
	h.authInitiate(rec, req)
	return rec
}

func TestAuthInitiateGoBackendFallsBack409(t *testing.T) {
	h, providerSvc, _ := newProvidersUIHandler(t)
	providerSvc.RegisterGoAdapter(testutil.NewMockAdapter("custom"))
	inst, err := providerSvc.CreateCustom("OmniRoute", "https://example.com/v1", "")
	if err != nil {
		t.Fatalf("create custom provider: %v", err)
	}

	rec := postAuthInitiate(t, h, inst.ID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status: got %d, body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "stepped auth flows") {
		t.Fatalf("body must name the stepped-flow fallback: %s", rec.Body.String())
	}
}

func TestAuthInitiateLuaWithoutHandlerFallsBack409(t *testing.T) {
	h, providerSvc, database := newProvidersUIHandler(t)
	h.luaSvc = seedUIRows(t, providerSvc, database)

	providers, err := providerSvc.List()
	if err != nil {
		t.Fatalf("list providers: %v", err)
	}
	zenID := ""
	for _, p := range providers {
		if p.TypeKey == "zen" {
			zenID = p.ID
		}
	}
	if zenID == "" {
		t.Fatal("seeded zen provider missing")
	}

	rec := postAuthInitiate(t, h, zenID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status: got %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestAuthInitiateMissingProvider404(t *testing.T) {
	h, _, _ := newProvidersUIHandler(t)

	rec := postAuthInitiate(t, h, "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestProvidersListHidesOrphanedPluginRows(t *testing.T) {
	h, svc, database := newProvidersUIHandler(t)
	luaSvc := seedUIRows(t, svc, database)
	h.luaSvc = luaSvc

	list := func() int {
		req := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers", nil)
		rec := httptest.NewRecorder()
		h.apiProvidersList(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d, body %s", rec.Code, rec.Body.String())
		}
		return len(decodeProvidersList(t, rec))
	}

	if got := list(); got != 1 {
		t.Fatalf("installed plugin: expected 1 visible provider, got %d", got)
	}
	plugins, err := luaSvc.List()
	if err != nil || len(plugins) != 1 {
		t.Fatalf("list plugins: %v", err)
	}
	if err := luaSvc.Delete(plugins[0].ID); err != nil {
		t.Fatalf("delete plugin: %v", err)
	}
	if got := list(); got != 0 {
		t.Fatalf("deleted plugin: expected 0 visible providers, got %d", got)
	}
}
