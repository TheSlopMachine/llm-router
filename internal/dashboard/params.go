package dashboard

import (
	"fmt"
	"log/slog"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/services/admin"
	configsvc "github.com/TheSlopMachine/llm-router/internal/services/config"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/datamanagement"
	"github.com/TheSlopMachine/llm-router/internal/services/doctor"
	"github.com/TheSlopMachine/llm-router/internal/services/events"
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
)

// Params carries every dependency of the dashboard Handler in one value,
// so adding a service touches one struct instead of every call site.
type Params struct {
	AdminSvc     *admin.GormService
	ProviderSvc  *provider.GormService
	CredSvc      *credential.GormService
	TokenSvc     *token.GormService
	ModelInfoSvc *modelinfo.GormService
	MetricsSvc   *metrics.Service
	VirtualSvc   *virtual.GormService
	RouterSvc    *router.Service
	ConfigSvc    *configsvc.Service
	LuaSvc       *luaplugin.Service
	RepoSvc      *pluginrepo.Service
	ProxySvc     *proxypool.Service
	MaintSvc     *maintenance.GormService
	EventsSvc    *events.Service
	Logger       *slog.Logger
	// RequestShutdown signals graceful shutdown when the admin shutdown
	// endpoint fires. Required: construction fails when it is missing so
	// integrator miswiring surfaces at startup, never as a silent no-op.
	RequestShutdown func()
	// SQLiteDatabase is the SQLite handle pair backing the GORM services.
	// Required: construction fails when it is missing so integrator
	// miswiring surfaces at startup, never as a nil dereference later.
	SQLiteDatabase *db.Database
}

// EffectiveBootstrapped is the bootstrap state the SPA acts on: the real
// database state, or true on the admin listener where no account exists.
// Single source of truth for the middleware and the status endpoint so
// the gate and the reported state can never diverge.
func EffectiveBootstrapped(bootstrapped, unrestricted bool) bool {
	return bootstrapped || unrestricted
}

// New constructs a dashboard Handler.
func New(parameters Params) (*Handler, error) {
	if parameters.SQLiteDatabase == nil || parameters.SQLiteDatabase.Handle == nil || parameters.SQLiteDatabase.Cache == nil || parameters.SQLiteDatabase.Events == nil {
		return nil, fmt.Errorf("dashboard sqlite database is not configured")
	}
	if parameters.AdminSvc == nil {
		return nil, fmt.Errorf("dashboard admin service is not configured")
	}
	if parameters.ProviderSvc == nil {
		return nil, fmt.Errorf("dashboard provider service is not configured")
	}
	if parameters.RequestShutdown == nil {
		return nil, fmt.Errorf("dashboard shutdown signal is not configured")
	}
	if parameters.CredSvc == nil {
		return nil, fmt.Errorf("dashboard credential service is not configured")
	}
	if parameters.TokenSvc == nil {
		return nil, fmt.Errorf("dashboard token service is not configured")
	}
	if parameters.ModelInfoSvc == nil {
		return nil, fmt.Errorf("dashboard model info service is not configured")
	}
	if parameters.VirtualSvc == nil {
		return nil, fmt.Errorf("dashboard virtual model service is not configured")
	}
	if parameters.EventsSvc == nil {
		return nil, fmt.Errorf("dashboard events service is not configured")
	}
	dataGormService := datamanagement.NewGorm(
		parameters.ProviderSvc,
		parameters.CredSvc,
		parameters.VirtualSvc,
		parameters.TokenSvc,
		parameters.LuaSvc,
		parameters.RepoSvc,
		parameters.ModelInfoSvc,
	)
	doctorGormService := doctor.NewGorm(
		parameters.SQLiteDatabase,
		parameters.ProviderSvc,
		parameters.VirtualSvc,
		parameters.TokenSvc,
		parameters.ProviderSvc,
		parameters.LuaSvc,
	)
	dataGormService.SetLogger(parameters.Logger)
	doctorGormService.SetLogger(parameters.Logger)
	return &Handler{
		gormAdminService:      parameters.AdminSvc,
		gormProviderService:   parameters.ProviderSvc,
		gormCredentialService: parameters.CredSvc,
		gormTokenService:      parameters.TokenSvc,
		gormModelInfoService:  parameters.ModelInfoSvc,
		metricsService:        parameters.MetricsSvc,
		gormVirtualService:    parameters.VirtualSvc,
		routerService:         parameters.RouterSvc,
		configService:         parameters.ConfigSvc,
		luaService:            parameters.LuaSvc,
		repositoryService:     parameters.RepoSvc,
		proxyService:          parameters.ProxySvc,
		maintenanceService:    parameters.MaintSvc,
		eventsService:         parameters.EventsSvc,
		dataGormService:       dataGormService,
		doctorGormService:     doctorGormService,
		logger:                parameters.Logger,
		requestShutdown:       parameters.RequestShutdown,
	}, nil
}
