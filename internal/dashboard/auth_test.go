package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/requestsource"
	"github.com/TheSlopMachine/llm-router/internal/services/admin"
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

// TestSetSessionCookieLifetime pins the remember-me contract: a remembered
// session gets a persistent cookie, any other a browser-session cookie.
func TestSetSessionCookieLifetime(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour)
	request := httptest.NewRequest(http.MethodPost, "/api/llm-router/login", nil)

	recorder := httptest.NewRecorder()
	setSessionCookie(recorder, request, "token", admin.Session{ExpiresAt: expiresAt, RememberMe: true})
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Expires.IsZero() {
		t.Fatalf("remembered session: want a persistent cookie, got %+v", cookies)
	}

	recorder = httptest.NewRecorder()
	setSessionCookie(recorder, request, "token", admin.Session{ExpiresAt: expiresAt})
	cookies = recorder.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Expires.IsZero() || cookies[0].MaxAge != 0 {
		t.Fatalf("session without remember me: want a browser-session cookie, got %+v", cookies)
	}
}

// TestAPIShutdownBranchesOnSource verifies the shutdown endpoint fires
// the signal with 200 on private source and 404s without firing on
// public source, hiding the route from the public listener.
func TestAPIShutdownBranchesOnSource(t *testing.T) {
	fired := false
	handler := &Handler{requestShutdown: func() { fired = true }}

	private := httptest.NewRequest(http.MethodPost, "/api/llm-router/shutdown", nil)
	private = private.WithContext(requestsource.ContextWithSource(private.Context(), requestsource.RequestSourcePrivate))
	recorder := httptest.NewRecorder()
	handler.apiShutdown(recorder, private)
	if recorder.Code != http.StatusOK {
		t.Fatalf("private shutdown: got %d (%s), want 200", recorder.Code, recorder.Body.String())
	}
	if !fired {
		t.Fatal("private shutdown: signal not fired")
	}

	public := httptest.NewRequest(http.MethodPost, "/api/llm-router/shutdown", nil)
	recorder = httptest.NewRecorder()
	handler.apiShutdown(recorder, public)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("public shutdown: got %d (%s), want 404", recorder.Code, recorder.Body.String())
	}
}
