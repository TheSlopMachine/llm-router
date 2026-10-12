package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1 "github.com/TheSlopMachine/llm-router/internal/api/v1"
	"github.com/TheSlopMachine/llm-router/internal/dashboard"
	"github.com/TheSlopMachine/llm-router/internal/requestsource"
)

// TestMuxesBuildCleanly guards route pattern registration: net/http
// panics on conflicting patterns, which would take the server down at
// startup. Every prefix must mount without conflict on both listeners.
func TestMuxesBuildCleanly(t *testing.T) {
	BuildPublicMux(&v1.Handler{}, &dashboard.Handler{})
	BuildPrivateMux(&v1.Handler{}, &dashboard.Handler{})
}

// TestPublicMuxNeverServesHTMLForAPI drives unknown paths through the
// public mux: API prefixes answer JSON, never dashboard HTML or
// redirects. Bare /v1 and /api count as API prefixes.
func TestPublicMuxNeverServesHTMLForAPI(t *testing.T) {
	mux := BuildPublicMux(&v1.Handler{}, &dashboard.Handler{})

	for _, path := range []string{
		"/v1", "/v1/", "/v1/unknown", "/v1/models",
		"/api", "/api/", "/api/llm-router/unknown",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			request := httptest.NewRequest(method, path, nil)
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, request)
			response := recorder.Result()
			contentType := response.Header.Get("Content-Type")
			if !strings.HasPrefix(contentType, "application/json") {
				t.Errorf("%s %s: Content-Type = %q, want application/json", method, path, contentType)
			}
			if location := response.Header.Get("Location"); location != "" {
				t.Errorf("%s %s: redirected to %q, want JSON body", method, path, location)
			}
			if response.StatusCode < 400 {
				t.Errorf("%s %s: status = %d, want 4xx", method, path, response.StatusCode)
			}
		}
	}
}

// TestPublicMuxKeepsDashboardHTMLOutsideAPI verifies lookalike paths
// (/v10, /apix) stay dashboard routes: prefix matching is exact-or-slash,
// never a raw string prefix.
func TestPublicMuxKeepsDashboardHTMLOutsideAPI(t *testing.T) {
	mux := BuildPublicMux(&v1.Handler{}, &dashboard.Handler{})

	for _, path := range []string{"/", "/v10", "/apix", "/v1models", "/login"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		response := recorder.Result()
		contentType := response.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "text/html") {
			t.Errorf("GET %s: Content-Type = %q, want text/html", path, contentType)
		}
		if location := response.Header.Get("Location"); location != "" {
			t.Errorf("GET %s: redirected to %q, want SPA HTML", path, location)
		}
	}
}

// TestPrivateMuxServesNoDashboard verifies the admin listener answers
// JSON everywhere: API paths behave as on public, and SPA paths
// (/, /login, /assets) return JSON 404 instead of HTML or redirects.
func TestPrivateMuxServesNoDashboard(t *testing.T) {
	mux := BuildPrivateMux(&v1.Handler{}, &dashboard.Handler{})

	for _, path := range []string{
		"/", "/login", "/bootstrap", "/assets/app.js", "/icons/x",
		"/v1", "/v1/", "/v1/unknown",
		"/api", "/api/", "/api/llm-router/unknown",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			request := withSource(method, path, requestsource.RequestSourcePrivate)
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, request)
			response := recorder.Result()
			contentType := response.Header.Get("Content-Type")
			if !strings.HasPrefix(contentType, "application/json") {
				t.Errorf("%s %s: Content-Type = %q, want application/json", method, path, contentType)
			}
			if location := response.Header.Get("Location"); location != "" {
				t.Errorf("%s %s: redirected to %q, want JSON body", method, path, location)
			}
			if response.StatusCode < 400 {
				t.Errorf("%s %s: status = %d, want 4xx", method, path, response.StatusCode)
			}
		}
	}
}

// TestPrivateMuxSkipsAuthentication verifies service-free routes pass
// without session cookie or bearer token on private source: logout and
// model listing answer 200. The public contrast on model listing (401)
// proves the source gates the bearer path; the dashboard gate is pinned
// in-package by TestRequireAuthBranchesOnSource.
func TestPrivateMuxSkipsAuthentication(t *testing.T) {
	mux := BuildPrivateMux(&v1.Handler{}, &dashboard.Handler{})

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/llm-router/logout"},
		{http.MethodGet, "/v1/models"},
	} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, withSource(tc.method, tc.path, requestsource.RequestSourcePrivate))
		if recorder.Code != http.StatusOK {
			t.Errorf("private %s %s: got %d (%s), want 200", tc.method, tc.path, recorder.Code, recorder.Body.String())
		}
	}

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, withSource(http.MethodGet, "/v1/models", requestsource.RequestSourcePublic))
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("public GET /v1/models: got %d (%s), want 401", recorder.Code, recorder.Body.String())
	}
}

func withSource(method, path string, source requestsource.RequestSource) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	return request.WithContext(requestsource.ContextWithSource(request.Context(), source))
}
