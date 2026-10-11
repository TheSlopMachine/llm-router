package dashboard

import (
	"log/slog"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/adapters/generic"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/services/admin"
	"github.com/TheSlopMachine/llm-router/internal/services/batches"
	configservice "github.com/TheSlopMachine/llm-router/internal/services/config"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
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
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// buildWiredParams mirrors internal/server wiring: the live bbolt stack
// plus the SQLite-backed GORM set. Every service constructs without I/O
// beyond its database handle.
func buildWiredParams(test *testing.T) Params {
	test.Helper()

	database := testutil.SetupTestDB(test)
	sqliteDatabase := testutil.SetupTestSQLite(test)
	logger := slog.Default()

	luaService, err := luaplugin.NewWithSQLite(database, sqliteDatabase, logger)
	if err != nil {
		test.Fatalf("init plugin service: %v", err)
	}
	providerService := provider.NewService(database)
	providerService.SetLogger(logger)
	providerService.SetLuaService(luaService)
	genericAdapter := &generic.Adapter{}
	genericAdapter.SetLogger(logger)
	providerService.RegisterGoAdapter(genericAdapter)
	if err := providerService.EnsureSeeded(); err != nil {
		test.Fatalf("seed providers: %v", err)
	}
	adminService := admin.New(database, providerService)
	tokenService := token.New(database)
	credentialService := credential.New(database, providerService)
	modelInfoService := modelinfo.New(database, providerService, credentialService, 0)
	virtualService := virtual.New(database, providerService, modelInfoService)
	configService := configservice.NewWithSQLite(database, sqliteDatabase)
	repositoryService := pluginrepo.NewWithSQLite(database, sqliteDatabase)
	proxyService, err := proxypool.New(database)
	if err != nil {
		test.Fatalf("init proxy service: %v", err)
	}
	videoJobsService := videojobs.New(database)
	videoJobsService.WithSQLite(sqliteDatabase)
	routerService := router.New(providerService, credentialService, modelInfoService, videoJobsService, responses.New(database), batches.New(database), logger)
	maintenanceService := maintenance.New(luaService, providerService, database, logger)
	metricsService := metrics.New(database, logger)

	return Params{
		AdminSvc: adminService, ProviderSvc: providerService, CredSvc: credentialService,
		TokenSvc: tokenService, ModelInfoSvc: modelInfoService, MetricsSvc: metricsService,
		VirtualSvc: virtualService, RouterSvc: routerService, ConfigSvc: configService,
		LuaSvc: luaService, RepoSvc: repositoryService, ProxySvc: proxyService, MaintSvc: maintenanceService,
		Logger:                logger,
		SQLiteDatabase:        sqliteDatabase,
		GormAdminService:      admin.NewGorm(sqliteDatabase),
		GormProviderService:   provider.NewGorm(sqliteDatabase),
		GormCredentialService: credential.NewGorm(sqliteDatabase, providerService),
		GormTokenService:      token.NewGorm(sqliteDatabase),
		GormModelInfoService:  modelinfo.NewGorm(sqliteDatabase),
		GormVirtualService:    virtual.NewGorm(sqliteDatabase),
	}
}

// TestNewWiresGormServices proves the full server-equivalent stack builds:
// bbolt handlers plus the SQLite-backed data and doctor services for the
// Wave 10 cutover.
func TestNewWiresGormServices(test *testing.T) {
	handler, err := New(buildWiredParams(test))
	if err != nil {
		test.Fatalf("New with wired params: %v", err)
	}
	if handler.dataService == nil || handler.doctorService == nil {
		test.Fatal("bbolt data and doctor services must be wired")
	}
	if handler.dataGormService == nil || handler.doctorGormService == nil {
		test.Fatal("GORM data and doctor services must be wired")
	}
	if handler.gormAdminService == nil || handler.gormProviderService == nil ||
		handler.gormCredentialService == nil || handler.gormTokenService == nil ||
		handler.gormModelInfoService == nil || handler.gormVirtualService == nil {
		test.Fatal("every GORM service must be stored")
	}
}

// TestNewRejectsMissingGormWiring proves New fails closed: each missing
// SQLite input is a construction error, never a later nil dereference.
func TestNewRejectsMissingGormWiring(test *testing.T) {
	cases := map[string]func(parameters *Params){
		"nil database":   func(parameters *Params) { parameters.SQLiteDatabase = nil },
		"empty handle":   func(parameters *Params) { parameters.SQLiteDatabase = &db.Database{} },
		"nil admin":      func(parameters *Params) { parameters.GormAdminService = nil },
		"nil provider":   func(parameters *Params) { parameters.GormProviderService = nil },
		"nil credential": func(parameters *Params) { parameters.GormCredentialService = nil },
		"nil token":      func(parameters *Params) { parameters.GormTokenService = nil },
		"nil model info": func(parameters *Params) { parameters.GormModelInfoService = nil },
		"nil virtual":    func(parameters *Params) { parameters.GormVirtualService = nil },
	}
	for name, mutate := range cases {
		test.Run(name, func(test *testing.T) {
			parameters := buildWiredParams(test)
			mutate(&parameters)
			if _, err := New(parameters); err == nil {
				test.Fatalf("New with %s succeeded, want wiring error", name)
			}
		})
	}
}
