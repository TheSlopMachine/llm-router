// Package server wires all services together and starts the HTTP servers.
// Dashboard and /v1 API run on separate ports with independent muxes.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/adapters/generic"
	v1 "github.com/TheSlopMachine/llm-router/internal/api/v1"
	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/dashboard"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/services/admin"
	"github.com/TheSlopMachine/llm-router/internal/services/agent"
	"github.com/TheSlopMachine/llm-router/internal/services/auth"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/maintenance"
	"github.com/TheSlopMachine/llm-router/internal/services/metrics"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/providers/agents"
)

// Server is the fully-wired llm-router application with two listeners.
type Server struct {
	cfg          *config.Config
	db           *db.DB
	logger       *slog.Logger
	dashboardSrv *http.Server
	apiSrv       *http.Server
	maintSvc     *maintenance.Service
	metricsSvc   *metrics.Service
}

// New builds the full Server from config.
func New(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	dashboardAddr := cfg.DashboardAddr
	if dashboardAddr == "" {
		return nil, fmt.Errorf("dashboard listen address is required")
	}
	apiAddr := cfg.APIAddr
	if apiAddr == "" {
		return nil, fmt.Errorf("api listen address is required")
	}

	providerSvc := provider.NewService(database)
	adminSvc := admin.New(database, providerSvc)
	tokenSvc := token.NewWithTestingKey(database, cfg.TestingKey)
	authSvc := auth.New(database)
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)
	agentSvc := agent.New(database, providerSvc, modelInfoSvc)
	routerSvc := router.New(providerSvc, credSvc, modelInfoSvc, cfg.MaxCredentialRetries, logger)
	maintSvc := maintenance.New(credSvc, providerSvc, database, logger)
	metricsSvc := metrics.New(database, logger)
	metricsSvc.Start()

	// Wire generic (custom) adapter to single source of truth — no per-call injection.
	generic.SetResolver(func(qualifier string) (string, error) {
		cp, err := providerSvc.GetCustom(qualifier)
		if err != nil {
			return "", err
		}
		return cp.BaseURL, nil
	})
	generic.SetLogger(logger)
	providerSvc.SetLogger(logger)
	modelInfoSvc.SetLogger(logger)

	// One-time GC of orphans from previous buggy deletes (covers double-prefix rows).
	if n, err := providerSvc.CleanupOrphanedCredentials(); err != nil {
		logger.Warn("orphan credential GC failed", "err", err)
	} else if n > 0 {
		logger.Info("orphan credential GC completed", "count", n)
	}

	// Invalidate model cache immediately after custom provider CRUD — "usable immediately".
	providerSvc.SetOnChanged(func(providerID string) {
		_ = modelInfoSvc.InvalidateProvider(providerID)
	})
	credSvc.SetOnChanged(func(providerID string) {
		_ = modelInfoSvc.InvalidateProvider(providerID)
	})

	// Initialize agents adapter with dependencies
	if agentsAdapter, err := provider.Lookup("agents"); err == nil {
		if a, ok := agentsAdapter.(*agents.Adapter); ok {
			a.SetRouterService(routerSvc)
			a.SetAgentService(agentSvc)
			a.SetLogger(logger)
		}
	}

	// Dashboard mux (SPA + /api/llm-router/*)
	dashMux := http.NewServeMux()
	dash, err := dashboard.New(adminSvc, providerSvc, credSvc, tokenSvc, authSvc, modelInfoSvc, metricsSvc, agentSvc, routerSvc, logger)
	if err != nil {
		return nil, fmt.Errorf("build dashboard handler: %w", err)
	}
	dash.Register(dashMux, database)
	dashHandler := bootstrapMiddleware(database)(requestLogger(logger, dashMux))

	// API mux (/v1/* only, no bootstrap redirect ever)
	apiMux := http.NewServeMux()
	apiV1 := v1.New(tokenSvc, routerSvc, metricsSvc, providerSvc, modelInfoSvc, agentSvc, logger)
	apiV1.Register(apiMux)

	apiHandler := requestLogger(logger, apiMux)

	dashboardSrv := &http.Server{
		Addr:    dashboardAddr,
		Handler: dashHandler,
	}
	apiSrv := &http.Server{
		Addr:    apiAddr,
		Handler: apiHandler,
	}

	return &Server{
		cfg:          cfg,
		db:           database,
		logger:       logger,
		dashboardSrv: dashboardSrv,
		apiSrv:       apiSrv,
		maintSvc:     maintSvc,
		metricsSvc:   metricsSvc,
	}, nil
}

// Run starts the maintenance loop and blocks on both HTTP servers.
func (s *Server) Run(ctx context.Context) error {
	s.maintSvc.Start(ctx)
	s.logger.Info("llm-router started", "dashboard", s.cfg.DashboardAddr, "api", s.cfg.APIAddr, "db", s.cfg.DBPath)

	errCh := make(chan error, 2)
	go func() {
		if err := s.dashboardSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("dashboard server: %w", err)
		}
	}()
	go func() {
		if err := s.apiSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("api server: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("shutting down gracefully...")
		s.metricsSvc.Stop()
		_ = s.dashboardSrv.Shutdown(context.Background())
		_ = s.apiSrv.Shutdown(context.Background())
		return nil
	case err := <-errCh:
		s.metricsSvc.Stop()
		_ = s.dashboardSrv.Shutdown(context.Background())
		_ = s.apiSrv.Shutdown(context.Background())
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
