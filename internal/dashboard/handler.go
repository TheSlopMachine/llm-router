// Package dashboard serves the admin SPA and its backing JSON API.
//
// @title        llm-router API
// @version      0.5.0
// @description  OpenAI-compatible plus Anthropic-compatible LLM routing gateway.
// @description  Public AI surface (/v1, BearerAuth or x-api-key alias):
// @description  chat/completions, completions, embeddings, images/generations,
// @description  images/edits, images/variations, audio/speech,
// @description  audio/transcriptions (multipart or JSON input_audio),
// @description  audio/translations, moderations, videos submit/poll/content,
// @description  messages, messages/count_tokens, messages/batches, complete,
// @description  models list/retrieve (TokenRules-filtered; Anthropic shape
// @description  when anthropic-version is present), responses (+cancel,
// @description  input_items, compact, input_tokens), conversations (+items),
// @description  assistants, threads (+messages, runs, cancel,
// @description  submit_tool_outputs). Derivative-client aliases (Mistral
// @description  random_seed/output_dimension, Ollama options/format,
// @description  OpenRouter reasoning envelope) translate at the edge; no
// @description  separate paths. Deprecated upstream flags are ignored: prior
// @description  generation endpoints stay first-class, never deprecated:true.
// @description  Dashboard surface (/api/llm-router, SessionAuth) is unchanged.
//
// @securityDefinitions.apikey BearerAuth
// @in                          header
// @name                        Authorization
// @description                 Router token as "Bearer llmr_*".
//
// @securityDefinitions.apikey SessionAuth
// @in                          cookie
// @name                        llmr_session
// @description                 Dashboard login session cookie.
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
	configsvc "github.com/TheSlopMachine/llm-router/internal/services/config"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/datamanagement"
	"github.com/TheSlopMachine/llm-router/internal/services/doctor"
	"github.com/TheSlopMachine/llm-router/internal/services/geoban"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/metrics"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/pluginrepo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
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
	virtualSvc   *virtual.Service
	routerSvc    *router.Service
	configSvc    *configsvc.Service
	luaSvc       *luaplugin.Service
	repoSvc      *pluginrepo.Service
	proxySvc     *proxypool.Service
	geobanSvc    *geoban.Service
	dataSvc      *datamanagement.Service
	doctorSvc    *doctor.Service
	logger       *slog.Logger
	noAuth       bool

	// devRedirect, when set, is the origin (e.g. "http://localhost:8080")
	// that browser navigations are 302-redirected to instead of being
	// served from the embedded build/web. Set via SetDevRedirect. See its
	// doc comment for why this exists.
	devRedirect string
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
	mux.HandleFunc("POST /api/llm-router/dashboard/admin/password", h.requireAuth(h.apiAdminChangePassword))
	mux.HandleFunc("GET /api/llm-router/status", h.apiStatus(db))

	// Dashboard APIs
	mux.HandleFunc("GET /api/llm-router/dashboard/providers", h.requireAuth(h.apiProvidersList))
	mux.HandleFunc("POST /api/llm-router/dashboard/providers", h.requireAuth(h.apiProvidersCreate))
	mux.HandleFunc("PUT /api/llm-router/dashboard/providers/{id}", h.requireAuth(h.apiProvidersUpdate))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/providers/{id}", h.requireAuth(h.apiProvidersDelete))
	mux.HandleFunc("GET /api/llm-router/dashboard/providers/stats", h.requireAuth(h.apiProvidersStats))
	mux.HandleFunc("GET /api/llm-router/dashboard/providers/{id}/config-schema", h.requireAuth(h.apiProviderConfigSchema))
	mux.HandleFunc("GET /api/llm-router/dashboard/providers/{id}/credential-schema", h.requireAuth(h.apiProviderCredentialSchema))

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
	mux.HandleFunc("POST /api/llm-router/dashboard/credentials/{id}/refresh", h.requireAuth(h.apiCredentialsRefresh))

	mux.HandleFunc("GET /api/llm-router/dashboard/models", h.requireAuth(h.apiModels))
	mux.HandleFunc("GET /api/llm-router/dashboard/models/available", h.requireAuth(h.apiAvailableModels))
	mux.HandleFunc("POST /api/llm-router/dashboard/models/test", h.requireAuth(h.apiModelTest))
	mux.HandleFunc("POST /api/llm-router/dashboard/models/capabilities", h.requireAuth(h.apiModelCapabilities))
	mux.HandleFunc("GET /api/llm-router/dashboard/providers/{id}/models", h.requireAuth(h.apiProviderModels))
	mux.HandleFunc("POST /api/llm-router/dashboard/providers/{id}/models/refresh", h.requireAuth(h.apiProviderModelsRefresh))
	mux.HandleFunc("GET /api/llm-router/dashboard/providers/{id}/virtual-models", h.requireAuth(h.apiProviderVirtualModels))
	mux.HandleFunc("POST /api/llm-router/dashboard/providers/{id}/virtual-models/sync", h.requireAuth(h.apiProviderVirtualModelsSync))
	mux.HandleFunc("PUT /api/llm-router/dashboard/providers/{id}/models/{model...}", h.requireAuth(h.apiProviderModelSetOverride))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/providers/{id}/models/{model...}", h.requireAuth(h.apiProviderModelDeleteOverride))

	// Geo bans: indefinite (provider, proxy) flags
	mux.HandleFunc("GET /api/llm-router/dashboard/providers/{id}/geo-bans", h.requireAuth(h.apiGeoBansList))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/providers/{id}/geo-bans", h.requireAuth(h.apiGeoBansClearAll))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/providers/{id}/geo-bans/{proxyId}", h.requireAuth(h.apiGeoBansClearOne))

	// Virtual model APIs
	mux.HandleFunc("GET /api/llm-router/dashboard/virtual-models", h.requireAuth(h.apiVirtualModelsList))
	mux.HandleFunc("POST /api/llm-router/dashboard/virtual-models", h.requireAuth(h.apiVirtualModelsCreate))
	mux.HandleFunc("GET /api/llm-router/dashboard/virtual-models/{id}", h.requireAuth(h.apiVirtualModelsGet))
	mux.HandleFunc("PUT /api/llm-router/dashboard/virtual-models/{id}", h.requireAuth(h.apiVirtualModelsUpdate))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/virtual-models/{id}", h.requireAuth(h.apiVirtualModelsDelete))
	// Deprecated agents aliases: same handlers, removed once the frontend migrates.
	mux.HandleFunc("GET /api/llm-router/dashboard/agents", h.requireAuth(h.apiVirtualModelsList))
	mux.HandleFunc("POST /api/llm-router/dashboard/agents", h.requireAuth(h.apiVirtualModelsCreate))
	mux.HandleFunc("GET /api/llm-router/dashboard/agents/{id}", h.requireAuth(h.apiVirtualModelsGet))
	mux.HandleFunc("PUT /api/llm-router/dashboard/agents/{id}", h.requireAuth(h.apiVirtualModelsUpdate))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/agents/{id}", h.requireAuth(h.apiVirtualModelsDelete))

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

	// Data management
	mux.HandleFunc("GET /api/llm-router/dashboard/data/stats", h.requireAuth(h.apiDataStats))
	mux.HandleFunc("GET /api/llm-router/dashboard/data/{subsystem}/export", h.requireAuth(h.apiDataExport))
	mux.HandleFunc("POST /api/llm-router/dashboard/data/{subsystem}/import", h.requireAuth(h.apiDataImport))
	mux.HandleFunc("POST /api/llm-router/dashboard/data/{subsystem}/clear", h.requireAuth(h.apiDataClear))
	mux.HandleFunc("GET /api/llm-router/dashboard/providers/{id}/export", h.requireAuth(h.apiProviderExport))
	mux.HandleFunc("POST /api/llm-router/dashboard/providers/import", h.requireAuth(h.apiProviderImport))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/providers/{id}/purge", h.requireAuth(h.apiProviderPurge))

	// Database Doctor
	mux.HandleFunc("GET /api/llm-router/dashboard/doctor/inspect", h.requireAuth(h.apiDoctorInspect))
	mux.HandleFunc("POST /api/llm-router/dashboard/doctor/fix", h.requireAuth(h.apiDoctorFix))

	// Proxy pool
	mux.HandleFunc("GET /api/llm-router/dashboard/proxies", h.requireAuth(h.apiProxiesList))
	mux.HandleFunc("POST /api/llm-router/dashboard/proxies/refresh", h.requireAuth(h.apiProxyRefresh))
	mux.HandleFunc("GET /api/llm-router/dashboard/proxy-sources", h.requireAuth(h.apiProxySources))
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

func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.noAuth {
			next(w, r)
			return
		}
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

// sessionUser resolves the authenticated username from the session cookie.
func (h *Handler) sessionUser(r *http.Request) string {
	if h.noAuth {
		return "admin"
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return ""
	}
	username, ok := h.adminSvc.ValidateSession(c.Value)
	if !ok {
		return ""
	}
	return username
}

func (h *Handler) json(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) jsonErr(w http.ResponseWriter, status int, msg string) {
	h.json(w, status, map[string]string{"error": msg})
}
