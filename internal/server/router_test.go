package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	v1 "github.com/TheSlopMachine/llm-router/internal/api/v1"
	"github.com/TheSlopMachine/llm-router/internal/dashboard"
)

// TestBuildRootMuxBuildsCleanly guards route pattern registration:
// net/http panics on conflicting patterns, which would take the server
// down at startup. Every prefix must mount without conflict.
func TestBuildRootMuxBuildsCleanly(t *testing.T) {
	buildRootMux(&v1.Handler{}, &dashboard.Handler{})
}

// TestBuildRootMuxNeverServesHTMLForAPI drives unknown paths through the
// full mux: API prefixes answer JSON, never dashboard HTML or redirects.
// Bare /v1 and /api count as API prefixes.
func TestBuildRootMuxNeverServesHTMLForAPI(t *testing.T) {
	mux := buildRootMux(&v1.Handler{}, &dashboard.Handler{})

	for _, path := range []string{
		"/v1", "/v1/", "/v1/unknown", "/v1/models",
		"/api", "/api/", "/api/llm-router/unknown",
	} {
		for _, method := range []string{"GET", "POST"} {
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

// TestBuildRootMuxKeepsDashboardHTMLOutsideAPI verifies lookalike paths
// (/v10, /apix) stay dashboard routes: prefix matching is exact-or-slash,
// never a raw string prefix.
func TestBuildRootMuxKeepsDashboardHTMLOutsideAPI(t *testing.T) {
	mux := buildRootMux(&v1.Handler{}, &dashboard.Handler{})

	for _, path := range []string{"/", "/v10", "/apix", "/v1models", "/login"} {
		request := httptest.NewRequest("GET", path, nil)
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

// TestIsAPIPathBoundary pins the prefix definition: exact-or-slash only.
func TestIsAPIPathBoundary(t *testing.T) {
	apiPaths := []string{"/api", "/api/", "/api/llm-router/status", "/v1", "/v1/", "/v1/models"}
	for _, path := range apiPaths {
		if !dashboard.IsAPIPath(path) {
			t.Errorf("IsAPIPath(%q) = false, want true", path)
		}
	}
	dashboardPaths := []string{"/", "/login", "/bootstrap", "/assets/app.js", "/icons/x", "/v10", "/apix", "/v1models", "/apiary"}
	for _, path := range dashboardPaths {
		if dashboard.IsAPIPath(path) {
			t.Errorf("IsAPIPath(%q) = true, want false", path)
		}
	}
}
