// Package server wires all services together and starts the HTTP servers.
// Dashboard and /v1 API run on separate ports with independent muxes.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
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
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
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
	genericAdapter := &generic.Adapter{}
	genericAdapter.SetLogger(logger)
	providerSvc.RegisterGoAdapter(genericAdapter)
	if err := providerSvc.EnsureSeeded(); err != nil {
		return nil, fmt.Errorf("seed providers: %w", err)
	}

	adminSvc := admin.New(database, providerSvc)
	tokenSvc := token.New(database)
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
	routerCfg, err := configSvc.Get()
	if err != nil {
		return nil, fmt.Errorf("load router config: %w", err)
	}
	exhaustedSvc := exhausted.New(database)
	routerSvc := router.New(providerSvc, credSvc, modelInfoSvc, exhaustedSvc, logger)
	virtualAdapter := virtualadapter.New(routerSvc, virtualSvc, logger)
	providerSvc.RegisterGoAdapter(virtualAdapter)
	luaSvc.SetUsageTracker(credSvc)
	luaSvc.SetExhaustedStore(exhaustedSvc)
	genericAdapter.SetUsageTracker(credSvc)

	// Proxy subsystem: pool, plugin proxy resolution, pair outcome reports.
	proxySvc := proxypool.New(database)
	proxySvc.SetConfig(routerCfg.MinDownloadSpeedKbps, routerCfg.MaxProxiesPerLocation)
	wireProxy(luaSvc, proxySvc, exhaustedSvc)

	maintSvc := maintenance.New(credSvc, providerSvc, database, logger)
	maintSvc.SetProxyServices(proxySvc, luaSvc)
	maintSvc.SetProxyTickInterval(proxyTickInterval(routerCfg))
	maintSvc.SetModelInfoService(modelInfoSvc)
	configSvc.SetOnChanged(func(cfg models.RouterConfiguration) {
		proxySvc.SetConfig(cfg.MinDownloadSpeedKbps, cfg.MaxProxiesPerLocation)
		maintSvc.SetProxyTickInterval(proxyTickInterval(cfg))
	})
	metricsSvc := metrics.New(database, logger)
	metricsSvc.Start()

	modelInfoSvc.SetLogger(logger)
	virtualSvc.SetLogger(logger)

	// Model caches persist in bbolt; warm the missing ones in the background
	// so first clicks never wait on upstream discovery.
	go modelInfoSvc.WarmMissing(context.Background())

	startupCleanup(logger, credSvc, providerSvc)
	migrateProxySourceKeys(logger, luaSvc, proxySvc)
	migrateDropProxyLimits(logger, database)
	migrateClearCredentialQuota(logger, database)

	if cfg.NoAuth {
		bootstrapped, err := database.IsBootstrapped()
		if err != nil || !bootstrapped {
			logger.Warn("no-auth mode with no admin account: dropping --no-auth later will require bootstrap")
		}
	}

	wireInvalidation(logger, providerSvc, modelInfoSvc, luaSvc)

	dashMux := http.NewServeMux()
	dash, err := dashboard.New(dashboard.Params{
		AdminSvc: adminSvc, ProviderSvc: providerSvc, CredSvc: credSvc,
		TokenSvc: tokenSvc, ModelInfoSvc: modelInfoSvc, MetricsSvc: metricsSvc,
		VirtualSvc: virtualSvc, RouterSvc: routerSvc, ConfigSvc: configSvc,
		LuaSvc: luaSvc, RepoSvc: repoSvc, ProxySvc: proxySvc, Logger: logger,
		NoAuth: cfg.NoAuth,
	})
	if err != nil {
		return nil, fmt.Errorf("build dashboard handler: %w", err)
	}
	if cfg.DevUIRedirect != "" {
		dash.SetDevRedirect(cfg.DevUIRedirect)
	}
	dash.Register(dashMux, database)
	dashHandler := bootstrapMiddleware(database, cfg.NoAuth)(requestLogger(logger, dashMux))

	apiMux := http.NewServeMux()
	apiV1 := v1.New(v1.Params{
		Tokens: tokenSvc, RouterSvc: routerSvc, MetricsSvc: metricsSvc,
		ProviderSvc: providerSvc, ModelInfoSvc: modelInfoSvc,
		VirtualSvc: virtualSvc, Logger: logger, NoAuth: cfg.NoAuth,
	})
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
