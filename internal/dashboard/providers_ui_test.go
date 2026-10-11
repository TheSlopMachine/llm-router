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
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func newProvidersUIHandler(t *testing.T) (*Handler, *provider.GormService, *db.Database) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	providerService := provider.NewGorm(database)
	return &Handler{gormProviderService: providerService}, providerService, database
}

func seedUIRows(t *testing.T, providerService *provider.GormService, database *db.Database) *luaplugin.Service {
	t.Helper()
	ctx := context.Background()
	luaService, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	const source = `--- @plugin Seed Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("zen", {
  complete = function() end,
})
`
	if _, err := luaService.InstallSQLite(ctx, []byte(source), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	providerService.SetLuaService(luaService)
	if err := providerService.EnsureSeeded(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return luaService
}

func decodeProvidersList(t *testing.T, recorder *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var entries []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return entries
}

func TestProvidersListExcludesHidden(t *testing.T) {
	h, providerService, database := newProvidersUIHandler(t)
	seedUIRows(t, providerService, database)

	request := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers", nil)
	recorder := httptest.NewRecorder()
	h.apiProvidersList(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, body %s", recorder.Code, recorder.Body.String())
	}
	entries := decodeProvidersList(t, recorder)
	if len(entries) != 1 {
		t.Fatalf("expected 1 visible provider, got %d: %v", len(entries), entries)
	}
	if entries[0]["id"] != "zen" {
		t.Fatalf("expected zen row, got %v", entries[0]["id"])
	}
	if entries[0]["is_ui_readonly"] != true {
		t.Fatalf("view must carry is_ui_readonly: %v", entries[0])
	}
}

func TestProvidersListHidesUnavailableBackend(t *testing.T) {
	h, providerService, database := newProvidersUIHandler(t)
	luaService := seedUIRows(t, providerService, database)
	ctx := context.Background()

	listIDs := func() []string {
		request := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers", nil)
		recorder := httptest.NewRecorder()
		h.apiProvidersList(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("list status: got %d, body %s", recorder.Code, recorder.Body.String())
		}
		var ids []string
		for _, entry := range decodeProvidersList(t, recorder) {
			id, _ := entry["id"].(string)
			ids = append(ids, id)
		}
		return ids
	}

	pluginID := ""
	records, err := luaService.ListSQLite(ctx)
	if err != nil {
		t.Fatalf("list plugins: %v", err)
	}
	for _, record := range records {
		for _, key := range record.TypeKeys {
			if key == "zen" {
				pluginID = record.ID
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

	if err := luaService.DeleteSQLite(ctx, pluginID); err != nil {
		t.Fatalf("delete plugin: %v", err)
	}
	if _, err := providerService.Get(ctx, "zen"); err != nil {
		t.Fatalf("orphaned provider row must survive plugin delete: %v", err)
	}
	for _, id := range listIDs() {
		if id == "zen" {
			t.Fatal("zen must be hidden while plugin removed")
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers/zen/models", nil)
	request.SetPathValue("id", "zen")
	recorder := httptest.NewRecorder()
	h.apiProviderModels(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unavailable models: got %d, want 404, body %s", recorder.Code, recorder.Body.String())
	}

	const source = `--- @plugin Seed Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("zen", {
  complete = function() end,
})
`
	if _, err := luaService.InstallSQLite(ctx, []byte(source), luaplugin.PluginOrigin{Manual: true}); err != nil {
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

func TestProvidersUpdateReadonlyRemovalPersists(t *testing.T) {
	h, providerService, database := newProvidersUIHandler(t)
	seedUIRows(t, providerService, database)
	ctx := context.Background()
	// Proxy pool references need the seeded free-pool row; server wiring
	// seeds it through the proxypool service, tests seed it explicitly.
	if err := proxypool.EnsureBuiltinFreePool(ctx, database); err != nil {
		t.Fatalf("seed free pool: %v", err)
	}

	put := func(body string) int {
		request := httptest.NewRequest(http.MethodPut, "/api/llm-router/dashboard/providers/zen", strings.NewReader(body))
		request.SetPathValue("id", "zen")
		recorder := httptest.NewRecorder()
		h.apiProvidersUpdate(recorder, request)
		return recorder.Code
	}
	configOf := func() map[string]any {
		t.Helper()
		instance, err := providerService.Get(ctx, "zen")
		if err != nil {
			t.Fatalf("get zen: %v", err)
		}
		if instance.Config == nil {
			return map[string]any{}
		}
		return instance.Config
	}

	if code := put(`{"config":{"proxy":{"pool":"auto"},"models_auto_sync":true}}`); code != http.StatusOK {
		t.Fatalf("set proxy: got %d", code)
	}
	if _, ok := configOf()["proxy"]; !ok {
		t.Fatal("proxy key must persist after set")
	}

	// Omitted allowed keys are removals: the switch-off must survive.
	if code := put(`{"config":{"models_auto_sync":true}}`); code != http.StatusOK {
		t.Fatalf("drop proxy: got %d", code)
	}
	storedConfig := configOf()
	if _, ok := storedConfig["proxy"]; ok {
		t.Fatalf("omitted proxy key must be deleted, got %v", storedConfig)
	}
	if storedConfig["models_auto_sync"] != true {
		t.Fatalf("unrelated keys must survive removal, got %v", storedConfig)
	}

	// Explicit null clears the same way, without storing nulls.
	if code := put(`{"config":{"proxy":{"pool":"auto"}}}`); code != http.StatusOK {
		t.Fatalf("re-set proxy: got %d", code)
	}
	if code := put(`{"config":{"proxy":null}}`); code != http.StatusOK {
		t.Fatalf("null proxy: got %d", code)
	}
	if _, ok := configOf()["proxy"]; ok {
		t.Fatalf("null proxy key must be deleted, got %v", configOf())
	}
}

func TestProvidersUpdateGuards(t *testing.T) {
	h, providerService, database := newProvidersUIHandler(t)
	seedUIRows(t, providerService, database)

	call := func(id, body string) int {
		request := httptest.NewRequest(http.MethodPut, "/api/llm-router/dashboard/providers/"+id, strings.NewReader(body))
		request.SetPathValue("id", id)
		recorder := httptest.NewRecorder()
		h.apiProvidersUpdate(recorder, request)
		return recorder.Code
	}

	if code := call("zen", `{"name":"Zen 2"}`); code != http.StatusForbidden {
		t.Errorf("readonly update: got %d, want 403", code)
	}
	if code := call("virtual", `{"name":"Virtual 2"}`); code != http.StatusNotFound {
		t.Errorf("hidden update: got %d, want 404", code)
	}
	if code := call("missing", `{"name":"X"}`); code != http.StatusNotFound {
		t.Errorf("missing update: got %d, want 404", code)
	}
}

func TestProvidersDeleteGuards(t *testing.T) {
	h, providerService, database := newProvidersUIHandler(t)
	seedUIRows(t, providerService, database)
	ctx := context.Background()

	call := func(id string) int {
		request := httptest.NewRequest(http.MethodDelete, "/api/llm-router/dashboard/providers/"+id, nil)
		request.SetPathValue("id", id)
		recorder := httptest.NewRecorder()
		h.apiProvidersDelete(recorder, request)
		return recorder.Code
	}

	if code := call("zen"); code != http.StatusForbidden {
		t.Errorf("readonly delete: got %d, want 403", code)
	}
	if code := call("virtual"); code != http.StatusNotFound {
		t.Errorf("hidden delete: got %d, want 404", code)
	}
	if _, err := providerService.Get(ctx, "zen"); err != nil {
		t.Errorf("readonly row must survive blocked delete: %v", err)
	}
	if _, err := providerService.Get(ctx, "virtual"); err != nil {
		t.Errorf("hidden row must survive blocked delete: %v", err)
	}
}

func TestProviderSchemasHidden404(t *testing.T) {
	h, providerService, database := newProvidersUIHandler(t)
	seedUIRows(t, providerService, database)

	request := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers/virtual/credential-schema", nil)
	request.SetPathValue("id", "virtual")
	recorder := httptest.NewRecorder()
	h.apiProviderCredentialSchema(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("hidden schema: got %d, want 404", recorder.Code)
	}
}

func TestProvidersCreateAllowsOnlyCustom(t *testing.T) {
	h, providerService, _ := newProvidersUIHandler(t)
	providerService.RegisterGoAdapter(testutil.NewMockAdapter("custom"))

	call := func(body string) int {
		request := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/providers", strings.NewReader(body))
		recorder := httptest.NewRecorder()
		h.apiProvidersCreate(recorder, request)
		return recorder.Code
	}

	if code := call(`{"name":"Mine","type_key":"custom","config":{"base_url":"https://api.example.com/v1"}}`); code != http.StatusOK {
		t.Errorf("custom create: got %d, want 200", code)
	}
	if code := call(`{"name":"Zen 2","type_key":"zen","config":{}}`); code != http.StatusBadRequest {
		t.Errorf("plugin type create: got %d, want 400", code)
	}
	if code := call(`{"name":"V","type_key":"virtual","config":{}}`); code != http.StatusBadRequest {
		t.Errorf("virtual create: got %d, want 400", code)
	}
}

func TestAvailableModelsIncludesVirtualWithoutCredentials(t *testing.T) {
	h, providerService, database := newProvidersUIHandler(t)
	seedUIRows(t, providerService, database)
	ctx := context.Background()
	providerService.RegisterGoAdapter(testutil.NewMockAdapter("mock"))
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}

	credentialService := credential.NewGorm(database, providerService)
	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)
	virtualService := virtual.NewGorm(database, providerService, modelInfoService)
	h.gormCredentialService = credentialService
	h.gormModelInfoService = modelInfoService
	h.gormVirtualService = virtualService

	virtualModel := &models.VirtualModel{
		Name:   "Helper",
		Models: []models.VirtualModelEntry{{ModelID: "mock/test-model"}},
	}
	if err := virtualService.Create(ctx, virtualModel); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}
	if virtualModel.ID != "helper" {
		t.Fatalf("virtual model id: got %q, want %q", virtualModel.ID, "helper")
	}

	rows, err := credentialService.ListByProvider(ctx, provider.TypeVirtual)
	if err != nil {
		t.Fatalf("list virtual credentials: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected zero virtual credentials, got %d", len(rows))
	}

	items, err := h.availableModels(ctx)
	if err != nil {
		t.Fatalf("available models failed: %v", err)
	}
	for _, item := range items {
		if item.ProviderType == provider.TypeVirtual {
			t.Fatalf("virtual entry must not be listed: %+v", item)
		}
	}
}

func postAuthInitiate(t *testing.T, h *Handler, providerID string) *httptest.ResponseRecorder {
	t.Helper()
	body := strings.NewReader(`{"provider_id":"` + providerID + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/auth/initiate", body)
	recorder := httptest.NewRecorder()
	h.authInitiate(recorder, request)
	return recorder
}

func TestAuthInitiateGoBackendFallsBack409(t *testing.T) {
	h, providerService, _ := newProvidersUIHandler(t)
	ctx := context.Background()
	providerService.RegisterGoAdapter(testutil.NewMockAdapter("custom"))
	instance, err := providerService.Create(ctx, provider.CreateOptions{Name: "OmniRoute", TypeKey: "custom",
		Config: map[string]any{"base_url": "https://example.com/v1"}})
	if err != nil {
		t.Fatalf("create custom provider: %v", err)
	}

	recorder := postAuthInitiate(t, h, instance.ID)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status: got %d, body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "stepped auth flows") {
		t.Fatalf("body must name the stepped-flow fallback: %s", recorder.Body.String())
	}
}

func TestAuthInitiateLuaWithoutHandlerFallsBack409(t *testing.T) {
	h, providerService, database := newProvidersUIHandler(t)
	h.luaService = seedUIRows(t, providerService, database)
	ctx := context.Background()

	providers, err := providerService.List(ctx)
	if err != nil {
		t.Fatalf("list providers: %v", err)
	}
	zenID := ""
	for _, provider := range providers {
		if provider.TypeKey == "zen" {
			zenID = provider.ID
		}
	}
	if zenID == "" {
		t.Fatal("seeded zen provider missing")
	}

	recorder := postAuthInitiate(t, h, zenID)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status: got %d, body %s", recorder.Code, recorder.Body.String())
	}
}

func TestAuthInitiateMissingProvider404(t *testing.T) {
	h, _, _ := newProvidersUIHandler(t)

	recorder := postAuthInitiate(t, h, "nope")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, body %s", recorder.Code, recorder.Body.String())
	}
}

func TestProvidersListHidesOrphanedPluginRows(t *testing.T) {
	h, providerService, database := newProvidersUIHandler(t)
	luaService := seedUIRows(t, providerService, database)
	h.luaService = luaService
	ctx := context.Background()

	list := func() int {
		request := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers", nil)
		recorder := httptest.NewRecorder()
		h.apiProvidersList(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status: got %d, body %s", recorder.Code, recorder.Body.String())
		}
		return len(decodeProvidersList(t, recorder))
	}

	if got := list(); got != 1 {
		t.Fatalf("installed plugin: expected 1 visible provider, got %d", got)
	}
	plugins, err := luaService.ListSQLite(ctx)
	if err != nil || len(plugins) != 1 {
		t.Fatalf("list plugins: %v", err)
	}
	if err := luaService.DeleteSQLite(ctx, plugins[0].ID); err != nil {
		t.Fatalf("delete plugin: %v", err)
	}
	if got := list(); got != 0 {
		t.Fatalf("deleted plugin: expected 0 visible providers, got %d", got)
	}
}
