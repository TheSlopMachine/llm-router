// Package server wires all services together and starts the HTTP servers.
// Dashboard and /v1 API run on separate ports with independent muxes.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
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
	database           *db.DB
	sqliteDatabase     *db.Database
	sqlitePath         string
	logger             *slog.Logger
	dashboardServer    *http.Server
	apiServer          *http.Server
	maintenanceService *maintenance.Service
	metricsService     *metrics.Service
	proxyService       *proxypool.Service
}

// resolveDatabasePaths splits the configured database path into the bbolt
// file and the SQLite file. An explicit SQLite override wins outright. A
// database path ending in .sqlite3 names SQLite directly with the legacy
// bbolt sibling beside it, so the DEV_DB switch preserves existing dev
// state; any other path keeps naming bbolt with the SQLite sibling beside
// it. Both files stay open until the Wave 10 cutover retires bbolt.
func resolveDatabasePaths(databasePath, sqlitePath string) (boltPath, resolvedSQLitePath string) {
	if trimmed := strings.TrimSpace(sqlitePath); trimmed != "" {
		return databasePath, trimmed
	}
	if strings.HasSuffix(databasePath, db.SQLiteFileSuffix) {
		return strings.TrimSuffix(databasePath, db.SQLiteFileSuffix) + ".db", databasePath
	}
	return databasePath, strings.TrimSuffix(databasePath, filepath.Ext(databasePath)) + db.SQLiteFileSuffix
}

