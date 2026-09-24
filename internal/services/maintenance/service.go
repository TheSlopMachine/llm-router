// Package maintenance implements the Provider Maintenance & Rotation Service.
//
// Responsibilities:
//   - Periodically scanning all credentials for those that need refresh
//   - Delegating refresh to the appropriate provider backend
//   - Persisting updated credentials via the Credential Pool Service
//
// Design principles:
//   - Flexible: no hardcoded refresh logic — each backend decides its own strategy
//   - Non-blocking: runs in a background goroutine; errors are logged, not fatal
//   - Jitter-free: each check interval is fixed; backends decide when NeedsRefresh is true
package maintenance

import (
	"context"
	"log/slog"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
)

const defaultCheckInterval = 60 * time.Second

// Service runs background maintenance tasks for provider credentials.
type Service struct {
	credSvc     *credential.Service
	providerSvc *provider.Service
	interval    time.Duration
	logger      *slog.Logger
	db          *db.DB

	proxySvc *proxypool.Service
	luaSvc   *luaplugin.Service

	modelInfoSvc modelInfoRefresher

	proxyTickInterval time.Duration
	lastProxyRotate   time.Time
	lastProxyFetch    map[string]time.Time
}

// modelInfoRefresher is the slice of modelinfo the maintenance loop needs.
type modelInfoRefresher interface {
	MergedView(ctx context.Context, providerID string) ([]modelinfo.ModelView, error)
}

// SetModelInfoService wires model metadata refresh for providers with
// config.models_auto_sync enabled.
func (s *Service) SetModelInfoService(mi modelInfoRefresher) {
	s.modelInfoSvc = mi
}

// New constructs a new maintenance Service with the default check interval.
func New(credSvc *credential.Service, providerSvc *provider.Service, db *db.DB, logger *slog.Logger) *Service {
	return &Service{
		credSvc:        credSvc,
		providerSvc:    providerSvc,
		interval:       defaultCheckInterval,
		logger:         logger,
		db:             db,
		lastProxyFetch: map[string]time.Time{},
	}
}

// SetProxyServices wires the proxy pool and plugin service for periodic
// pool rotation and source fetching.
func (s *Service) SetProxyServices(proxySvc *proxypool.Service, luaSvc *luaplugin.Service) {
	s.proxySvc = proxySvc
	s.luaSvc = luaSvc
}

// SetProxyTickInterval sets the automatic proxy rotation period.
func (s *Service) SetProxyTickInterval(d time.Duration) {
	s.proxyTickInterval = d
}

// WithInterval overrides the check interval (useful for testing).
func (s *Service) WithInterval(d time.Duration) *Service {
	s.interval = d
	return s
}

// Start launches the maintenance loop in a background goroutine.
// It stops when ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	s.logger.Info("maintenance service started", "interval", s.interval)
	go func() {
		// Rotate the proxy pool at once instead of waiting for the first
		// tick: a restarted router re-verifies its pool immediately.
		s.rotateProxyOnce(ctx)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				s.logger.Info("maintenance service stopped")
				return
			case <-ticker.C:
				s.runCycle(ctx)
				s.cleanupAuthFlows()
				s.maintainProxyPool(ctx)
			}
		}
	}()
}
