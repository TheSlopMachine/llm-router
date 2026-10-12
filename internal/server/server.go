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
	"github.com/TheSlopMachine/llm-router/internal/services/batches"
	configservice "github.com/TheSlopMachine/llm-router/internal/services/config"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/healthcheck"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/maintenance"
	"github.com/TheSlopMachine/llm-router/internal/services/metrics"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/pluginrepo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
	"github.com/TheSlopMachine/llm-router/internal/services/responses"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
	virtualadapter "github.com/TheSlopMachine/llm-router/providers/virtual"
)

// Server is the fully-wired llm-router application with two listeners.
type Server struct {
	applicationConfig  *config.Config
	sqliteDatabase     *db.Database
	sqlitePath         string
	logger             *slog.Logger
	dashboardServer    *http.Server
	apiServer          *http.Server
	maintenanceService *maintenance.GormService
	metricsService     *metrics.Service
	proxyService       *proxypool.Service
	luaService         *luaplugin.Service
}

// New builds the full Server from config. The SQLite file at DBPath owns
// durable configuration; the file at CachePath owns rebuildable cache
// state. Both start empty on first run; built-in rows seed into the
// database file.
func New(applicationConfig *config.Config, logger *slog.Logger) (*Server, error) {
	sqlitePath := applicationConfig.DBPath
	cachePath := applicationConfig.CachePath
	sqliteDatabase, err := db.OpenSQLite(context.Background(), sqlitePath, cachePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	dashboardAddr := applicationConfig.DashboardAddr
	if dashboardAddr == "" {
		return nil, fmt.Errorf("dashboard listen address is required")
	}
	apiAddr := applicationConfig.APIAddr
	if apiAddr == "" {
		return nil, fmt.Errorf("api listen address is required")
	}

	luaService, err := luaplugin.New(sqliteDatabase, logger)
	if err != nil {
		return nil, fmt.Errorf("init plugin service: %w", err)
	}

	providerService := provider.NewGorm(sqliteDatabase)
	providerService.SetLogger(logger)
	providerService.SetLuaService(luaService)
	genericAdapter := &generic.Adapter{}
	genericAdapter.SetLogger(logger)
	providerService.RegisterGoAdapter(genericAdapter)
	if err := providerService.EnsureSeeded(context.Background()); err != nil {
		return nil, fmt.Errorf("seed providers: %w", err)
	}

	adminService := admin.NewGorm(sqliteDatabase)
	tokenService := token.NewGorm(sqliteDatabase)
	credentialService := credential.NewGorm(sqliteDatabase, providerService)
	credentialService.SetLogger(logger)
	modelInfoService := modelinfo.NewGorm(sqliteDatabase, providerService, credentialService, 1*time.Hour)
	virtualService := virtual.NewGorm(sqliteDatabase, providerService, modelInfoService)
	configService := configservice.New(sqliteDatabase)
	repositoryService := pluginrepo.New(sqliteDatabase)
	if err := repositoryService.EnsureBuiltinRepos(); err != nil {
		return nil, fmt.Errorf("seed built-in plugin repos: %w", err)
	}
	videoJobsService := videojobs.New(sqliteDatabase)

	// Proxy subsystem first: the plugin runtime and dashboard both read
	// from it. Provider request routing never consults it: plugins query
	// read-only and assign proxy_url per request.
	proxyService, err := proxypool.New(sqliteDatabase)
	if err != nil {
		return nil, fmt.Errorf("init proxy cache: %w", err)
	}
	proxyService.SetLogger(logger)
	proxyService.SetSourceHandlers(luaService.ProxySourceKeys, luaService.FetchProxies)

	routerService := router.New(providerService, credentialService, modelInfoService, videoJobsService, responses.NewGormService(sqliteDatabase.Cache), batches.NewSQLiteService(sqliteDatabase), logger)
	virtualAdapter := virtualadapter.New(routerService, virtualService, logger)
	providerService.RegisterGoAdapter(virtualAdapter)
	healthService := healthcheck.NewGormService(sqliteDatabase, luaService, credentialService, providerService)
	healthService.SetLogger(logger)
	luaService.SetHealthTrigger(healthService)
	luaService.SetMarkDead(proxyService.MarkDead)
	luaService.SetProxyQuery(proxyService.Query)
	luaService.SetProxyRequire(proxyService.Require)
	luaService.SetCredentialAccess(
		func(ctx context.Context, providerID string) ([]*models.Credential, error) {
			return credentialService.ListUsable(ctx, providerID)
		},
		func(ctx context.Context, id string) (*models.Credential, error) {
			return credentialService.Get(ctx, id)
		},
		func(ctx context.Context, id string, data map[string]any) error {
			return credentialService.Update(ctx, id, data, nil)
		},
	)
	luaService.SetCredentialLifecycle(
		credentialService.DisableByPlugin,
		credentialService.EnableByPlugin,
		func(providerID string) bool {
			instance, err := providerService.Get(context.Background(), providerID)
			if err != nil || instance == nil {
				return false
			}
			return models.CredentialAutomationOn(instance.Config)
		},
	)
	luaService.SetCredentialParks(credentialService.Park, credentialService.Unpark, credentialService.Parked)
	luaService.SetOnChanged(func(_ string) {
		_ = proxyService.RequestIngest()
	})

	maintenanceService := maintenance.NewGormService(luaService, providerService, logger)
	maintenanceService.SetModelInfoService(modelInfoService)
	maintenanceService.SetVirtualSyncer(virtualService)
	metricsService := metrics.New(sqliteDatabase, logger)
	metricsService.Start()

	modelInfoService.SetLogger(logger)
	virtualService.SetLogger(logger)

	// Model caches persist in SQLite; warm the missing ones in the background
	// so first clicks never wait on upstream discovery.
	go modelInfoService.WarmMissing(context.Background())

	startupCleanup(logger, credentialService, providerService)

	if applicationConfig.NoAuth {
		bootstrapped, err := adminService.IsBootstrapped(context.Background())
		if err != nil || !bootstrapped {
			logger.Warn("no-auth mode with no admin account: dropping NO_AUTH later will require bootstrap")
		}
	}

	wireInvalidation(logger, providerService, modelInfoService, luaService)

	dashboardMux := http.NewServeMux()
	dashboardHandler, err := dashboard.New(dashboard.Params{
		AdminSvc: adminService, ProviderSvc: providerService, CredSvc: credentialService,
		TokenSvc: tokenService, ModelInfoSvc: modelInfoService, MetricsSvc: metricsService,
		VirtualSvc: virtualService, RouterSvc: routerService, ConfigSvc: configService,
		LuaSvc: luaService, RepoSvc: repositoryService, ProxySvc: proxyService, MaintSvc: maintenanceService,
		Logger:         logger,
		NoAuth:         applicationConfig.NoAuth,
		SQLiteDatabase: sqliteDatabase,
	})
	if err != nil {
		return nil, fmt.Errorf("build dashboard handler: %w", err)
	}
	if applicationConfig.DevUIRedirect != "" {
		dashboardHandler.SetDevRedirect(applicationConfig.DevUIRedirect)
	}
	dashboardHandler.Register(dashboardMux)
	dashHandler := bootstrapMiddleware(adminService, applicationConfig.NoAuth)(requestLogger(logger, dashboardMux))

	apiMux := http.NewServeMux()
	apiV1 := v1.New(v1.Params{
		Tokens: tokenService, RouterSvc: routerService, MetricsSvc: metricsService,
		ProviderSvc: providerService, ModelInfoSvc: modelInfoService,
		VirtualSvc: virtualService, Logger: logger, NoAuth: applicationConfig.NoAuth,
	})
	apiV1.Register(apiMux)

	apiHandler := requestLogger(logger, apiMux)

	dashboardServer := &http.Server{
		Addr:    dashboardAddr,
		Handler: dashHandler,
	}
	apiServer := &http.Server{
		Addr:    apiAddr,
		Handler: apiHandler,
	}

	return &Server{
		applicationConfig:  applicationConfig,
		sqliteDatabase:     sqliteDatabase,
		sqlitePath:         sqlitePath,
		logger:             logger,
		dashboardServer:    dashboardServer,
		apiServer:          apiServer,
		maintenanceService: maintenanceService,
		metricsService:     metricsService,
		proxyService:       proxyService,
		luaService:         luaService,
	}, nil
}

// startupRefreshTimeout bounds the synchronous startup jobs before the
// server listens: a pathological slow upstream must delay startup, never
// block it. The ticker retries whatever was skipped.
const startupRefreshTimeout = 30 * time.Second

// shutdownLifecycleTimeout bounds plugin deinitialize hooks on exit:
// shutdown proceeds when the gate expires, regardless of hook outcomes.
const shutdownLifecycleTimeout = 30 * time.Second

// runShutdownHooks runs deinitialize with reason shutdown for every plugin
// type that declares it. Failures log; shutdown always proceeds.
func (s *Server) runShutdownHooks() {
	if s.luaService == nil {
		return
	}
	gate, cancel := context.WithTimeout(context.Background(), shutdownLifecycleTimeout)
	defer cancel()
	if err := s.luaService.RunShutdownDeinitializers(gate); err != nil && s.logger != nil {
		s.logger.Warn("plugin shutdown hooks failed", "err", err)
	}
}

// Run starts the maintenance loop and blocks on both HTTP servers.
func (s *Server) Run(ctx context.Context) error {
	// Run startup plugin jobs before serving traffic so a restarted
	// router refreshes-around stale state while the ticker runs.
	rgate, cancel := context.WithTimeout(ctx, startupRefreshTimeout)
	s.maintenanceService.RunStartupRefresh(rgate)
	// Run plugin initializers in the same gate: a restarted router
	// one-time-seeds shared state while the ticker runs. Slow hooks cancel
	// with the gate; startup never blocks on them.
	if err := s.luaService.RunStartupInitializers(rgate); err != nil && s.logger != nil {
		s.logger.Warn("plugin startup initializers failed", "err", err)
	}
	cancel()
	s.proxyService.Start(ctx)
	s.maintenanceService.Start(ctx)
	s.logger.Info("llm-router started", "dashboard", s.applicationConfig.DashboardAddr, "api", s.applicationConfig.APIAddr, "db", s.sqlitePath, "cache", s.applicationConfig.CachePath)

	errorChannel := make(chan error, 2)
	go func() {
		if err := s.dashboardServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errorChannel <- fmt.Errorf("dashboard server: %w", err)
		}
	}()
	go func() {
		if err := s.apiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errorChannel <- fmt.Errorf("api server: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("shutting down gracefully...")
		s.runShutdownHooks()
		s.metricsService.Stop()
		_ = s.dashboardServer.Shutdown(context.Background())
		_ = s.apiServer.Shutdown(context.Background())
		return nil
	case err := <-errorChannel:
		s.runShutdownHooks()
		s.metricsService.Stop()
		_ = s.dashboardServer.Shutdown(context.Background())
		_ = s.apiServer.Shutdown(context.Background())
		return fmt.Errorf("http server: %w", err)
	}
}

// Close releases the SQLite handle, checkpointing its write-ahead log.
// Call after Run returns.
func (s *Server) Close() error {
	return s.sqliteDatabase.Close()
}
