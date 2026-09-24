package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/db"
)

func bootstrapMiddleware(database *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			exempt := r.URL.Path == "/login" ||
				r.URL.Path == "/bootstrap" ||
				r.URL.Path == "/api/llm-router/login" ||
				r.URL.Path == "/api/llm-router/logout" ||
				r.URL.Path == "/api/llm-router/bootstrap" ||
				r.URL.Path == "/api/llm-router/status" ||
				strings.HasPrefix(r.URL.Path, "/assets/") ||
				strings.HasPrefix(r.URL.Path, "/icons/")

			if !exempt {
				ok, err := database.IsBootstrapped()
				if err != nil || !ok {
					if strings.HasPrefix(r.URL.Path, "/api/") {
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
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		logger.Info("→", "method", r.Method, "path", r.URL.Path, "status", rw.status)
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
