package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	h.noAuth = true
	mux = http.NewServeMux()
	h.Register(mux)
	request := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/proxy/recheck-banned", strings.NewReader(`{"reason":"bad_status"}`))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("recheck route: got %d (%s)", recorder.Code, recorder.Body.String())
	}
}
