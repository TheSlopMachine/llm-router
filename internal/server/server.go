// Package server wires all services together and starts the HTTP servers.
// The public listener serves dashboard SPA, dashboard JSON API (/api) and
// /v1 API with bootstrapping and authentication enforced. The optional
// admin listener serves the same JSON API without either and binds
// loopback only. Route tables live in httpapi and mount on both.
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
	"github.com/TheSlopMachine/llm-router/internal/httpapi"
	"github.com/TheSlopMachine/llm-router/internal/logging"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/requestsource"
	"github.com/TheSlopMachine/llm-router/internal/services/admin"
	"github.com/TheSlopMachine/llm-router/internal/services/batches"
	configservice "github.com/TheSlopMachine/llm-router/internal/services/config"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/events"
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

// Server is the fully-wired llm-router application: one public listener
// plus an optional loopback-only admin listener answering the same JSON
// API without authentication.
type Server struct {
	applicationConfig  *config.Config
	sqliteDatabase     *db.Database
	sqlitePath         string
	logger             *slog.Logger
	httpServer         *http.Server
	adminServer        *http.Server
	shutdownRequested  chan struct{}
	maintenanceService *maintenance.GormService
	metricsService     *metrics.Service
	proxyService       *proxypool.Service
	luaService         *luaplugin.Service
	eventsService      *events.Service
}