// New builds the full Server from config.
func New(applicationConfig *config.Config, logger *slog.Logger) (*Server, error) {
	boltPath, sqlitePath := resolveDatabasePaths(applicationConfig.DBPath, applicationConfig.SQLitePath)
	database, err := db.Open(boltPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqliteDatabase, err := db.OpenSQLite(context.Background(), sqlitePath)
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

	luaService, err := luaplugin.NewWithSQLite(database, sqliteDatabase, logger)
	if err != nil {
		return nil, fmt.Errorf("init plugin service: %w", err)
	}

	providerService := provider.NewService(database)
	providerService.SetLogger(logger)
	providerService.SetLuaService(luaService)
	genericAdapter := &generic.Adapter{}
	genericAdapter.SetLogger(logger)
	providerService.RegisterGoAdapter(genericAdapter)
	if err := providerService.EnsureSeeded(); err != nil {
		return nil, fmt.Errorf("seed providers: %w", err)
	}

	adminService := admin.New(database, providerService)
	tokenService := token.New(database)
	credentialService := credential.New(database, providerService)
	credentialService.SetLogger(logger)
	modelInfoService := modelinfo.New(database, providerService, credentialService, 1*time.Hour)
	virtualService := virtual.New(database, providerService, modelInfoService)
	configService := configservice.NewWithSQLite(database, sqliteDatabase)
	repositoryService := pluginrepo.NewWithSQLite(database, sqliteDatabase)
	if err := repositoryService.PruneLegacyRepos(); err != nil {
		return nil, fmt.Errorf("prune legacy plugin repos: %w", err)
	}
	if err := repositoryService.EnsureBuiltinRepos(); err != nil {
		return nil, fmt.Errorf("seed built-in plugin repos: %w", err)
	}
	videoJobsService := videojobs.New(database)
	videoJobsService.WithSQLite(sqliteDatabase)

	// Proxy subsystem first: the plugin runtime and dashboard both read
	// from it. Provider request routing never consults it: plugins query
	// read-only and assign proxy_url per request.
	proxyService, err := proxypool.New(database)
	if err != nil {
		return nil, fmt.Errorf("init proxy cache: %w", err)
	}
	proxyService.SetLogger(logger)
	proxyService.SetSourceHandlers(luaService.ProxySourceKeys, luaService.FetchProxies)

	routerService := router.New(providerService, credentialService, modelInfoService, videoJobsService, responses.New(database), batches.New(database), logger)
	virtualAdapter := virtualadapter.New(routerService, virtualService, logger)
	providerService.RegisterGoAdapter(virtualAdapter)
	healthService := healthcheck.New(database, luaService, credentialService, providerService)
	healthService.SetLogger(logger)
	luaService.SetHealthTrigger(healthService)
	luaService.SetMarkDead(proxyService.MarkDead)
	luaService.SetProxyQuery(proxyService.Query)
	luaService.SetProxyRequire(proxyService.Require)
	luaService.SetCredentialAccess(
		func(providerID string) ([]*models.Credential, error) { return credentialService.ListUsable(providerID) },
		func(id string) (*models.Credential, error) { return credentialService.Get(id) },
		func(id string, data map[string]any) error { return credentialService.Update(id, data, nil) },
	)
	luaService.SetCredentialLifecycle(
		credentialService.DisableByPlugin,
		credentialService.EnableByPlugin,
		func(providerID string) bool {
			instance, err := providerService.Get(providerID)
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

	maintenanceService := maintenance.New(luaService, providerService, database, logger)
	maintenanceService.SetModelInfoService(modelInfoService)
	metricsService := metrics.New(database, logger)
	// Attach SQLite before Start: base bucket rows dual-write to bbolt and
	// SQLite while both handles are attached; bbolt stays authoritative.
	metricsService.SetSQLiteDatabase(sqliteDatabase.Handle)
	metricsService.Start()

	modelInfoService.SetLogger(logger)
	virtualService.SetLogger(logger)

	// GORM counterparts of the live bbolt services. Dashboard validates
	// and stores them for the Wave 10 cutover; request paths keep serving
	// bbolt until then. Credential validation resolves backends through
	// the live provider service; doctor keeps the bbolt provider registry
	// for Go backends and the plugin runtime for plugin backends.
	providerGormService := provider.NewGorm(sqliteDatabase)
	credentialGormService := credential.NewGorm(sqliteDatabase, providerService)
	tokenGormService := token.NewGorm(sqliteDatabase)
	adminGormService := admin.NewGorm(sqliteDatabase)
	modelInfoGormService := modelinfo.NewGorm(sqliteDatabase)
	virtualGormService := virtual.NewGorm(sqliteDatabase)

	// Model caches persist in bbolt; warm the missing ones in the background
	// so first clicks never wait on upstream discovery.
	go modelInfoService.WarmMissing(context.Background())

	startupCleanup(logger, credentialService, providerService)
	migrateClearCredentialQuota(logger, database)

	if applicationConfig.NoAuth {
		bootstrapped, err := database.IsBootstrapped()
		if err != nil || !bootstrapped {
			logger.Warn("no-auth mode with no admin account: dropping --no-auth later will require bootstrap")
		}
	}

	wireInvalidation(logger, providerService, modelInfoService, luaService)

	dashboardMux := http.NewServeMux()
	dashboardHandler, err := dashboard.New(dashboard.Params{
		AdminSvc: adminService, ProviderSvc: providerService, CredSvc: credentialService,
		TokenSvc: tokenService, ModelInfoSvc: modelInfoService, MetricsSvc: metricsService,
		VirtualSvc: virtualService, RouterSvc: routerService, ConfigSvc: configService,
		LuaSvc: luaService, RepoSvc: repositoryService, ProxySvc: proxyService, MaintSvc: maintenanceService,
		Logger:           logger,
		NoAuth:           applicationConfig.NoAuth,
		SQLiteDatabase:   sqliteDatabase,
		GormAdminService: adminGormService, GormProviderService: providerGormService,
		GormCredentialService: credentialGormService, GormTokenService: tokenGormService,
		GormModelInfoService: modelInfoGormService, GormVirtualService: virtualGormService,
	})
	if err != nil {
		return nil, fmt.Errorf("build dashboard handler: %w", err)
	}
	if applicationConfig.DevUIRedirect != "" {
		dashboardHandler.SetDevRedirect(applicationConfig.DevUIRedirect)
	}
	dashboardHandler.Register(dashboardMux, database)
	dashHandler := bootstrapMiddleware(database, applicationConfig.NoAuth)(requestLogger(logger, dashboardMux))

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
		database:           database,
		sqliteDatabase:     sqliteDatabase,
		sqlitePath:         sqlitePath,
		logger:             logger,
		dashboardServer:    dashboardServer,
		apiServer:          apiServer,
		maintenanceService: maintenanceService,
		metricsService:     metricsService,
		proxyService:       proxyService,
	}, nil
}

// startupRefreshTimeout bounds the synchronous startup jobs before the
// server listens: a pathological slow upstream must delay startup, never
// block it. The ticker retries whatever was skipped.
const startupRefreshTimeout = 30 * time.Second

// Run starts the maintenance loop and blocks on both HTTP servers.
func (s *Server) Run(ctx context.Context) error {
	// Run startup plugin jobs before serving traffic so a restarted
	// router refreshes-around stale state while the ticker runs.
	rgate, cancel := context.WithTimeout(ctx, startupRefreshTimeout)
	s.maintenanceService.RunStartupRefresh(rgate)
	cancel()
	s.proxyService.Start(ctx)
	s.maintenanceService.Start(ctx)
	s.logger.Info("llm-router started", "dashboard", s.applicationConfig.DashboardAddr, "api", s.applicationConfig.APIAddr, "db", s.applicationConfig.DBPath, "sqlite", s.sqlitePath)

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
		s.metricsService.Stop()
		_ = s.dashboardServer.Shutdown(context.Background())
		_ = s.apiServer.Shutdown(context.Background())
		return nil
	case err := <-errorChannel:
		s.metricsService.Stop()
		_ = s.dashboardServer.Shutdown(context.Background())
		_ = s.apiServer.Shutdown(context.Background())
		return fmt.Errorf("http server: %w", err)
	}
}

// Close releases both database handles. SQLite checkpoints its
// write-ahead log on close. Call after Run returns.
func (s *Server) Close() error {
	if err := s.database.Close(); err != nil {
		_ = s.sqliteDatabase.Close()
		return err
	}
	return s.sqliteDatabase.Close()
}
