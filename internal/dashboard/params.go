package dashboard

import (
	"log/slog"

	"github.com/TheSlopMachine/llm-router/internal/services/admin"
	configsvc "github.com/TheSlopMachine/llm-router/internal/services/config"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
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
	Logger       *slog.Logger
}

// New constructs a dashboard Handler.
func New(p Params) (*Handler, error) {
	return &Handler{
		adminSvc:     p.AdminSvc,
		providerSvc:  p.ProviderSvc,
		credSvc:      p.CredSvc,
		tokenSvc:     p.TokenSvc,
		modelInfoSvc: p.ModelInfoSvc,
		metricsSvc:   p.MetricsSvc,
		virtualSvc:   p.VirtualSvc,
		routerSvc:    p.RouterSvc,
		configSvc:    p.ConfigSvc,
		luaSvc:       p.LuaSvc,
		repoSvc:      p.RepoSvc,
		proxySvc:     p.ProxySvc,
		logger:       p.Logger,
	}, nil
}
