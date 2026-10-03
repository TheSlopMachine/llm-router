package dashboard

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/services/geoban"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
)

func TestValidateProxyRetryConfig(t *testing.T) {
	if err := validateProxyRetryConfig(nil); err != nil {
		t.Fatalf("nil: %v", err)
	}
	if err := validateProxyRetryConfig(map[string]any{}); err != nil {
		t.Fatalf("empty: %v", err)
	}
	valid := map[string]any{"proxy_retry": map[string]any{"mode": "next_proxy", "max_attempts": 3.0}}
	if err := validateProxyRetryConfig(valid); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := validateProxyRetryConfig(map[string]any{"geo": map[string]any{}}); err == nil {
		t.Fatal("legacy geo must be rejected")
	}
	for name, cfg := range map[string]map[string]any{
		"bad mode":     {"proxy_retry": map[string]any{"mode": "bogus"}},
		"zero max":     {"proxy_retry": map[string]any{"max_attempts": 0.0}},
		"huge max":     {"proxy_retry": map[string]any{"max_attempts": 99.0}},
		"fraction max": {"proxy_retry": map[string]any{"max_attempts": 2.5}},
		"string max":   {"proxy_retry": map[string]any{"max_attempts": "many"}},
		"non-object":   {"proxy_retry": "next_proxy"},
	} {
		if err := validateProxyRetryConfig(cfg); err == nil {
			t.Fatalf("%s must be rejected", name)
		}
	}
}

func newGeoBansHandler(t *testing.T) (*Handler, *geoban.Service, string) {
	t.Helper()
	h, providerSvc, database := newProvidersUIHandler(t)
	luaSvc := seedUIRows(t, providerSvc, database)
	geobanSvc := geoban.New(database)
	proxySvc, err := proxypool.New(database)
	if err != nil {
		t.Fatalf("init proxy pool: %v", err)
	}
	h.luaSvc = luaSvc
	h.geobanSvc = geobanSvc
	h.proxySvc = proxySvc
	rec, err := luaSvc.Lookup("zen")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	return &Handler{
		providerSvc: h.providerSvc, luaSvc: luaSvc,
		geobanSvc: geobanSvc, proxySvc: proxySvc,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, geobanSvc, rec.ID
}

func decodeBans(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var out struct {
		Bans []map[string]any `json:"bans"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode bans: %v", err)
	}
	return out.Bans
}

func TestGeoBansRoundTrip(t *testing.T) {
	h, geobanSvc, pluginID := newGeoBansHandler(t)

	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers/zen/geo-bans", nil)
		req.SetPathValue("id", "zen")
		rec := httptest.NewRecorder()
		h.apiGeoBansList(rec, req)
		return rec
	}
	if rec := get(); rec.Code != http.StatusOK {
		t.Fatalf("list status: %d %s", rec.Code, rec.Body.String())
	} else if bans := decodeBans(t, rec); len(bans) != 0 {
		t.Fatalf("must start empty: %v", bans)
	}

	if err := geobanSvc.Mark(pluginID, "zen", "px1", "geo blocked"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	rec := get()
	if rec.Code != http.StatusOK {
		t.Fatalf("list status: %d", rec.Code)
	}
	bans := decodeBans(t, rec)
	if len(bans) != 1 || bans[0]["proxy_id"] != "px1" || bans[0]["reason"] != "geo blocked" || bans[0]["banned_at"] == "" {
		t.Fatalf("ban row: %v", bans)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/llm-router/dashboard/providers/zen/geo-bans/px-missing", nil)
	req.SetPathValue("id", "zen")
	req.SetPathValue("proxyId", "px-missing")
	miss := httptest.NewRecorder()
	h.apiGeoBansClearOne(miss, req)
	if miss.Code != http.StatusNotFound {
		t.Fatalf("missing ban must 404: %d", miss.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/llm-router/dashboard/providers/zen/geo-bans/px1", nil)
	req.SetPathValue("id", "zen")
	req.SetPathValue("proxyId", "px1")
	del := httptest.NewRecorder()
	h.apiGeoBansClearOne(del, req)
	if del.Code != http.StatusNoContent {
		t.Fatalf("clear one: %d %s", del.Code, del.Body.String())
	}
	if bans := decodeBans(t, get()); len(bans) != 0 {
		t.Fatalf("must be empty after clear: %v", bans)
	}

	if err := geobanSvc.Mark(pluginID, "zen", "px1", "a"); err != nil {
		t.Fatal(err)
	}
	if err := geobanSvc.Mark(pluginID, "zen", "px2", "b"); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/llm-router/dashboard/providers/zen/geo-bans", nil)
	req.SetPathValue("id", "zen")
	all := httptest.NewRecorder()
	h.apiGeoBansClearAll(all, req)
	if all.Code != http.StatusNoContent {
		t.Fatalf("clear all: %d", all.Code)
	}
	if bans := decodeBans(t, get()); len(bans) != 0 {
		t.Fatalf("must be empty after clear all: %v", bans)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers/nope/geo-bans", nil)
	req.SetPathValue("id", "nope")
	nf := httptest.NewRecorder()
	h.apiGeoBansList(nf, req)
	if nf.Code != http.StatusNotFound {
		t.Fatalf("unknown provider must 404: %d", nf.Code)
	}
}
