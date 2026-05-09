// Package dashboard serves the admin SPA and its backing JSON API.
package dashboard

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"

	httpSwagger "github.com/swaggo/http-swagger/v2"

	"github.com/TheSlopMachine/llm-router/internal/services/admin"
	"github.com/TheSlopMachine/llm-router/internal/services/agent"
	"github.com/TheSlopMachine/llm-router/internal/services/auth"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/metrics"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
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
	authSvc      *auth.Service
	modelInfoSvc *modelinfo.Service
	metricsSvc   *metrics.Service
	agentSvc     *agent.Service
	logger       *slog.Logger
}

// New constructs a dashboard Handler.
func New(
	adminSvc *admin.Service,
	providerSvc *provider.Service,
	credSvc *credential.Service,
	tokenSvc *token.Service,
	authSvc *auth.Service,
	modelInfoSvc *modelinfo.Service,
	metricsSvc *metrics.Service,
	agentSvc *agent.Service,
	logger *slog.Logger,
) (*Handler, error) {
	return &Handler{
		adminSvc:     adminSvc,
		providerSvc:  providerSvc,
		credSvc:      credSvc,
		tokenSvc:     tokenSvc,
		authSvc:      authSvc,
		modelInfoSvc: modelInfoSvc,
		metricsSvc:   metricsSvc,
		agentSvc:     agentSvc,
		logger:       logger,
	}, nil
}

// Register mounts all dashboard routes.
func (h *Handler) Register(mux *http.ServeMux, db interface{ IsBootstrapped() (bool, error) }) {
	distSub, _ := fs.Sub(spaFS, "build/web")

	// Vite asset files
	mux.Handle("GET /assets/", http.FileServer(http.FS(distSub)))
	mux.Handle("GET /icons/", http.FileServer(http.FS(distSub)))

	// Auth endpoints
	mux.HandleFunc("POST /api/llm-router/login", h.apiLogin)
	mux.HandleFunc("POST /api/llm-router/logout", h.apiLogout)
	mux.HandleFunc("POST /api/llm-router/bootstrap", h.apiBootstrap)
	mux.HandleFunc("GET /api/llm-router/status", h.apiStatus(db))

	// Dashboard APIs
	mux.HandleFunc("GET /api/llm-router/dashboard/providers", h.requireAuth(h.apiProvidersList))
	mux.HandleFunc("GET /api/llm-router/dashboard/adapter-types", h.requireAuth(h.apiAdapterTypes))
	mux.HandleFunc("GET /api/llm-router/dashboard/providers/stats", h.requireAuth(h.apiProvidersStats))

	mux.HandleFunc("GET /api/llm-router/dashboard/tokens", h.requireAuth(h.apiTokensList))
	mux.HandleFunc("POST /api/llm-router/dashboard/tokens", h.requireAuth(h.apiTokensCreate))
	mux.HandleFunc("PUT /api/llm-router/dashboard/tokens/{id}", h.requireAuth(h.apiTokensUpdate))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/tokens/{id}", h.requireAuth(h.apiTokensDelete))

	mux.HandleFunc("GET /api/llm-router/dashboard/credentials", h.requireAuth(h.apiCredentialsList))
	mux.HandleFunc("DELETE /api/llm-router/dashboard/credentials/{id}", h.requireAuth(h.apiCredentialsDelete))

	mux.HandleFunc("GET /api/llm-router/dashboard/models", h.requireAuth(h.apiModels))

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

	// Auth flow endpoints
	mux.HandleFunc("POST /api/llm-router/dashboard/auth/start", h.requireAuth(h.authStart))
	mux.HandleFunc("POST /api/llm-router/dashboard/auth/callback", h.requireAuth(h.authCallback))

	// Swagger UI
	mux.HandleFunc("GET /swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
		httpSwagger.DeepLinking(true),
		httpSwagger.DocExpansion("none"),
		httpSwagger.DomID("swagger-ui"),
	))

	// SPA fallback
	indexHTML, _ := distSub.Open("index.html")
	indexBytes, _ := io.ReadAll(indexHTML)
	indexHTML.Close()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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

