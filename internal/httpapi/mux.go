// Package httpapi owns the HTTP route tables shared by both listeners.
// The public listener requires bootstrapping and authentication; the
// loopback-only admin listener answers the same JSON API without either
// and serves no dashboard. Route rules are defined once in
// dashboard.RegisterAPI and v1.Register and mounted on both muxes; only
// the "/" catch-all differs (SPA fallback vs JSON 404). Authentication
// behavior comes from the requestsource in the request context, so the
// same handler instances serve both listeners.
package httpapi

import (
	"net/http"

	v1 "github.com/TheSlopMachine/llm-router/internal/api/v1"
	"github.com/TheSlopMachine/llm-router/internal/dashboard"
)

// BuildPublicMux mounts the full surface: /v1 and /api answer JSON,
// everything else serves the dashboard SPA (redirects allowed).
func BuildPublicMux(apiHandler *v1.Handler, dashboardHandler *dashboard.Handler) *http.ServeMux {
	apiMux, dashboardAPIMux := sharedAPIMuxes(apiHandler, dashboardHandler)
	spaMux := http.NewServeMux()
	dashboardHandler.RegisterSPA(spaMux)

	rootMux := http.NewServeMux()
	// Bare prefixes register alongside their subtrees: without the exact
	// pattern the mux 301-redirects POST /v1 to /v1/, which drops bodies.
	rootMux.Handle("/v1", apiMux)
	rootMux.Handle("/v1/", apiMux)
	rootMux.Handle("/api", dashboardAPIMux)
	rootMux.Handle("/api/", dashboardAPIMux)
	rootMux.Handle("/assets/", spaMux)
	rootMux.Handle("/icons/", spaMux)
	rootMux.Handle("/", spaMux)
	return rootMux
}

// BuildPrivateMux mounts the JSON API only: /v1 and /api behave exactly
// as on public (minus authentication), every other path answers JSON
// 404. No SPA, no redirects, no dashboard HTML anywhere.
func BuildPrivateMux(apiHandler *v1.Handler, dashboardHandler *dashboard.Handler) *http.ServeMux {
	apiMux, dashboardAPIMux := sharedAPIMuxes(apiHandler, dashboardHandler)

	rootMux := http.NewServeMux()
	rootMux.Handle("/v1", apiMux)
	rootMux.Handle("/v1/", apiMux)
	rootMux.Handle("/api", dashboardAPIMux)
	rootMux.Handle("/api/", dashboardAPIMux)
	rootMux.HandleFunc("/", dashboard.WriteUnknownEndpoint)
	return rootMux
}

// sharedAPIMuxes registers the JSON API routes once per mux pair: v1
// routes (with their JSON fallback) plus dashboard API routes (with the
// shared JSON fallback). Both listeners mount the pair, so the rules
// exist in one place only.
func sharedAPIMuxes(apiHandler *v1.Handler, dashboardHandler *dashboard.Handler) (apiMux, dashboardAPIMux *http.ServeMux) {
	apiMux = http.NewServeMux()
	apiHandler.Register(apiMux)
	dashboardAPIMux = http.NewServeMux()
	dashboardHandler.RegisterAPI(dashboardAPIMux)
	dashboardAPIMux.HandleFunc("/", dashboard.WriteUnknownEndpoint)
	return apiMux, dashboardAPIMux
}
