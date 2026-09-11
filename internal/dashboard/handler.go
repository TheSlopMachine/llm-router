// Package dashboard serves the admin SPA and its backing JSON API.
package dashboard

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/admin"
	"github.com/TheSlopMachine/llm-router/internal/services/agent"
	configsvc "github.com/TheSlopMachine/llm-router/internal/services/config"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/geoip"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/metrics"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/pluginrepo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
)

//go:embed build/web
var spaFS embed.FS

const sessionCookie = "llmr_session"

// Handler serves the admin dashboard.
type Handler struct {
	adminSvc     *admin.Service
	providerSvc  *provider.Service
	credSvc      *credential.Service
	tokenSvc     *token.Service
	modelInfoSvc *modelinfo.Service
	metricsSvc   *metrics.Service
	agentSvc     *agent.Service
	routerSvc    *router.Service
	configSvc    *configsvc.Service
	luaSvc       *luaplugin.Service
	repoSvc      *pluginrepo.Service
	proxySvc     *proxypool.Service
	geoSvc       *geoip.Service
	logger       *slog.Logger

	// devRedirect, when set, is the origin (e.g. "http://localhost:8080")
	// that browser navigations are 302-redirected to instead of being
	// served from the embedded build/web. Set via SetDevRedirect. See its
	// doc comment for why this exists.
	devRedirect string
}

// New constructs a dashboard Handler.
func New(
	adminSvc *admin.Service,
	providerSvc *provider.Service,
	credSvc *credential.Service,
	tokenSvc *token.Service,
	modelInfoSvc *modelinfo.Service,
	metricsSvc *metrics.Service,
	agentSvc *agent.Service,
	routerSvc *router.Service,
	configSvc *configsvc.Service,
	luaSvc *luaplugin.Service,
	repoSvc *pluginrepo.Service,
	proxySvc *proxypool.Service,
	geoSvc *geoip.Service,
	logger *slog.Logger,
) (*Handler, error) {
	return &Handler{
		adminSvc:     adminSvc,
		providerSvc:  providerSvc,
		credSvc:      credSvc,
		tokenSvc:     tokenSvc,
		modelInfoSvc: modelInfoSvc,
		metricsSvc:   metricsSvc,
		agentSvc:     agentSvc,
		routerSvc:    routerSvc,
		configSvc:    configSvc,
		luaSvc:       luaSvc,
		repoSvc:      repoSvc,
		proxySvc:     proxySvc,
		geoSvc:       geoSvc,
		logger:       logger,
	}, nil
}

// SetDevRedirect configures the handler to 302-redirect any browser
// navigation that would otherwise fall through to the embedded SPA (i.e.
// everything not matched by a more specific route in Register) to origin
// instead, preserving path and query.
//
// This exists because `make start` never runs a real `vite build` — the
// embedded build/web directory is just a placeholder stub so the
// //go:embed directive has something to embed. Without this, hitting the
// dashboard port directly in dev (or any /login, /bootstrap navigation
// not proxied to the dev server) serves that meaningless placeholder
// instead of the actual UI, which is only running on WEB_PORT in dev.
// Production builds never call this, so it has no effect there.
func (h *Handler) SetDevRedirect(origin string) {
	h.devRedirect = strings.TrimSuffix(origin, "/")
}

