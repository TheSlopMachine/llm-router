package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func newProxyDashboardHandler(t *testing.T) *Handler {
	t.Helper()
	service, err := proxypool.New(testutil.SetupTestDB(t))
	if err != nil {
		t.Fatalf("new proxy service: %v", err)
	}
	return &Handler{proxySvc: service, noAuth: true}
}

func TestProxyRecheckBannedHandler(t *testing.T) {
	h := newProxyDashboardHandler(t)
	url := "http://192.0.2.10:8080"
	h.proxySvc.MarkDead(url, "dns_resolution")

	t.Run("valid reason", func(t *testing.T) {
		body, _ := json.Marshal(models.RecheckBannedRequest{Reason: "dns", Source: ""})
		req := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/proxy/recheck-banned", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		h.apiProxyRecheckBanned(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		var out models.RecheckBannedResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if out.Queued < 0 {
			t.Fatalf("queued = %d, want non-negative", out.Queued)
		}
	})

	t.Run("invalid reason", func(t *testing.T) {
		body := bytes.NewBufferString(`{"reason":"not-a-reason"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/proxy/recheck-banned", body)
		rec := httptest.NewRecorder()
		h.apiProxyRecheckBanned(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/proxy/recheck-banned", nil)
		rec := httptest.NewRecorder()
		h.apiProxyRecheckBanned(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestProxyStatusRouteAndShape(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service, err := proxypool.New(database)
	if err != nil {
		t.Fatalf("new proxy service: %v", err)
	}
	h := &Handler{proxySvc: service, noAuth: true}
	mux := http.NewServeMux()
	h.Register(mux, database)

	req := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/proxy/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status route: got %d (%s)", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	for _, key := range []string{"lanes", "sources", "ban_reasons"} {
		if string(raw[key]) == "null" {
			t.Fatalf("%s must not be null", key)
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/proxy/recheck-banned", bytes.NewBufferString(`{}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("recheck route: got %d (%s)", rec.Code, rec.Body.String())
	}
}
