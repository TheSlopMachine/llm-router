package v1

import (
	"log/slog"

	"github.com/TheSlopMachine/llm-router/internal/services/metrics"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
)

// Params carries every dependency of the v1 Handler in one value,
// so adding a service touches one struct instead of every call site.
type Params struct {
	Tokens       *token.Service
	RouterSvc    *router.Service
	MetricsSvc   *metrics.Service
	ProviderSvc  *provider.Service
	ModelInfoSvc *modelinfo.Service
	VirtualSvc   *virtual.Service
	Logger       *slog.Logger
	// NoAuth skips bearer validation: requests route with a nil token.
	NoAuth bool
}

// New constructs a v1 Handler.
func New(parameters Params) *Handler {
	return &Handler{
		tokens:       parameters.Tokens,
		router:       parameters.RouterSvc,
		metrics:      parameters.MetricsSvc,
		providerSvc:  parameters.ProviderSvc,
		modelInfoSvc: parameters.ModelInfoSvc,
		virtualSvc:   parameters.VirtualSvc,
		logger:       parameters.Logger,
		noAuth:       parameters.NoAuth,
	}
}