func (h *Handler) Register(mux *http.ServeMux, db interface{ IsBootstrapped() (bool, error) }) {
	distSub, _ := fs.Sub(spaFS, "build/web")

	// SPA asset files
	mux.Handle("GET /assets/", http.FileServer(http.FS(distSub)))
	mux.Handle("GET /icons/", http.FileServer(http.FS(distSub)))

	// Auth endpoints
	mux.HandleFunc("POST /api/llm-router/login", h.apiLogin)
	mux.HandleFunc("POST /api/llm-router/logout", h.apiLogout)
	mux.HandleFunc("POST /api/llm-router/bootstrap", h.apiBootstrap)
	mux.HandleFunc("GET /api/llm-router/status", h.apiStatus(db))

	// Dashboard APIs
	mux.HandleFunc("GET /api/llm-router/dashboard/providers", h.requireAuth(h.apiProvidersList))
	mux.HandleFunc("POST /api/llm-router/dashboard/providers", h.requireAuth(h.apiProvidersCreate))
	mux.HandleFunc("PUT /api/llm-router/dashboard/providers/{id}", h.requireAuth(h.apiProvidersUpdate))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/providers/{id}", h.requireAuth(h.apiProvidersDelete))
	mux.HandleFunc("GET /api/llm-router/dashboard/adapter-types", h.requireAuth(h.apiAdapterTypes))
	mux.HandleFunc("GET /api/llm-router/dashboard/providers/stats", h.requireAuth(h.apiProvidersStats))
	mux.HandleFunc("GET /api/llm-router/dashboard/providers/{id}/config-schema", h.requireAuth(h.apiProviderConfigSchema))
	mux.HandleFunc("GET /api/llm-router/dashboard/providers/{id}/credential-schema", h.requireAuth(h.apiProviderCredentialSchema))
	mux.HandleFunc("GET /api/llm-router/dashboard/type-schemas", h.requireAuth(h.apiTypeSchemas))

	mux.HandleFunc("GET /api/llm-router/dashboard/tokens", h.requireAuth(h.apiTokensList))
	mux.HandleFunc("POST /api/llm-router/dashboard/tokens", h.requireAuth(h.apiTokensCreate))
	mux.HandleFunc("PUT /api/llm-router/dashboard/tokens/{id}", h.requireAuth(h.apiTokensUpdate))
	mux.HandleFunc("POST /api/llm-router/dashboard/tokens/{id}/regenerate", h.requireAuth(h.apiTokensRegenerate))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/tokens/{id}", h.requireAuth(h.apiTokensDelete))

	mux.HandleFunc("GET /api/llm-router/dashboard/credentials", h.requireAuth(h.apiCredentialsList))
	mux.HandleFunc("POST /api/llm-router/dashboard/credentials", h.requireAuth(h.apiCredentialsCreate))
	mux.HandleFunc("PUT /api/llm-router/dashboard/credentials/reorder", h.requireAuth(h.apiCredentialsReorder))
	mux.HandleFunc("PUT /api/llm-router/dashboard/credentials/{id}", h.requireAuth(h.apiCredentialsUpdate))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/credentials/{id}", h.requireAuth(h.apiCredentialsDelete))
	mux.HandleFunc("POST /api/llm-router/dashboard/credentials/{id}/test", h.requireAuth(h.apiCredentialsTest))

	mux.HandleFunc("GET /api/llm-router/dashboard/models", h.requireAuth(h.apiModels))
	mux.HandleFunc("GET /api/llm-router/dashboard/models/available", h.requireAuth(h.apiAvailableModels))
	mux.HandleFunc("POST /api/llm-router/dashboard/models/test", h.requireAuth(h.apiModelTest))
	mux.HandleFunc("POST /api/llm-router/dashboard/models/capabilities", h.requireAuth(h.apiModelCapabilities))
	mux.HandleFunc("GET /api/llm-router/dashboard/providers/{id}/models", h.requireAuth(h.apiProviderModels))
	mux.HandleFunc("POST /api/llm-router/dashboard/providers/{id}/models/refresh", h.requireAuth(h.apiProviderModelsRefresh))
	mux.HandleFunc("PUT /api/llm-router/dashboard/providers/{id}/models/{model...}", h.requireAuth(h.apiProviderModelSetOverride))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/providers/{id}/models/{model...}", h.requireAuth(h.apiProviderModelDeleteOverride))

	// Agent APIs
	mux.HandleFunc("GET /api/llm-router/dashboard/agents", h.requireAuth(h.apiAgentsList))
	mux.HandleFunc("POST /api/llm-router/dashboard/agents", h.requireAuth(h.apiAgentsCreate))
	mux.HandleFunc("GET /api/llm-router/dashboard/agents/{id}", h.requireAuth(h.apiAgentsGet))
	mux.HandleFunc("PUT /api/llm-router/dashboard/agents/{id}", h.requireAuth(h.apiAgentsUpdate))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/agents/{id}", h.requireAuth(h.apiAgentsDelete))
	mux.HandleFunc("GET /api/llm-router/dashboard/agents/available-models", h.requireAuth(h.apiAgentsAvailableModels))

	// Metrics APIs
	mux.HandleFunc("GET /api/llm-router/dashboard/metrics/overview", h.requireAuth(h.apiMetricsOverview))
	mux.HandleFunc("GET /api/llm-router/dashboard/metrics/timeseries", h.requireAuth(h.apiMetricsTimeSeries))
	mux.HandleFunc("GET /api/llm-router/dashboard/metrics/models", h.requireAuth(h.apiMetricsModels))
	mux.HandleFunc("GET /api/llm-router/dashboard/tokens/usage", h.requireAuth(h.apiTokenUsage))

	// Auth flow endpoints (lua UI-tree wizards)
	mux.HandleFunc("POST /api/llm-router/dashboard/auth/initiate", h.requireAuth(h.authInitiate))
	mux.HandleFunc("POST /api/llm-router/dashboard/auth/step", h.requireAuth(h.authStep))

	// Plugin endpoints
	mux.HandleFunc("GET /api/llm-router/dashboard/plugins", h.requireAuth(h.apiPluginsList))
	mux.HandleFunc("POST /api/llm-router/dashboard/plugins/install-file", h.requireAuth(h.apiPluginsInstallFile))
	mux.HandleFunc("POST /api/llm-router/dashboard/plugins/install-from-repo", h.requireAuth(h.apiPluginsInstallFromRepo))
	mux.HandleFunc("GET /api/llm-router/dashboard/plugins/{id}", h.requireAuth(h.apiPluginsGet))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/plugins/{id}", h.requireAuth(h.apiPluginsDelete))
	mux.HandleFunc("POST /api/llm-router/dashboard/plugins/{id}/rollback", h.requireAuth(h.apiPluginsRollback))
	mux.HandleFunc("GET /api/llm-router/dashboard/plugins/{id}/logs", h.requireAuth(h.apiPluginsLogs))
	mux.HandleFunc("GET /api/llm-router/dashboard/plugins/{id}/crashes", h.requireAuth(h.apiPluginsCrashes))

	// Plugin store endpoints
	mux.HandleFunc("GET /api/llm-router/dashboard/plugin-repos", h.requireAuth(h.apiReposList))
	mux.HandleFunc("POST /api/llm-router/dashboard/plugin-repos", h.requireAuth(h.apiReposAdd))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/plugin-repos/{id}", h.requireAuth(h.apiReposDelete))
	mux.HandleFunc("GET /api/llm-router/dashboard/plugin-repos/{id}/files", h.requireAuth(h.apiReposFiles))
	mux.HandleFunc("GET /api/llm-router/dashboard/plugin-store/search", h.requireAuth(h.apiStoreSearch))
	mux.HandleFunc("GET /api/llm-router/dashboard/plugin-store/updates", h.requireAuth(h.apiStoreUpdates))

	// Router configuration (instance-wide)
	mux.HandleFunc("GET /api/llm-router/dashboard/config", h.requireAuth(h.apiConfigGet))
	mux.HandleFunc("PUT /api/llm-router/dashboard/config", h.requireAuth(h.apiConfigPut))

	// Proxy pool
	mux.HandleFunc("GET /api/llm-router/dashboard/proxies", h.requireAuth(h.apiProxiesList))
	mux.HandleFunc("POST /api/llm-router/dashboard/proxies", h.requireAuth(h.apiProxiesAdd))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/proxies/{id}", h.requireAuth(h.apiProxiesDelete))
	mux.HandleFunc("POST /api/llm-router/dashboard/proxies/{id}/check", h.requireAuth(h.apiProxiesCheck))
	mux.HandleFunc("POST /api/llm-router/dashboard/proxies/check-all", h.requireAuth(h.apiProxiesCheckAll))
	mux.HandleFunc("GET /api/llm-router/dashboard/proxy-sources", h.requireAuth(h.apiProxySources))
	mux.HandleFunc("POST /api/llm-router/dashboard/proxy-sources/{key}/refresh", h.requireAuth(h.apiProxySourceRefresh))
	mux.HandleFunc("GET /api/llm-router/dashboard/proxy/status", h.requireAuth(h.apiProxyStatus))

	// Chat proxy (dashboard session -> router, no token required)
	mux.HandleFunc("POST /api/llm-router/dashboard/chat/completions", h.requireAuth(h.apiChatCompletions))

	// SPA fallback
	indexHTML, _ := distSub.Open("index.html")
	indexBytes, _ := io.ReadAll(indexHTML)
	indexHTML.Close()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Unknown API paths are a JSON 404, never an SPA redirect: in dev the
		// Vite proxy would otherwise follow the redirect back into itself.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(models.ErrorResponse{Error: "unknown endpoint"})
			return
		}
		if h.devRedirect != "" {
			target := h.devRedirect + r.URL.Path
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusFound)
			return
		}

		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(indexBytes)
			return
		}

		file, err := distSub.Open(r.URL.Path[1:])
		if err != nil {
			// File not found - serve index.html for SPA client-side routing
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(indexBytes)
			return
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil || stat.IsDir() {
			// Error or directory - serve index.html for SPA client-side routing
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(indexBytes)
			return
		}

		http.ServeContent(w, r, stat.Name(), stat.ModTime(), file.(io.ReadSeeker))
	})
}

