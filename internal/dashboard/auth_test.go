package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/requestsource"
)

// TestRequireAuthBranchesOnSource pins the session gate to the request
// source: private passes without a cookie, public without a cookie gets
// 401. Same handler instance serves both listeners.
func TestRequireAuthBranchesOnSource(t *testing.T) {
	handler := &Handler{}
	called := false
	next := func(http.ResponseWriter, *http.Request) { called = true }

	private := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers", nil)
	private = private.WithContext(requestsource.ContextWithSource(private.Context(), requestsource.RequestSourcePrivate))
	handler.requireAuth(next)(httptest.NewRecorder(), private)
	if !called {
		t.Fatal("private source: next not called, want auth bypassed")
	}

	public := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers", nil)
	recorder := httptest.NewRecorder()
	handler.requireAuth(next)(recorder, public)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("public source without cookie: got %d, want 401", recorder.Code)
	}
}

// TestSessionUserPrivate pins the admin identity on the admin listener.
func TestSessionUserPrivate(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request = request.WithContext(requestsource.ContextWithSource(request.Context(), requestsource.RequestSourcePrivate))
	if got := handler.sessionUser(request); got != "admin" {
		t.Fatalf("private session user = %q, want admin", got)
	}
}
