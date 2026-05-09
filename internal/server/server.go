// Package server wires all services together and starts the HTTP server.
// This is the composition root of the application.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	v1 "github.com/TheSlopMachine/llm-router/internal/api/v1"
	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/dashboard"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/services/admin"
	"github.com/TheSlopMachine/llm-router/internal/services/agent"
	"github.com/TheSlopMachine/llm-router/internal/services/auth"
	"github.com/TheSlopMachine/llm-router/internal/services/compaction"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/maintenance"
	"github.com/TheSlopMachine/llm-router/internal/services/metrics"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/services/tokencount"
	"github.com/TheSlopMachine/llm-router/providers/agents"
)

// Server is the fully-wired llm-router application.
type Server struct {
	cfg        *config.Config
	db         *db.DB
	logger     *slog.Logger
	http       *http.Server
	maintSvc   *maintenance.Service
	metricsSvc *metrics.Service
}

// New builds the full Server from config.
func New(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	providerSvc := provider.NewService(database)
	adminSvc := admin.New(database, providerSvc)
	tokenSvc := token.New(database)
	authSvc := auth.New(database)
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)
	agentSvc := agent.New(database, providerSvc, modelInfoSvc)
	tokenCountSvc := tokencount.New()
	compactionSvc := compaction.New(tokenCountSvc, logger)
	routerSvc := router.New(providerSvc, credSvc, modelInfoSvc, compactionSvc, cfg.MaxCredentialRetries, logger)
	maintSvc := maintenance.New(credSvc, providerSvc, database, logger)
	metricsSvc := metrics.New(database, logger)
	metricsSvc.Start()

	// Initialize agents adapter with dependencies
	if agentsAdapter, err := provider.Lookup("agents"); err == nil {
		if a, ok := agentsAdapter.(*agents.Adapter); ok {
			a.SetRouterService(routerSvc)
			a.SetAgentService(agentSvc)
			a.SetLogger(logger)
		}
	}

	mux := http.NewServeMux()

	dash, err := dashboard.New(adminSvc, providerSvc, credSvc, tokenSvc, authSvc, modelInfoSvc, metricsSvc, agentSvc, logger)
	if err != nil {
		return nil, fmt.Errorf("build dashboard handler: %w", err)
	}
	dash.Register(mux, database)

	apiV1 := v1.New(tokenSvc, routerSvc, metricsSvc, logger)
	apiV1.Register(mux)

	handler := bootstrapMiddleware(database)(requestLogger(logger, mux))

	httpSrv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
	}

	return &Server{
		cfg:        cfg,
		db:         database,
		logger:     logger,
		http:       httpSrv,
		maintSvc:   maintSvc,
		metricsSvc: metricsSvc,
	}, nil
}

// Run starts the maintenance loop and blocks on the HTTP server.
func (s *Server) Run(ctx context.Context) error {
	s.maintSvc.Start(ctx)
	s.logger.Info("llm-router started", "addr", s.cfg.ListenAddr, "db", s.cfg.DBPath)

	errCh := make(chan error, 1)
	go func() {
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("shutting down gracefully...")
		s.metricsSvc.Stop()
		return s.http.Shutdown(context.Background())
	case err := <-errCh:
		s.metricsSvc.Stop()
		return fmt.Errorf("http server: %w", err)
	}
}

// Close releases the database handle. Call after Run returns.
func (s *Server) Close() error {
	return s.db.Close()
}

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
				strings.HasPrefix(r.URL.Path, "/icons/") ||
				strings.HasPrefix(r.URL.Path, "/v1/")

			if !exempt {
				ok, err := database.IsBootstrapped()
				if err != nil || !ok {
					// Check if this is an API request
					if strings.HasPrefix(r.URL.Path, "/api/") {
						// Return JSON error for API requests
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusServiceUnavailable)
						json.NewEncoder(w).Encode(map[string]string{
							"error": "system not bootstrapped",
						})
						return
					}
					// Redirect HTML requests to bootstrap page
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