// New builds the full Server from config. The SQLite file at DBPath owns
// durable configuration; the file at CachePath owns rebuildable cache
// state; the file at EventsPath owns operator telemetry. All start empty
// on first run; built-in rows seed into the database file.
func New(applicationConfig *config.Config, logger *slog.Logger) (*Server, error) {
	sqlitePath := applicationConfig.DBPath
	cachePath := applicationConfig.CachePath
	eventsPath := applicationConfig.EventsPath
	sqliteDatabase, err := db.OpenSQLite(context.Background(), sqlitePath, cachePath, eventsPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	listenAddr := applicationConfig.Addr
	if listenAddr == "" {
		return nil, fmt.Errorf("listen address is required")
	}

	luaService, err := luaplugin.New(sqliteDatabase, logger)
	if err != nil {
		return nil, fmt.Errorf("init plugin service: %w", err)
	}
	luaService.SetMinimumLevel(applicationConfig.LogLevel.SlogLevel())

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
	adminService.SetLogger(logger)
	tokenService := token.NewGorm(sqliteDatabase)
	tokenService.SetLogger(logger)
	credentialService := credential.NewGorm(sqliteDatabase, providerService)
	credentialService.SetLogger(logger)
	modelInfoService := modelinfo.NewGorm(sqliteDatabase, providerService, credentialService, 1*time.Hour)
	virtualService := virtual.NewGorm(sqliteDatabase, providerService, modelInfoService)
	configService := configservice.New(sqliteDatabase)
	configService.SetLogger(logger)
	repositoryService := pluginrepo.New(sqliteDatabase)
	repositoryService.SetLogger(logger)
	if err := repositoryService.EnsureBuiltinRepos(); err != nil {
		return nil, fmt.Errorf("seed built-in plugin repos: %w", err)
	}
	videoJobsService := videojobs.New(sqliteDatabase)
	videoJobsService.SetLogger(logger)

	// Proxy subsystem first: the plugin runtime and dashboard both read
	// from it. Provider request routing never consults it: plugins query
	// read-only and assign proxy_url per request.
	proxyService, err := proxypool.New(sqliteDatabase)
	if err != nil {
		return nil, fmt.Errorf("init proxy cache: %w", err)
	}
	proxyService.SetLogger(logger)
	proxyService.SetSourceHandlers(luaService.ProxySourceKeys, luaService.FetchProxies)

	responsesService := responses.NewGormService(sqliteDatabase.Cache)
	responsesService.SetLogger(logger)
	batchesService := batches.NewSQLiteService(sqliteDatabase)
	batchesService.SetLogger(logger)
	routerService := router.New(providerService, credentialService, modelInfoService, videoJobsService, responsesService, batchesService, logger)
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

	eventsService, err := events.New(sqliteDatabase, applicationConfig.EventsRetentionDays)
	if err != nil {
		_ = sqliteDatabase.Close()
		return nil, fmt.Errorf("init events service: %w", err)
	}
	eventsService.SetLogger(logger)
	if handler, ok := logger.Handler().(*logging.EventsHandler); ok {
		handler.SetService(eventsService)
	}
	if err := eventsService.Prune(context.Background()); err != nil && logger != nil {
		logger.Warn("Events prune fails", "error", err)
	} else if logger != nil {
		logger.Debug("Events prune completes")
	}

	modelInfoService.SetLogger(logger)
	virtualService.SetLogger(logger)

	// Model caches persist in SQLite; warm the missing ones in the background
	// so first clicks never wait on upstream discovery.
	logger.Debug("Model cache warmup starts")
	go modelInfoService.WarmMissing(context.Background())

	startupCleanup(logger, credentialService, providerService)

	wireInvalidation(logger, providerService, modelInfoService, luaService)

	// shutdownRequested fires when the admin shutdown endpoint runs. The
	// buffered non-blocking send lets the endpoint answer 200 before the
	// Run loop reacts.
	shutdownRequested := make(chan struct{}, 1)
	requestShutdown := func() {
		select {
		case shutdownRequested <- struct{}{}:
		default:
		}
	}

	dashboardHandler, err := dashboard.New(dashboard.Params{
		AdminSvc: adminService, ProviderSvc: providerService, CredSvc: credentialService,
		TokenSvc: tokenService, ModelInfoSvc: modelInfoService, MetricsSvc: metricsService,
		VirtualSvc: virtualService, RouterSvc: routerService, ConfigSvc: configService,
		LuaSvc: luaService, RepoSvc: repositoryService, ProxySvc: proxyService, MaintSvc: maintenanceService,
		EventsSvc:       eventsService,
		Logger:          logger,
		RequestShutdown: requestShutdown,
		SQLiteDatabase:  sqliteDatabase,
	})
	if err != nil {
		return nil, fmt.Errorf("build dashboard handler: %w", err)
	}
	if applicationConfig.DevUIRedirect != "" {
		dashboardHandler.SetDevRedirect(applicationConfig.DevUIRedirect)
	}

	apiV1 := v1.New(v1.Params{
		Tokens: tokenService, RouterSvc: routerService, MetricsSvc: metricsService,
		ProviderSvc: providerService, ModelInfoSvc: modelInfoService,
		VirtualSvc: virtualService, Logger: logger,
	})

	// Both listeners mount the same route tables; the source in the
	// request context selects authenticated vs bypass behavior.
	publicMux := httpapi.BuildPublicMux(apiV1, dashboardHandler)
	if err := verifySPAMux(publicMux, applicationConfig.DevUIRedirect); err != nil {
		_ = sqliteDatabase.Close()
		return nil, err
	}
	privateMux := httpapi.BuildPrivateMux(apiV1, dashboardHandler)
	publicHandler := requestsource.Middleware(requestsource.RequestSourcePublic)(
		bootstrapMiddleware(adminService, logger)(requestLogger(logger, publicMux)))
	privateHandler := requestsource.Middleware(requestsource.RequestSourcePrivate)(
		bootstrapMiddleware(adminService, logger)(requestLogger(logger, privateMux)))

	httpServer := &http.Server{
		Addr:    listenAddr,
		Handler: publicHandler,
	}
	var adminServer *http.Server
	if applicationConfig.AdminAddr != "" {
		adminServer = &http.Server{
			Addr:    applicationConfig.AdminAddr,
			Handler: privateHandler,
		}
	}

	return &Server{
		applicationConfig:  applicationConfig,
		sqliteDatabase:     sqliteDatabase,
		sqlitePath:         sqlitePath,
		logger:             logger,
		httpServer:         httpServer,
		adminServer:        adminServer,
		shutdownRequested:  shutdownRequested,
		maintenanceService: maintenanceService,
		metricsService:     metricsService,
		proxyService:       proxyService,
		luaService:         luaService,
		eventsService:      eventsService,
	}, nil
}

// spaProbeRecorder captures one synthetic SPA response without the
// httptest dependency.
type spaProbeRecorder struct {
	header http.Header
	body   []byte
	status int
}

func (r *spaProbeRecorder) Header() http.Header { return r.header }

func (r *spaProbeRecorder) Write(body []byte) (int, error) {
	r.body = append(r.body, body...)
	return len(body), nil
}

func (r *spaProbeRecorder) WriteHeader(status int) { r.status = status }

// verifySPAMux fails startup when the embedded SPA serves an empty page.
// RegisterSPA discards embed-FS errors internally; probing "/" turns a
// silent empty page into a loud startup error. Dev redirect mode skips
// the check: Vite serves the SPA there.
func verifySPAMux(mux *http.ServeMux, devRedirect string) error {
	if devRedirect != "" {
		return nil
	}
	request, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil {
		return fmt.Errorf("build SPA probe request: %w", err)
	}
	recorder := &spaProbeRecorder{header: make(http.Header), status: http.StatusOK}
	mux.ServeHTTP(recorder, request)
	status := recorder.status
	if status == 0 {
		status = http.StatusOK
	}
	if status != http.StatusOK {
		return fmt.Errorf("serve embedded SPA: unexpected status %d", status)
	}
	if len(recorder.body) == 0 {
		return fmt.Errorf("serve embedded SPA: empty body")
	}
	return nil
}

// startupRefreshTimeout bounds the synchronous startup jobs before the
// server listens: a pathological slow upstream must delay startup, never
// block it. The ticker retries whatever was skipped.
const startupRefreshTimeout = 30 * time.Second

// shutdownLifecycleTimeout bounds plugin deinitialize hooks on exit:
// shutdown proceeds when the gate expires, regardless of hook outcomes.
const shutdownLifecycleTimeout = 30 * time.Second

// eventsDrainTimeout bounds the events queue drain on exit: shutdown
// proceeds when the gate expires, regardless of remaining queue depth.
const eventsDrainTimeout = 5 * time.Second

// runShutdownHooks runs deinitialize with reason shutdown for every plugin
// type that declares it. Failures log; shutdown always proceeds.
func (s *Server) runShutdownHooks() {
	if s.luaService == nil {
		return
	}
	gate, cancel := context.WithTimeout(context.Background(), shutdownLifecycleTimeout)
	defer cancel()
	if err := s.luaService.RunShutdownDeinitializers(gate); err != nil && s.logger != nil {
		s.logger.Warn("Plugin shutdown hooks fail", "error", err)
	}
}

// Run starts the maintenance loop and blocks on the HTTP servers.
func (s *Server) Run(ctx context.Context) error {
	// Run startup plugin jobs before serving traffic so a restarted
	// router refreshes-around stale state while the ticker runs.
	rgate, cancel := context.WithTimeout(ctx, startupRefreshTimeout)
	s.maintenanceService.RunStartupRefresh(rgate)
	// Run plugin initializers in the same gate: a restarted router
	// one-time-seeds shared state while the ticker runs. Slow hooks cancel
	// with the gate; startup never blocks on them.
	if err := s.luaService.RunStartupInitializers(rgate); err != nil && s.logger != nil {
		s.logger.Warn("Plugin startup initializers fail", "error", err)
	} else if s.logger != nil {
		s.logger.Debug("Plugin startup initializers complete")
	}
	cancel()
	// servicesCtx bounds every background writer: maintenance jobs, proxy
	// flush loop and events pruning. Cancelling it first in graceful
	// stops all writers before the databases vacuum in Close, including
	// on the admin-shutdown path where the caller's ctx is not done.
	servicesCtx, cancelServices := context.WithCancel(ctx)
	defer cancelServices()
	s.proxyService.Start(servicesCtx)
	s.logger.Debug("Proxy service starts")
	s.maintenanceService.Start(servicesCtx)
	if s.eventsService != nil {
		s.eventsService.Start(servicesCtx)
		s.logger.Debug("Events service starts")
	}
	startFields := []any{"endpoint", s.applicationConfig.Addr, "database", s.sqlitePath, "cache", s.applicationConfig.CachePath, "events", s.applicationConfig.EventsPath}
	if s.applicationConfig.AdminAddr != "" {
		s.logger.Warn("Admin listener runs without authentication", "endpoint", s.applicationConfig.AdminAddr, "detail", "loopback only")
		startFields = append(startFields, "admin_endpoint", s.applicationConfig.AdminAddr)
	}
	s.logger.Info("Server starts", startFields...)

	errorChannel := make(chan error, 2)
	serve := func(server *http.Server, name string) {
		if server == nil {
			return
		}
		go func() {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errorChannel <- fmt.Errorf("%s server: %w", name, err)
			}
		}()
	}
	serve(s.httpServer, "http")
	serve(s.adminServer, "admin")

	shutdown := func() {
		if err := s.httpServer.Shutdown(context.Background()); err != nil && s.logger != nil {
			s.logger.Warn("Public server shutdown fails", "source", "public", "error", err)
		}
		if s.adminServer != nil {
			if err := s.adminServer.Shutdown(context.Background()); err != nil && s.logger != nil {
				s.logger.Warn("Admin server shutdown fails", "source", "admin", "error", err)
			}
		}
	}
	// graceful runs the single shutdown path: background writers stop,
	// plugin hooks, metrics flush, then both listeners. Shutdown waits for
	// in-flight requests, so the admin shutdown response is always
	// delivered first. The events queue drains after the listeners stop so
	// the final lines persist before Close vacuums.
	graceful := func() {
		cancelServices()
		s.runShutdownHooks()
		s.metricsService.Stop()
		shutdown()
		if handler, ok := s.logger.Handler().(*logging.EventsHandler); ok {
			if !handler.Drain(eventsDrainTimeout) && s.logger != nil {
				s.logger.Warn("Events drain times out", "wait", eventsDrainTimeout)
			}
		}
	}
	select {
	case <-ctx.Done():
		s.logger.Info("Server shuts down gracefully", "source", "signal")
		graceful()
		return nil
	case <-s.shutdownRequested:
		s.logger.Info("Server shuts down gracefully", "source", "admin")
		graceful()
		return nil
	case err := <-errorChannel:
		s.logger.Error("HTTP server fails", "error", err)
		graceful()
		return fmt.Errorf("http server: %w", err)
	}
}

// Close releases the SQLite handles, vacuuming each file after
// checkpointing its write-ahead log. Call after Run returns.
func (s *Server) Close() error {
	return s.sqliteDatabase.Close()
}

// EventsService exposes the events service for logger wiring.
func (s *Server) EventsService() *events.Service {
	if s == nil {
		return nil
	}
	return s.eventsService
}
