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
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/admin"
	configsvc "github.com/TheSlopMachine/llm-router/internal/services/config"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/maintenance"
	"github.com/TheSlopMachine/llm-router/internal/services/metrics"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/pluginrepo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
	virtualadapter "github.com/TheSlopMachine/llm-router/providers/virtual"
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
	proxySvc     *proxypool.Service
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

	luaSvc, err := luaplugin.New(database, logger)
	if err != nil {
		return nil, fmt.Errorf("init plugin service: %w", err)
	}

	providerSvc := provider.NewService(database)
	providerSvc.SetLogger(logger)
	providerSvc.SetLuaService(luaSvc)
	providerSvc.RegisterGoAdapter(&generic.Adapter{})
	virtualAdapter := &virtualadapter.Adapter{}
	providerSvc.RegisterGoAdapter(virtualAdapter)
	if err := providerSvc.EnsureSeeded(); err != nil {
		return nil, fmt.Errorf("seed providers: %w", err)
	}

	adminSvc := admin.New(database, providerSvc)
	tokenSvc := token.NewWithTestingKey(database, cfg.TestingKey)
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)
	virtualSvc := virtual.New(database, providerSvc, modelInfoSvc)
	configSvc := configsvc.New(database)
	repoSvc := pluginrepo.New(database)
	if err := repoSvc.PruneLegacyRepos(); err != nil {
		return nil, fmt.Errorf("prune legacy plugin repos: %w", err)
	}
	if err := repoSvc.EnsureBuiltinRepos(); err != nil {
		return nil, fmt.Errorf("seed built-in plugin repos: %w", err)
	}
	routerCfg, _ := configSvc.Get()
	routerSvc := router.New(providerSvc, credSvc, modelInfoSvc, routerCfg.MaxRetries, logger)

	// Proxy subsystem: pool, plugin proxy resolution, pair outcome reports.
	proxySvc := proxypool.New(database)
	proxySvc.SetConfig(routerCfg.MinDownloadSpeedKbps, routerCfg.MaxProxiesPerLocation)
	luaSvc.SetProxyResolver(func(rec *luaplugin.PluginRecord, providerConfig map[string]any) ([]luaplugin.ProxyPick, error) {
		mode, ids := proxyMode(providerConfig)
		typeKey := ""
		if len(rec.TypeKeys) > 0 {
			typeKey = rec.TypeKeys[0]
		}
		picks, err := proxySvc.Rank(rec.ProxyLocations, mode, ids, typeKey)
		if err != nil {
			return nil, err
		}
		out := make([]luaplugin.ProxyPick, 0, len(picks))
		for _, p := range picks {
			out = append(out, luaplugin.ProxyPick{ID: p.ID, URL: p.URL})
		}
		return out, nil
	})
	luaSvc.SetProxyEventReporter(func(ev luaplugin.ProxyEvent) {
		if ev.RateLimited {
			proxySvc.RecordRateLimit(ev.ProxyID, ev.Provider, ev.ResetsAt)
		}
		if ev.Blocked {
			proxySvc.RecordBlocked(ev.ProxyID, ev.Provider, ev.BlockReason)
		}
	})

	maintSvc := maintenance.New(credSvc, providerSvc, database, logger)
	maintSvc.SetProxyServices(proxySvc, luaSvc)
	maintSvc.SetProxyTickInterval(proxyTickInterval(routerCfg))
	maintSvc.SetModelInfoService(modelInfoSvc)
	configSvc.SetOnChanged(func(cfg models.RouterConfiguration) {
		routerSvc.SetMaxRetries(cfg.MaxRetries)
		proxySvc.SetConfig(cfg.MinDownloadSpeedKbps, cfg.MaxProxiesPerLocation)
		maintSvc.SetProxyTickInterval(proxyTickInterval(cfg))
	})
	metricsSvc := metrics.New(database, logger)
	metricsSvc.Start()

	modelInfoSvc.SetLogger(logger)

	// Model caches persist in bbolt; warm the missing ones in the background
	// so first clicks never wait on upstream discovery.
	go modelInfoSvc.WarmMissing(context.Background())

	// Virtual models resolve from the model name and need no credentials;
	// rows left over from the credential-bound era are dead weight.
	for _, key := range []string{"agents", provider.TypeVirtual} {
		if n, err := credSvc.DeleteByProvider(key); err != nil {
			logger.Warn("virtual credential cleanup failed", "provider", key, "err", err)
		} else if n > 0 {
			logger.Info("virtual credential cleanup completed", "provider", key, "count", n)
		}
	}

	if n, err := providerSvc.CleanupOrphanedCredentials(); err != nil {
		logger.Warn("orphan credential GC failed", "err", err)
	} else if n > 0 {
		logger.Info("orphan credential GC completed", "count", n)
	}

	invalidate := func(providerID string) {
		_ = modelInfoSvc.InvalidateProvider(providerID)
	}
	providerSvc.SetOnChanged(invalidate)
	credSvc.SetOnChanged(invalidate)
	luaSvc.SetOnChanged(func(typeKey string) {
		// Runtime plugin changes (install/update/rollback/enable) must
		// create default provider rows, previously seeded at startup.
		if err := providerSvc.SyncDefaultProviders(); err != nil {
			logger.Warn("sync default providers failed", "err", err)
		}
		providers, err := providerSvc.GetByType(typeKey)
		if err != nil {
			return
		}
		for _, p := range providers {
			_ = modelInfoSvc.InvalidateProvider(p.ID)
		}
	})

	virtualAdapter.SetRouterService(routerSvc)
	virtualAdapter.SetVirtualService(virtualSvc)
	virtualAdapter.SetLogger(logger)

	dashMux := http.NewServeMux()
	dash, err := dashboard.New(adminSvc, providerSvc, credSvc, tokenSvc, modelInfoSvc, metricsSvc, virtualSvc, routerSvc, configSvc, luaSvc, repoSvc, proxySvc, logger)
	if err != nil {
		return nil, fmt.Errorf("build dashboard handler: %w", err)
	}
	if cfg.DevUIRedirect != "" {
		dash.SetDevRedirect(cfg.DevUIRedirect)
	}
	dash.Register(dashMux, database)
	dashHandler := bootstrapMiddleware(database)(requestLogger(logger, dashMux))

	apiMux := http.NewServeMux()
	apiV1 := v1.New(tokenSvc, routerSvc, metricsSvc, providerSvc, modelInfoSvc, virtualSvc, logger)
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
		proxySvc:     proxySvc,
	}, nil
}

// proxyMode reads the provider-level proxy mode from
// ProviderInstance.Config. Unknown shapes fall back to disabled.
func proxyMode(providerConfig map[string]any) (mode string, ids []string) {
	mode = models.ProxyModeDisabled
	raw, ok := providerConfig["proxy"].(map[string]any)
	if !ok {
		return mode, nil
	}
	if m, ok := raw["mode"].(string); ok {
		switch m {
		case models.ProxyModeAuto, models.ProxyModeManual:
			mode = m
		}
	}
	if list, ok := raw["ids"].([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok {
				ids = append(ids, s)
			}
		}
	}
	return mode, ids
}

// proxyTickInterval converts the configured rotation period.
func proxyTickInterval(cfg models.RouterConfiguration) time.Duration {
	if cfg.UpdateIntervalMinutes < 1 {
		return time.Duration(models.DefaultUpdateIntervalMinutes) * time.Minute
	}
	return time.Duration(cfg.UpdateIntervalMinutes) * time.Minute
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
