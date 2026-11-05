package server

import (
	"encoding/json"
	"log/slog"
	"net"
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
			if err != nil && logger != nil {
				logger.Warn("Bootstrap check fails", "endpoint", r.URL.Path, "error", err)
			}
			// A failed check denies like an unbootstrapped instance: fail closed.
			if err != nil || !dashboard.EffectiveBootstrapped(real, private) {
				// API prefixes answer JSON, never redirect: a client
				// asking for JSON must not receive dashboard HTML.
				if dashboard.IsAPIPath(r.URL.Path) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusServiceUnavailable)
					_ = json.NewEncoder(w).Encode(map[string]string{
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

// loopbackHostMiddleware answers 421 unless the Host header names the admin
// listener itself (127.0.0.1, localhost or [::1] on its port). The admin
// listener has no authentication and binds loopback, so a browser page that
// rebinds its own domain to 127.0.0.1 could otherwise call it: the page's
// Host header still carries the attacker's domain and fails this check.
func loopbackHostMiddleware(adminAddress string, logger *slog.Logger) func(http.Handler) http.Handler {
	_, port, err := net.SplitHostPort(adminAddress)
	allowed := map[string]bool{}
	if err == nil {
		for _, host := range []string{"127.0.0.1", "localhost", "::1"} {
			allowed[net.JoinHostPort(host, port)] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !allowed[strings.ToLower(r.Host)] {
				if logger != nil {
					logger.Warn("Admin listener rejects foreign host", "endpoint", r.URL.Path, "reason", "foreign_host", "detail", r.Host)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusMisdirectedRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "admin listener accepts loopback hosts only"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// securityHeadersMiddleware sets browser hardening headers on every public
// response: no framing (clickjacking), no MIME sniffing, no cross-origin
// referrer leaks. The CSP restricts framing, plugins and base URLs only; it
// leaves script and style sources open so the SPA build keeps working.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Frame-Options", "DENY")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "same-origin")
		header.Set("Content-Security-Policy", "frame-ancestors 'none'; base-uri 'self'; object-src 'none'")
		next.ServeHTTP(w, r)
	})
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

// Unwrap exposes the underlying writer to http.ResponseController.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
