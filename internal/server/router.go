package server

import (
	"net/http"

	v1 "github.com/TheSlopMachine/llm-router/internal/api/v1"
	"github.com/TheSlopMachine/llm-router/internal/dashboard"
)

// Single-listener routing contract:
//
//	/v1, /v1/*   -> OpenAI-compatible API (router-token auth). JSON only.
//	/api, /api/* -> dashboard JSON API (session auth). JSON only.
//	/*           -> dashboard SPA (boot gate). Redirects allowed.
//
// API prefixes never redirect and never serve SPA HTML: a client asking
// for JSON receives JSON, even for unknown paths. dashboard.IsAPIPath
// owns the prefix definition; the SPA fallback rechecks it as a second
// boundary so a mis-dispatched API path still returns JSON.
func buildRootMux(apiHandler *v1.Handler, dashboardHandler *dashboard.Handler) *http.ServeMux {
	apiMux := http.NewServeMux()
	apiHandler.Register(apiMux)
	dashboardMux := http.NewServeMux()
	dashboardHandler.Register(dashboardMux)

	rootMux := http.NewServeMux()
	// Bare prefixes register alongside their subtrees: without the exact
	// pattern the mux 301-redirects POST /v1 to /v1/, which drops bodies.
	rootMux.Handle("/v1", apiMux)
	rootMux.Handle("/v1/", apiMux)
	rootMux.Handle("/api", dashboardMux)
	rootMux.Handle("/api/", dashboardMux)
	rootMux.Handle("/assets/", dashboardMux)
	rootMux.Handle("/icons/", dashboardMux)
	rootMux.Handle("/", dashboardMux)
	return rootMux
}
