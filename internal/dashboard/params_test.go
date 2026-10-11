package dashboard

import (
	"context"
	"log/slog"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/adapters/generic"
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

// buildWiredParams mirrors internal/server wiring: the SQLite-backed GORM
// stack. Every service constructs without I/O beyond its database handle.
func buildWiredParams(test *testing.T) Params {
	test.Helper()

	sqliteDatabase := testutil.SetupTestSQLite(test)
	logger := slog.Default()
	ctx := context.Background()

	luaService, err := luaplugin.New(sqliteDatabase, logger)
	if err != nil {
		test.Fatalf("init plugin service: %v", err)
	}
	providerService := provider.NewGorm(sqliteDatabase)
	providerService.SetLogger(logger)
	providerService.SetLuaService(luaService)
	genericAdapter := &generic.Adapter{}
	genericAdapter.SetLogger(logger)
	providerService.RegisterGoAdapter(genericAdapter)
	if err := providerService.EnsureSeeded(ctx); err != nil {
		test.Fatalf("seed providers: %v", err)
	}
	adminService := admin.NewGorm(sqliteDatabase)
	tokenService := token.NewGorm(sqliteDatabase)
	credentialService := credential.NewGorm(sqliteDatabase, providerService)
	modelInfoService := modelinfo.NewGorm(sqliteDatabase, providerService, credentialService, 0)
	virtualService := virtual.NewGorm(sqliteDatabase, providerService, modelInfoService)
	configService := configservice.New(sqliteDatabase)
	repositoryService := pluginrepo.New(sqliteDatabase)
	proxyService, err := proxypool.New(sqliteDatabase)
	if err != nil {
		test.Fatalf("init proxy service: %v", err)
	}
	videoJobsService := videojobs.New(sqliteDatabase)
	routerService := router.New(providerService, credentialService, modelInfoService, videoJobsService, responses.NewGormService(sqliteDatabase.Handle), batches.NewSQLiteService(sqliteDatabase), logger)
	maintenanceService := maintenance.NewGormService(luaService, providerService, logger)
	maintenanceService.SetModelInfoService(modelInfoService)
	metricsService := metrics.New(sqliteDatabase, logger)

	return Params{
		AdminSvc: adminService, ProviderSvc: providerService, CredSvc: credentialService,
		TokenSvc: tokenService, ModelInfoSvc: modelInfoService, MetricsSvc: metricsService,
		VirtualSvc: virtualService, RouterSvc: routerService, ConfigSvc: configService,
		LuaSvc: luaService, RepoSvc: repositoryService, ProxySvc: proxyService, MaintSvc: maintenanceService,
		Logger:         logger,
		SQLiteDatabase: sqliteDatabase,
	}
}

// TestNewWiresGormServices proves the full server-equivalent stack builds:
// every GORM service plus the data and doctor services.
func TestNewWiresGormServices(test *testing.T) {
	handler, err := New(buildWiredParams(test))
	if err != nil {
		test.Fatalf("New with wired params: %v", err)
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

// TestNewRejectsMissingWiring proves New fails closed: each missing
// input is a construction error, never a later nil dereference.
func TestNewRejectsMissingWiring(test *testing.T) {
	cases := map[string]func(parameters *Params){
		"nil database":   func(parameters *Params) { parameters.SQLiteDatabase = nil },
		"nil admin":      func(parameters *Params) { parameters.AdminSvc = nil },
		"nil provider":   func(parameters *Params) { parameters.ProviderSvc = nil },
		"nil credential": func(parameters *Params) { parameters.CredSvc = nil },
		"nil token":      func(parameters *Params) { parameters.TokenSvc = nil },
		"nil model info": func(parameters *Params) { parameters.ModelInfoSvc = nil },
		"nil virtual":    func(parameters *Params) { parameters.VirtualSvc = nil },
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
