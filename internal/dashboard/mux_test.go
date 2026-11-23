package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/requestsource"
	"github.com/TheSlopMachine/llm-router/internal/services/dockerhost"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// TestRegisterBuildsMux guards route pattern registration: net/http panics
// on invalid patterns (e.g. a mid-pattern {...} wildcard), which would take
// the whole server down at startup. Every route must register cleanly.
func TestRegisterBuildsMux(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	h := &Handler{}
	mux := http.NewServeMux()
	h.Register(mux)

	service, err := proxypool.New(database)
	if err != nil {
		t.Fatalf("new proxy service: %v", err)
	}
	h.proxyService = service
	mux = http.NewServeMux()
	h.Register(mux)
	request := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/proxy/recheck-banned", strings.NewReader(`{"reason":"bad_status"}`))
	request = request.WithContext(requestsource.ContextWithSource(request.Context(), requestsource.RequestSourcePrivate))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("recheck route: got %d (%s)", recorder.Code, recorder.Body.String())
	}
}

// TestPluginFileRoutes guards the upload review: a preview reports the
// permissions, and an install with another approved digest is refused.
func TestPluginFileRoutes(t *testing.T) {
	luaService, err := luaplugin.New(testutil.SetupTestSQLite(t), nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	mux := http.NewServeMux()
	(&Handler{luaService: luaService}).Register(mux)
	serve := func(path string) *httptest.ResponseRecorder {
		source := "--- @plugin Upload\n--- @author tester\n--- @version 1.0.0\n--- @plugin_api 1.0\n--- @allow_host example.com\n\nllm_router.register(\"upload-type\", {\n  complete = function() end,\n})\n"
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(source))
		request = request.WithContext(requestsource.ContextWithSource(request.Context(), requestsource.RequestSourcePrivate))
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		return recorder
	}
	recorder := serve("/api/llm-router/dashboard/plugins/preview-file")
	var preview pluginPreview
	if err := json.Unmarshal(recorder.Body.Bytes(), &preview); err != nil || recorder.Code != http.StatusOK {
		t.Fatalf("preview: got %d %s", recorder.Code, recorder.Body.String())
	}
	if preview.ID == "" || preview.Permissions.Digest == "" || len(preview.Permissions.AllowHosts) != 1 {
		t.Fatalf("preview: %+v", preview)
	}
	if recorder := serve("/api/llm-router/dashboard/plugins/install-file?permissions_digest=other"); recorder.Code != http.StatusConflict {
		t.Fatalf("install with another digest: got %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := serve("/api/llm-router/dashboard/plugins/install-file?permissions_digest=" + preview.Permissions.Digest); recorder.Code != http.StatusOK {
		t.Fatalf("install with the approved digest: got %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestDockerConnectionRoutes guards the Docker settings routes: defaults on
// a fresh database, validation on write, 503 without the service, and the
// platform routes and connection kinds of each OS.
func TestDockerConnectionRoutes(t *testing.T) {
	serve := func(h *Handler, method, path, body string) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		h.Register(mux)
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request = request.WithContext(requestsource.ContextWithSource(request.Context(), requestsource.RequestSourcePrivate))
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		return recorder
	}
	if recorder := serve(&Handler{}, http.MethodGet, "/api/llm-router/dashboard/docker/connection", ""); recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("without docker service: got %d", recorder.Code)
	}
	h := &Handler{dockerService: dockerhost.New(testutil.SetupTestSQLite(t))}
	recorder := serve(h, http.MethodGet, "/api/llm-router/dashboard/docker/connection", "")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"namespace":"llm-router"`) || !strings.Contains(recorder.Body.String(), `"dial_mode":"auto"`) {
		t.Fatalf("defaults: got %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = serve(h, http.MethodPut, "/api/llm-router/dashboard/docker/connection", `{"endpoint":"ssh://user@host","ssh_host_key":"not a key"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid host key: got %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = serve(h, http.MethodPut, "/api/llm-router/dashboard/docker/connection", `{"endpoint":"tcp://127.0.0.1:1","ssh_private_key":"secret"}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"has_ssh_private_key":true`) || strings.Contains(recorder.Body.String(), "secret\"") {
		t.Fatalf("store: got %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = serve(h, http.MethodPost, "/api/llm-router/dashboard/docker/connection/test", "")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unreachable daemon test: got %d %s", recorder.Code, recorder.Body.String())
	}
	kinds, wslRoute, localRoute := `"connection_kinds":["ssh","unix","tcp"]`, http.StatusNotFound, http.StatusBadRequest
	switch runtime.GOOS {
	case "windows":
		kinds, wslRoute, localRoute = `"connection_kinds":["ssh","tcp","wsl"]`, http.StatusBadRequest, http.StatusNotFound
	case "freebsd":
		kinds, localRoute = `"connection_kinds":["ssh","tcp"]`, http.StatusNotFound
	}
	if recorder := serve(h, http.MethodGet, "/api/llm-router/dashboard/docker/connection", ""); !strings.Contains(recorder.Body.String(), kinds) {
		t.Fatalf("connection kinds: %s", recorder.Body.String())
	}
	recorder = serve(h, http.MethodPost, "/api/llm-router/dashboard/docker/wsl/distributions/bad%20name/setup", "")
	if recorder.Code != wslRoute {
		t.Fatalf("wsl route with an invalid distro name: got %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = serve(h, http.MethodPost, "/api/llm-router/dashboard/docker/local/probe", `{"path":"docker.sock"}`)
	if recorder.Code != localRoute {
		t.Fatalf("local probe with a relative path: got %d %s", recorder.Code, recorder.Body.String())
	}
}
