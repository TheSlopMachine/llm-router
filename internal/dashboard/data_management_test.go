package dashboard

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/requestsource"
	"github.com/TheSlopMachine/llm-router/internal/services/datamanagement"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

const archivePluginSource = `--- @plugin Archive Test
--- @author tester
--- @version 1.2.3
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("archive-type", {
  complete = function() end,
})
`

// wiredDataMux builds the full server-equivalent handler and its mux.
func wiredDataMux(test *testing.T) (*Handler, *http.ServeMux) {
	test.Helper()
	handler, err := New(buildWiredParams(test))
	if err != nil {
		test.Fatalf("New with wired params: %v", err)
	}
	mux := http.NewServeMux()
	handler.Register(mux)
	return handler, mux
}

func serveData(test *testing.T, mux *http.ServeMux, method, target, contentType string, body []byte) *httptest.ResponseRecorder {
	test.Helper()
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request = request.WithContext(requestsource.ContextWithSource(request.Context(), requestsource.RequestSourcePrivate))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder
}

// TestPluginsArchiveRoundTrip exports the plugins subsystem as a zip (settings
// without sources plus one .lua per plugin), clears it, and imports the zip
// back: the plugin returns with its source and disabled flag.
func TestPluginsArchiveRoundTrip(test *testing.T) {
	handler, mux := wiredDataMux(test)
	ctx := context.Background()
	record, err := handler.luaService.InstallSQLite(ctx, []byte(archivePluginSource), luaplugin.PluginOrigin{Manual: true})
	if err != nil {
		test.Fatalf("install: %v", err)
	}
	if _, err := handler.luaService.SetDisabled(ctx, record.ID, true); err != nil {
		test.Fatalf("disable: %v", err)
	}

	exported := serveData(test, mux, http.MethodGet, "/api/llm-router/dashboard/data/plugins/export", "", nil)
	if exported.Code != http.StatusOK {
		test.Fatalf("export: got %d (%s)", exported.Code, exported.Body.String())
	}
	if contentType := exported.Header().Get("Content-Type"); contentType != "application/zip" {
		test.Fatalf("export content type: got %q", contentType)
	}
	archive := exported.Body.Bytes()

	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		test.Fatalf("open archive: %v", err)
	}
	entries := map[string][]byte{}
	for _, file := range reader.File {
		opened, err := file.Open()
		if err != nil {
			test.Fatalf("open %s: %v", file.Name, err)
		}
		content, err := io.ReadAll(opened)
		opened.Close()
		if err != nil {
			test.Fatalf("read %s: %v", file.Name, err)
		}
		entries[file.Name] = content
	}
	if got := string(entries[datamanagement.PluginSourceFileName(record.ID)]); got != archivePluginSource {
		test.Fatalf("archived source: got %q", got)
	}
	var settings datamanagement.PluginsExportBundle
	if err := json.Unmarshal(entries[datamanagement.PluginsArchiveSettingsFile], &settings); err != nil {
		test.Fatalf("decode settings: %v", err)
	}
	if len(settings.Plugins) != 1 || settings.Plugins[0].ID != record.ID || !settings.Plugins[0].Disabled {
		test.Fatalf("settings plugins: %+v", settings.Plugins)
	}
	if len(settings.Plugins[0].Source) != 0 || len(settings.Plugins[0].History) != 0 {
		test.Fatal("settings must not carry sources")
	}

	if cleared := serveData(test, mux, http.MethodPost, "/api/llm-router/dashboard/data/plugins/clear", "", nil); cleared.Code != http.StatusOK {
		test.Fatalf("clear: got %d (%s)", cleared.Code, cleared.Body.String())
	}
	if _, err := handler.luaService.GetSQLite(ctx, record.ID); err == nil {
		test.Fatal("plugin must be gone after clear")
	}

	imported := serveData(test, mux, http.MethodPost, "/api/llm-router/dashboard/data/plugins/import", "application/zip", archive)
	if imported.Code != http.StatusOK {
		test.Fatalf("import: got %d (%s)", imported.Code, imported.Body.String())
	}
	restored, err := handler.luaService.GetSQLite(ctx, record.ID)
	if err != nil {
		test.Fatalf("restored plugin: %v", err)
	}
	if string(restored.Source) != archivePluginSource || !restored.Disabled || restored.Version != "1.2.3" {
		test.Fatalf("restored plugin: version %q disabled %v", restored.Version, restored.Disabled)
	}
}

