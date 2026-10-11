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
	proxyService, err := proxypool.New(testutil.SetupTestSQLite(t))
	if err != nil {
		t.Fatalf("new proxy service: %v", err)
	}
	return &Handler{proxyService: proxyService, noAuth: true}
}

func TestProxyRecheckBannedHandler(t *testing.T) {
	h := newProxyDashboardHandler(t)
	url := "http://192.0.2.10:8080"
	h.proxyService.MarkDead(url, "dns_resolution")

	t.Run("valid reason", func(t *testing.T) {
		body, _ := json.Marshal(models.RecheckBannedRequest{Reason: "dns", Source: ""})
		request := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/proxy/recheck-banned", bytes.NewReader(body))
		recorder := httptest.NewRecorder()
		h.apiProxyRecheckBanned(recorder, request)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
		}
		var response models.RecheckBannedResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if response.Queued < 0 {
			t.Fatalf("queued = %d, want non-negative", response.Queued)
		}
	})

	t.Run("invalid reason", func(t *testing.T) {
		body := bytes.NewBufferString(`{"reason":"not-a-reason"}`)
		request := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/proxy/recheck-banned", body)
		recorder := httptest.NewRecorder()
		h.apiProxyRecheckBanned(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("empty body", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/proxy/recheck-banned", nil)
		recorder := httptest.NewRecorder()
		h.apiProxyRecheckBanned(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
		}
	})
}

func TestProxyStatusRouteAndShape(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	proxyService, err := proxypool.New(database)
	if err != nil {
		t.Fatalf("new proxy service: %v", err)
	}
	h := &Handler{proxyService: proxyService, noAuth: true}
	mux := http.NewServeMux()
	h.Register(mux)

	request := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/proxy/status", nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status route: got %d (%s)", recorder.Code, recorder.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	for _, key := range []string{"lanes", "sources", "ban_reasons"} {
		if string(raw[key]) == "null" {
			t.Fatalf("%s must not be null", key)
		}
	}

	request = httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/proxy/recheck-banned", bytes.NewBufferString(`{}`))
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("recheck route: got %d (%s)", recorder.Code, recorder.Body.String())
	}
}