// serve404 returns a styled 404 error page matching the app's design system
func (h *Handler) serve404(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)

	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>404 Not Found - llm-router</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&display=swap" rel="stylesheet">
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: "Inter", system-ui, -apple-system, sans-serif;
            font-size: 14px;
            line-height: 1.5;
            background: #fafafa;
            color: #2b2d31;
            display: flex;
            align-items: center;
            justify-content: center;
            min-height: 100vh;
            padding: 20px;
        }
        .container {
            background: #ffffff;
            border: 1px solid #e2e3e4;
            border-radius: 16px;
            padding: 48px;
            max-width: 500px;
            width: 100%;
            text-align: center;
            box-shadow: 0 4px 6px -1px rgba(10, 13, 18, 0.1), 0 2px 4px -2px rgba(10, 13, 18, 0.06);
        }
        h1 {
            font-size: 48px;
            font-weight: 600;
            color: #6c717a;
            margin-bottom: 16px;
        }
        h2 {
            font-size: 24px;
            font-weight: 400;
            color: #2b2d31;
            margin-bottom: 12px;
        }
        p {
            color: #6c717a;
            margin-bottom: 32px;
            font-size: 14px;
        }
        .path {
            font-family: "DM Mono", "SF Mono", "Fira Code", monospace;
            font-size: 13px;
            background: #f4f5f5;
            padding: 8px 12px;
            border-radius: 6px;
            color: #2b2d31;
            margin-bottom: 32px;
            word-break: break-all;
        }
        a {
            display: inline-flex;
            align-items: center;
            justify-content: center;
            padding: 0 24px;
            height: 40px;
            background: #ffffff;
            border: 1px solid #e2e3e4;
            border-radius: 12px;
            color: #2b2d31;
            text-decoration: none;
            font-weight: 500;
            transition: background 0.15s ease;
        }
        a:hover {
            background: #eaeaeb;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>404</h1>
        <h2>Page Not Found</h2>
        <p>The page you requested does not exist.</p>
        <div class="path">` + r.URL.Path + `</div>
        <a href="/">Return to Dashboard</a>
    </div>
</body>
</html>`

	w.Write([]byte(html))
}

func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			h.jsonErr(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		if _, ok := h.adminSvc.ValidateSession(c.Value); !ok {
			h.jsonErr(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		next(w, r)
	}
}

func (h *Handler) json(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) jsonErr(w http.ResponseWriter, status int, msg string) {
	h.json(w, status, map[string]string{"error": msg})
}