func TestPluginsArchiveImportRejectsMissingSource(test *testing.T) {
	_, mux := wiredDataMux(test)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create(datamanagement.PluginsArchiveSettingsFile)
	if err != nil {
		test.Fatalf("create entry: %v", err)
	}
	if _, err := entry.Write([]byte(`{"repos":[],"plugins":[{"id":"tester/missing"}]}`)); err != nil {
		test.Fatalf("write entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		test.Fatalf("close archive: %v", err)
	}
	if got := serveData(test, mux, http.MethodPost, "/api/llm-router/dashboard/data/plugins/import", "application/zip", archive.Bytes()); got.Code != http.StatusBadRequest {
		test.Fatalf("import without source: got %d (%s)", got.Code, got.Body.String())
	}
	if got := serveData(test, mux, http.MethodPost, "/api/llm-router/dashboard/data/plugins/import", "application/json", []byte(`{"repos":[],"plugins":[]}`)); got.Code != http.StatusBadRequest {
		test.Fatalf("import of JSON: got %d (%s)", got.Code, got.Body.String())
	}
}

func TestPluginSourceDownload(test *testing.T) {
	handler, mux := wiredDataMux(test)
	record, err := handler.luaService.InstallSQLite(context.Background(), []byte(archivePluginSource), luaplugin.PluginOrigin{Manual: true})
	if err != nil {
		test.Fatalf("install: %v", err)
	}
	got := serveData(test, mux, http.MethodGet, "/api/llm-router/dashboard/plugins/"+url.PathEscape(record.ID)+"/source", "", nil)
	if got.Code != http.StatusOK {
		test.Fatalf("source: got %d (%s)", got.Code, got.Body.String())
	}
	if got.Body.String() != archivePluginSource {
		test.Fatalf("source body: got %q", got.Body.String())
	}
	if missing := serveData(test, mux, http.MethodGet, "/api/llm-router/dashboard/plugins/"+url.PathEscape("tester/none")+"/source", "", nil); missing.Code != http.StatusNotFound {
		test.Fatalf("missing source: got %d", missing.Code)
	}
}

// TestProviderImportTakesOneItemAndSubsystemFiles imports the per-provider
// export (one bundle object) and the subsystem export (an array).
func TestProviderImportTakesOneItemAndSubsystemFiles(test *testing.T) {
	handler, mux := wiredDataMux(test)
	ctx := context.Background()
	instance, err := handler.gormProviderService.Create(ctx, provider.CreateOptions{Name: "Archive Custom", TypeKey: "custom", Config: customProviderConfig()})
	if err != nil {
		test.Fatalf("create provider: %v", err)
	}
	countBefore := len(mustListProviders(test, handler))

	single := serveData(test, mux, http.MethodGet, "/api/llm-router/dashboard/providers/"+url.PathEscape(instance.ID)+"/export", "", nil)
	if single.Code != http.StatusOK {
		test.Fatalf("provider export: got %d (%s)", single.Code, single.Body.String())
	}
	whole := serveData(test, mux, http.MethodGet, "/api/llm-router/dashboard/data/providers/export", "", nil)
	if whole.Code != http.StatusOK {
		test.Fatalf("subsystem export: got %d (%s)", whole.Code, whole.Body.String())
	}

	for name, file := range map[string][]byte{"one item": single.Body.Bytes(), "subsystem": whole.Body.Bytes()} {
		if err := handler.gormProviderService.Delete(ctx, instance.ID); err != nil {
			test.Fatalf("%s: delete provider: %v", name, err)
		}
		if got := serveData(test, mux, http.MethodPost, "/api/llm-router/dashboard/data/providers/import", "application/json", file); got.Code != http.StatusOK {
			test.Fatalf("%s import: got %d (%s)", name, got.Code, got.Body.String())
		}
		providers := mustListProviders(test, handler)
		if len(providers) != countBefore {
			test.Fatalf("%s import: got %d providers, want %d", name, len(providers), countBefore)
		}
		instance = findProviderByName(test, providers, "Archive Custom")
	}
}

// TestVirtualModelImportTakesOneItemAndSubsystemFiles imports one virtual
// model object (the per-row export) and the subsystem array.
func TestVirtualModelImportTakesOneItemAndSubsystemFiles(test *testing.T) {
	handler, mux := wiredDataMux(test)
	ctx := context.Background()
	instance, err := handler.gormProviderService.Create(ctx, provider.CreateOptions{Name: "Virtual Member", TypeKey: "custom", Config: customProviderConfig()})
	if err != nil {
		test.Fatalf("create provider: %v", err)
	}
	virtualModel := &models.VirtualModel{
		Name:   "Archive Virtual",
		Models: []models.VirtualModelEntry{{ModelID: models.ModelId(instance.ID + "/some-model")}},
	}
	if err := handler.gormVirtualService.Create(ctx, virtualModel); err != nil {
		test.Fatalf("create virtual model: %v", err)
	}

	whole := serveData(test, mux, http.MethodGet, "/api/llm-router/dashboard/data/virtual_models/export", "", nil)
	if whole.Code != http.StatusOK {
		test.Fatalf("export: got %d (%s)", whole.Code, whole.Body.String())
	}
	var exported []json.RawMessage
	if err := json.Unmarshal(whole.Body.Bytes(), &exported); err != nil || len(exported) != 1 {
		test.Fatalf("export body: %v, %d items", err, len(exported))
	}

	for name, file := range map[string][]byte{"one item": exported[0], "subsystem": whole.Body.Bytes()} {
		if err := handler.gormVirtualService.Delete(ctx, virtualModel.ID); err != nil {
			test.Fatalf("%s: delete virtual model: %v", name, err)
		}
		if got := serveData(test, mux, http.MethodPost, "/api/llm-router/dashboard/data/virtual_models/import", "application/json", file); got.Code != http.StatusOK {
			test.Fatalf("%s import: got %d (%s)", name, got.Code, got.Body.String())
		}
		listed, err := handler.gormVirtualService.List(ctx)
		if err != nil {
			test.Fatalf("%s: list: %v", name, err)
		}
		if len(listed) != 1 || listed[0].Name != "Archive Virtual" {
			test.Fatalf("%s import: got %+v", name, listed)
		}
		virtualModel = listed[0]
	}
}

func customProviderConfig() map[string]any {
	return map[string]any{"base_url": "https://api.example.com/v1"}
}

func mustListProviders(test *testing.T, handler *Handler) []*models.ProviderInstance {
	test.Helper()
	providers, err := handler.gormProviderService.List(context.Background())
	if err != nil {
		test.Fatalf("list providers: %v", err)
	}
	return providers
}

func findProviderByName(test *testing.T, providers []*models.ProviderInstance, name string) *models.ProviderInstance {
	test.Helper()
	for _, instance := range providers {
		if instance.Name == name {
			return instance
		}
	}
	test.Fatalf("provider %q not found", name)
	return nil
}
