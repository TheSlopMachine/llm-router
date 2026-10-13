package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/requestsource"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

const slashIDPluginSource = `--- @plugin Slash ID
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("slash-type", {
  complete = function() end,
})
`

// TestPluginIDRoutesAcceptEncodedSlashes locks the ID contract: record IDs
// always contain slashes (manual/a/b, index/.../...), so clients must send
// them encoded (as the dashboard frontend does with encodeURIComponent)
// and the API must resolve them back to the record.
func TestPluginIDRoutesAcceptEncodedSlashes(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	luaService, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	record, err := luaService.InstallSQLite(context.Background(), []byte(slashIDPluginSource), luaplugin.PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(record.ID, "/") {
		t.Fatalf("fixture must produce a slash ID, got %q", record.ID)
	}
	h := &Handler{luaService: luaService}
	mux := http.NewServeMux()
	h.Register(mux)

	serve := func(method, target string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, target, nil)
		request = request.WithContext(requestsource.ContextWithSource(request.Context(), requestsource.RequestSourcePrivate))
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		return recorder
	}

	raw := "/api/llm-router/dashboard/plugins/" + record.ID
	if got := serve(http.MethodGet, raw); got.Code == http.StatusOK {
		t.Logf("note: raw slashes resolve (mux behavior change)")
	}
	encoded := "/api/llm-router/dashboard/plugins/" + url.PathEscape(record.ID)
	if got := serve(http.MethodGet, encoded); got.Code != http.StatusOK {
		t.Fatalf("encoded GET: got %d (%s)", got.Code, got.Body.String())
	}
	if got := serve(http.MethodDelete, encoded); got.Code != http.StatusOK {
		t.Fatalf("encoded DELETE: got %d (%s)", got.Code, got.Body.String())
	}
	if got := serve(http.MethodGet, encoded); got.Code == http.StatusOK {
		t.Fatal("deleted plugin must be gone")
	}
}

func TestPluginUpdateTogglesDisabled(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	luaService, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	record, err := luaService.InstallSQLite(context.Background(), []byte(slashIDPluginSource), luaplugin.PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	handler := &Handler{luaService: luaService}
	mux := http.NewServeMux()
	handler.Register(mux)
	encoded := "/api/llm-router/dashboard/plugins/" + url.PathEscape(record.ID)

	serve := func(method, target string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		} else {
			reader = bytes.NewReader(nil)
		}
		request := httptest.NewRequest(method, target, reader)
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		request = request.WithContext(requestsource.ContextWithSource(request.Context(), requestsource.RequestSourcePrivate))
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		return recorder
	}
	decodeDisabled := func(payload []byte) bool {
		t.Helper()
		var view struct {
			Disabled bool `json:"disabled"`
		}
		if err := json.Unmarshal(payload, &view); err != nil {
			t.Fatalf("decode plugin view: %v", err)
		}
		return view.Disabled
	}

	if got := serve(http.MethodGet, encoded, nil); got.Code != http.StatusOK {
		t.Fatalf("initial GET: got %d (%s)", got.Code, got.Body.String())
	} else if decodeDisabled(got.Body.Bytes()) {
		t.Fatal("fresh install must stay enabled")
	}
	if got := serve(http.MethodPut, encoded, []byte(`{"disabled":true}`)); got.Code != http.StatusOK {
		t.Fatalf("disable PUT: got %d (%s)", got.Code, got.Body.String())
	} else if !decodeDisabled(got.Body.Bytes()) {
		t.Fatal("disable PUT must return disabled true")
	}
	if got := serve(http.MethodGet, encoded, nil); got.Code != http.StatusOK {
		t.Fatalf("GET after disable: got %d (%s)", got.Code, got.Body.String())
	} else if !decodeDisabled(got.Body.Bytes()) {
		t.Fatal("disabled flag must persist across reads")
	}
	if _, err := luaService.Lookup("slash-type"); err == nil {
		t.Fatal("disabled type must leave the registry")
	}
	if got := serve(http.MethodPut, encoded, []byte(`{"disabled":false}`)); got.Code != http.StatusOK {
		t.Fatalf("enable PUT: got %d (%s)", got.Code, got.Body.String())
	} else if decodeDisabled(got.Body.Bytes()) {
		t.Fatal("enable PUT must return disabled false")
	}
	if _, err := luaService.Lookup("slash-type"); err != nil {
		t.Fatalf("lookup after enable: %v", err)
	}
	if got := serve(http.MethodPut, encoded, []byte(`{}`)); got.Code != http.StatusBadRequest {
		t.Fatalf("empty PUT must fail, got %d (%s)", got.Code, got.Body.String())
	}
}
