package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/dashboard"
	"github.com/TheSlopMachine/llm-router/internal/requestsource"
	"github.com/TheSlopMachine/llm-router/internal/services/admin"
)

func bootstrapMiddleware(adminService *admin.GormService, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The admin listener answers without bootstrapping; the
			// source in the request context decides, never a flag.
			private := requestsource.SourceFromContext(r.Context()) == requestsource.RequestSourcePrivate
			if private {
				if logger != nil {
					logger.Debug("Private request bypasses bootstrap check", "endpoint", r.URL.Path, "method", r.Method)
				}
				next.ServeHTTP(w, r)
				return
			}
			exempt := r.URL.Path == "/login" ||
				r.URL.Path == "/bootstrap" ||
				r.URL.Path == "/api/llm-router/login" ||
				r.URL.Path == "/api/llm-router/logout" ||
				r.URL.Path == "/api/llm-router/bootstrap" ||
				r.URL.Path == "/api/llm-router/status" ||
				strings.HasPrefix(r.URL.Path, "/assets/") ||
				strings.HasPrefix(r.URL.Path, "/icons/")

			if exempt {
				if logger != nil {
					logger.Debug("Exempt path bypasses bootstrap check", "endpoint", r.URL.Path, "method", r.Method)
				}
				next.ServeHTTP(w, r)
				return
			}
			real, err := adminService.IsBootstrapped(r.Context())
			if err != nil {
				if logger != nil {
					logger.Warn("Bootstrap check fails", "endpoint", r.URL.Path, "error", err)
				}
				// API prefixes answer JSON, never redirect: a client
				// asking for JSON must not receive dashboard HTML.
				if dashboard.IsAPIPath(r.URL.Path) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusServiceUnavailable)
					json.NewEncoder(w).Encode(map[string]string{
						"error": "system not bootstrapped",
					})
					return
				}
				http.Redirect(w, r, "/bootstrap", http.StatusSeeOther)
				return
			}
			if !dashboard.EffectiveBootstrapped(real, private) {
				// API prefixes answer JSON, never redirect: a client
				// asking for JSON must not receive dashboard HTML.
				if dashboard.IsAPIPath(r.URL.Path) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusServiceUnavailable)
					json.NewEncoder(w).Encode(map[string]string{
						"error": "system not bootstrapped",
					})
					return
				}
				http.Redirect(w, r, "/bootstrap", http.StatusSeeOther)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		if logger != nil {
			logger.Debug("Request completes", "method", r.Method, "endpoint", r.URL.Path, "status", rw.status, "latency_ms", time.Since(start).Milliseconds())
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
