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
	AdminSvc     *admin.Service
	ProviderSvc  *provider.Service
	CredSvc      *credential.Service
	TokenSvc     *token.Service
	ModelInfoSvc *modelinfo.Service
	MetricsSvc   *metrics.Service
	VirtualSvc   *virtual.Service
	RouterSvc    *router.Service
	ConfigSvc    *configsvc.Service
	LuaSvc       *luaplugin.Service
	RepoSvc      *pluginrepo.Service
	ProxySvc     *proxypool.Service
	MaintSvc     *maintenance.Service
	Logger       *slog.Logger
	// NoAuth skips session checks: dashboard APIs serve without login.
	NoAuth bool
	// SQLiteDatabase is the SQLite handle backing the GORM services below.
	// Required: construction fails when it is missing so integrator
	// miswiring surfaces at startup, never as a nil dereference later.
	SQLiteDatabase *db.Database
	// GormAdminService through GormVirtualService are the SQLite-backed
	// counterparts of the bbolt services above. Handlers keep serving the
	// bbolt services until the Wave 10 cutover flips them; the GORM set is
	// validated here and feeds the GORM data and doctor services below.
	GormAdminService      *admin.GormService
	GormProviderService   *provider.GormService
	GormCredentialService *credential.GormService
	GormTokenService      *token.GormService
	GormModelInfoService  *modelinfo.GormService
	GormVirtualService    *virtual.GormService
}

// EffectiveBootstrapped is the bootstrap state the SPA acts on: the real
// database state, or true under NoAuth where no account is required.
// Single source of truth for the middleware and the status endpoint so
// the gate and the reported state can never diverge.
func EffectiveBootstrapped(bootstrapped, noAuth bool) bool {
	return bootstrapped || noAuth
}

// New constructs a dashboard Handler.
func New(parameters Params) (*Handler, error) {
	if parameters.SQLiteDatabase == nil || parameters.SQLiteDatabase.Handle == nil {
		return nil, fmt.Errorf("dashboard sqlite database is not configured")
	}
	if parameters.GormAdminService == nil {
		return nil, fmt.Errorf("dashboard admin GORM service is not configured")
	}
	if parameters.GormProviderService == nil {
		return nil, fmt.Errorf("dashboard provider GORM service is not configured")
	}
	if parameters.GormCredentialService == nil {
		return nil, fmt.Errorf("dashboard credential GORM service is not configured")
	}
	if parameters.GormTokenService == nil {
		return nil, fmt.Errorf("dashboard token GORM service is not configured")
	}
	if parameters.GormModelInfoService == nil {
		return nil, fmt.Errorf("dashboard model info GORM service is not configured")
	}
	if parameters.GormVirtualService == nil {
		return nil, fmt.Errorf("dashboard virtual model GORM service is not configured")
	}
	dataService := datamanagement.New(
		parameters.AdminSvc.DB(),
		parameters.ProviderSvc,
		parameters.CredSvc,
		parameters.VirtualSvc,
		parameters.TokenSvc,
		parameters.LuaSvc,
		parameters.RepoSvc,
		parameters.ModelInfoSvc,
	)
	doctorService := doctor.New(
		parameters.AdminSvc.DB(),
		parameters.ProviderSvc,
		parameters.CredSvc,
		parameters.VirtualSvc,
		parameters.TokenSvc,
		parameters.ProxySvc,
		parameters.LuaSvc,
		parameters.ModelInfoSvc,
	)
	dataGormService := datamanagement.NewGorm(
		parameters.GormProviderService,
		parameters.GormCredentialService,
		parameters.GormVirtualService,
		parameters.GormTokenService,
		parameters.LuaSvc,
		parameters.RepoSvc,
		parameters.GormModelInfoService,
	)
	doctorGormService := doctor.NewGorm(
		parameters.SQLiteDatabase,
		parameters.GormProviderService,
		parameters.GormVirtualService,
		parameters.GormTokenService,
		parameters.ProviderSvc,
		parameters.LuaSvc,
	)
	return &Handler{
		adminService:          parameters.AdminSvc,
		providerService:       parameters.ProviderSvc,
		credentialService:     parameters.CredSvc,
		tokenService:          parameters.TokenSvc,
		modelInfoService:      parameters.ModelInfoSvc,
		metricsService:        parameters.MetricsSvc,
		virtualService:        parameters.VirtualSvc,
		routerService:         parameters.RouterSvc,
		configService:         parameters.ConfigSvc,
		luaService:            parameters.LuaSvc,
		repositoryService:     parameters.RepoSvc,
		proxyService:          parameters.ProxySvc,
		maintenanceService:    parameters.MaintSvc,
		dataService:           dataService,
		doctorService:         doctorService,
		gormAdminService:      parameters.GormAdminService,
		gormProviderService:   parameters.GormProviderService,
		gormCredentialService: parameters.GormCredentialService,
		gormTokenService:      parameters.GormTokenService,
		gormModelInfoService:  parameters.GormModelInfoService,
		gormVirtualService:    parameters.GormVirtualService,
		dataGormService:       dataGormService,
		doctorGormService:     doctorGormService,
		logger:                parameters.Logger,
		noAuth:                parameters.NoAuth,
	}, nil
}
