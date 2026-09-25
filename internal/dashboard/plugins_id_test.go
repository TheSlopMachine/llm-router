package dashboard

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

const slashIDPluginSource = `--- @plugin Slash ID
--- @author tester
--- @version 1.0.0
--- @router_version 0.1.1
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
	database := testutil.SetupTestDB(t)
	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	rec, err := luaSvc.Install([]byte(slashIDPluginSource), luaplugin.PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(rec.ID, "/") {
		t.Fatalf("fixture must produce a slash ID, got %q", rec.ID)
	}
	h := &Handler{luaSvc: luaSvc, noAuth: true}
	mux := http.NewServeMux()
	h.Register(mux, database)

	serve := func(method, target string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, nil)
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)
		return recorder
	}

	raw := "/api/llm-router/dashboard/plugins/" + rec.ID
	if got := serve(http.MethodGet, raw); got.Code == http.StatusOK {
		t.Logf("note: raw slashes resolve (mux behavior change)")
	}
	encoded := "/api/llm-router/dashboard/plugins/" + url.PathEscape(rec.ID)
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
