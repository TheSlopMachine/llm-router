// Package maintenance runs plugin background jobs and provider upkeep.
//
// Responsibilities:
//   - Scheduling colocated plugin jobs per provider instance
//   - Syncing opted-in model lists on the same tick
//   - Cleaning expired auth flows
//
// Design principles:
//   - Plugin-owned: each backend decides its own jobs and schedules; the
//     core only triggers, bounds concurrency and records outcomes
//   - Non-blocking: runs in a background goroutine; errors are logged,
//     never fatal
package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

const defaultCheckInterval = 60 * time.Second

// maxJobWorkers bounds concurrent job runs: jobs are independent per
// provider, but an unbounded fan-out would storm one upstream when many
// schedules align.
const maxJobWorkers = 4

// Service runs background jobs for provider plugins.
type Service struct {
	luaSvc      *luaplugin.Service
	providerSvc *provider.Service
	interval    time.Duration
	logger      *slog.Logger
	db          *db.DB

	modelInfoSvc modelInfoRefresher

	mu      sync.Mutex
	lastRun map[string]time.Time
	running map[string]bool
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
func New(luaSvc *luaplugin.Service, providerSvc *provider.Service, database *db.DB, logger *slog.Logger) *Service {
	return &Service{
		luaSvc:      luaSvc,
		providerSvc: providerSvc,
		interval:    defaultCheckInterval,
		logger:      logger,
		db:          database,
		lastRun:     map[string]time.Time{},
		running:     map[string]bool{},
	}
}

// WithInterval overrides the check interval (useful for testing).
func (s *Service) WithInterval(d time.Duration) *Service {
	s.interval = d
	return s
}

// RunStartupRefresh runs startup jobs and cleans expired auth flows
// synchronously. The server calls it before listening so a restarted router
// never serves traffic with stale plugin state. It never fails startup:
// every outcome logs inside the pass, and cancellation only stops
// launching new workers.
func (s *Service) RunStartupRefresh(ctx context.Context) {
	s.runStartupJobs(ctx)
	s.cleanupAuthFlows()
}

// Start launches the maintenance loop in a background goroutine.
// It stops when ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	s.logger.Info("maintenance service started", "interval", s.interval)
	go func() {
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
			}
		}
	}()
}

// RunJob triggers one job for one provider on demand (dashboard).
// Unknown jobs fail loudly; running jobs report busy.
func (s *Service) RunJob(ctx context.Context, providerID, jobName string) error {
	inst, err := s.providerSvc.Get(providerID)
	if err != nil {
		return err
	}
	jobs := s.luaSvc.Jobs(inst.TypeKey)
	if _, ok := jobs[jobName]; !ok {
		return fmt.Errorf("job %q not found for provider %q", jobName, providerID)
	}
	return s.launch(ctx, inst, jobName, "manual")
}
