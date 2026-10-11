// GORM-backed maintenance scheduling.
//
// GormService runs background jobs for provider plugins against
// context-taking dependencies. It owns no tables: jobs stay in-memory (the
// lastRun/running maps). Auth flow state has no table: expiry is enforced
// by the dashboard readers, and no cleanup pass exists.
package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

const defaultCheckInterval = 60 * time.Second

// maxJobWorkers bounds concurrent job runs: jobs are independent per
// provider, but an unbounded fan-out would storm one upstream when many
// schedules align.
const maxJobWorkers = 4

// modelInfoRefresher is the slice of modelinfo the maintenance loop needs.
type modelInfoRefresher interface {
	MergedView(ctx context.Context, providerID string) ([]modelinfo.ModelView, error)
}

// PluginJobs is the luaplugin surface the maintenance path needs: job
// schedules plus one job execution. The luaplugin Service satisfies it; the
// registry and RunJob signatures are storage-agnostic, so no luaplugin
// statement runs here.
type PluginJobs interface {
	Jobs(typeKey string) map[string]luaplugin.JobSpec
	RunJob(ctx context.Context, typeKey, jobName, reason, providerID string, providerConfig map[string]any) error
}

// ProviderInventory is the provider surface the maintenance path needs:
// one row for manual triggers plus the full list for ticks. Signatures
// match the provider GORM service.
type ProviderInventory interface {
	Get(ctx context.Context, identifier string) (*models.ProviderInstance, error)
	List(ctx context.Context) ([]*models.ProviderInstance, error)
}

// Compile-time proof that the GORM-backed provider service and the
// luaplugin service plug into the narrow maintenance interfaces without
// involving their package internals.
var (
	_ PluginJobs        = (*luaplugin.Service)(nil)
	_ ProviderInventory = (*provider.GormService)(nil)
)

// GormService runs background jobs for provider plugins against
// GORM-compatible dependencies. Scheduling semantics: bounded workers,
// overlap skips, startup bypass, auto-sync gate.
type GormService struct {
	jobBackend     PluginJobs
	providerSource ProviderInventory
	interval       time.Duration
	logger         *slog.Logger

	modelInfoRefresher modelInfoRefresher

	mutex   sync.Mutex
	lastRun map[string]time.Time
	running map[string]bool
}

// NewGormService constructs a GORM-backed maintenance service with the
// default check interval. Construction performs no I/O. SetModelInfoService
// wires model metadata refresh for providers with
// config.models_auto_sync enabled.
func NewGormService(jobBackend PluginJobs, providerSource ProviderInventory, logger *slog.Logger) *GormService {
	return &GormService{
		jobBackend:     jobBackend,
		providerSource: providerSource,
		interval:       defaultCheckInterval,
		logger:         logger,
		lastRun:        map[string]time.Time{},
		running:        map[string]bool{},
	}
}

// SetModelInfoService wires model metadata refresh for providers with
// config.models_auto_sync enabled.
func (s *GormService) SetModelInfoService(refresher modelInfoRefresher) {
	s.modelInfoRefresher = refresher
}

// WithInterval overrides the check interval (useful for testing).
func (s *GormService) WithInterval(duration time.Duration) *GormService {
	s.interval = duration
	return s
}

// RunStartupRefresh runs startup jobs synchronously. It never fails
// startup: every outcome logs inside the pass, and cancellation only stops
// launching new workers.
func (s *GormService) RunStartupRefresh(ctx context.Context) {
	s.runStartupJobs(ctx)
}

// Start launches the maintenance loop in a background goroutine.
// It stops when ctx is cancelled.
func (s *GormService) Start(ctx context.Context) {
	s.logInfo("maintenance gorm service started", "interval", s.interval)
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				s.logInfo("maintenance gorm service stopped")
				return
			case <-ticker.C:
				s.runCycle(ctx)
			}
		}
	}()
}

// RunJob triggers one job for one provider on demand (dashboard).
// Unknown jobs fail loudly; running jobs report busy.
func (s *GormService) RunJob(ctx context.Context, providerID, jobName string) error {
	instance, err := s.providerSource.Get(ctx, providerID)
	if err != nil {
		return err
	}
	jobs := s.jobBackend.Jobs(instance.TypeKey)
	if _, ok := jobs[jobName]; !ok {
		return fmt.Errorf("job %q not found for provider %q", jobName, providerID)
	}
	return s.launch(ctx, instance, jobName, "manual")
}

// runCycle runs due jobs, then syncs opted-in model lists. A provider-list
// failure never skips the model sync: the jobs are independent and share
// only the tick.
func (s *GormService) runCycle(ctx context.Context) {
	s.runDueJobs(ctx, false)
	s.syncProviderModels(ctx)
}

// runStartupJobs runs every job flagged run_on_startup once.
func (s *GormService) runStartupJobs(ctx context.Context) {
	s.runDueJobs(ctx, true)
}

// runDueJobs launches due jobs through a bounded worker pool. Workers are
// independent: one job's failure never affects the others. Overlap skips:
// a job already running keeps its slot and the tick passes it by.
// Cancellation stops launching new workers; in-flight workers run to
// completion so no job is abandoned mid-write.
func (s *GormService) runDueJobs(ctx context.Context, startup bool) {
	if s.providerSource == nil || s.jobBackend == nil {
		return
	}
	providers, err := s.providerSource.List(ctx)
	if err != nil {
		s.logError("maintenance: list providers failed", "err", err)
		return
	}
	type work struct {
		instance *models.ProviderInstance
		jobName  string
	}
	var queue []work
	for _, providerInstance := range providers {
		if providerInstance == nil || providerInstance.Disabled {
			continue
		}
		jobs := s.jobBackend.Jobs(providerInstance.TypeKey)
		names := make([]string, 0, len(jobs))
		for name := range jobs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			jobSpecification := jobs[name]
			if startup {
				if !jobSpecification.RunOnStartup {
					continue
				}
			} else if !s.due(providerInstance.ID, name, jobSpecification.IntervalSeconds) {
				continue
			}
			if !s.claim(providerInstance.ID, name) {
				continue
			}
			queue = append(queue, work{instance: providerInstance, jobName: name})
		}
	}
	var waitGroup sync.WaitGroup
	semaphore := make(chan struct{}, maxJobWorkers)
loop:
	for _, task := range queue {
		select {
		case <-ctx.Done():
			s.release(task.instance.ID, task.jobName)
			break loop
		case semaphore <- struct{}{}:
		}
		waitGroup.Add(1)
		go func(task work) {
			defer waitGroup.Done()
			defer func() { <-semaphore }()
			defer s.release(task.instance.ID, task.jobName)
			s.runOne(ctx, task.instance, task.jobName, map[bool]string{true: "startup", false: "tick"}[startup])
		}(task)
	}
	waitGroup.Wait()
}

// due reports whether a job's interval elapsed since its last run.
// Startup runs bypass the interval.
func (s *GormService) due(providerID, jobName string, intervalSeconds int64) bool {
	if intervalSeconds <= 0 {
		return true
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	last, ok := s.lastRun[providerID+"\x00"+jobName]
	if !ok {
		return true
	}
	return last.Add(intervalSecondsToDuration(intervalSeconds)).Before(nowUTC())
}

// claim marks a job running. False means another worker owns it.
func (s *GormService) claim(providerID, jobName string) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	key := providerID + "\x00" + jobName
	if s.running[key] {
		return false
	}
	s.running[key] = true
	return true
}

func (s *GormService) release(providerID, jobName string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	key := providerID + "\x00" + jobName
	delete(s.running, key)
	s.lastRun[key] = nowUTC()
}

// launch runs one job synchronously in the caller goroutine (dashboard
// trigger). Overlap refuses with a busy error instead of queueing.
func (s *GormService) launch(ctx context.Context, instance *models.ProviderInstance, jobName, reason string) error {
	if !s.claim(instance.ID, jobName) {
		return &jobBusyError{providerID: instance.ID, job: jobName}
	}
	defer s.release(instance.ID, jobName)
	s.runOne(ctx, instance, jobName, reason)
	return nil
}

func (s *GormService) runOne(ctx context.Context, instance *models.ProviderInstance, jobName, reason string) {
	if err := s.jobBackend.RunJob(ctx, instance.TypeKey, jobName, reason, instance.ID, instance.Config); err != nil {
		s.logError("maintenance: job failed",
			"provider_id", instance.ID, "type", instance.TypeKey, "job", jobName, "err", err)
		return
	}
	s.logDebug("maintenance: job completed",
		"provider_id", instance.ID, "type", instance.TypeKey, "job", jobName)
}

// syncProviderModels warms the model metadata cache for providers that opted
// into automatic sync (config.models_auto_sync). The MergedView TTL (1h)
// throttles actual upstream calls; disabled providers are skipped. A
// missing refresher disables the pass; fetch failures log and continue.
func (s *GormService) syncProviderModels(ctx context.Context) {
	if s.modelInfoRefresher == nil || s.providerSource == nil {
		return
	}
	providers, err := s.providerSource.List(ctx)
	if err != nil {
		s.logWarn("maintenance: list providers for model sync failed", "err", err)
		return
	}
	for _, providerInstance := range providers {
		if ctx.Err() != nil {
			return
		}
		if providerInstance.Disabled {
			continue
		}
		enabled, _ := providerInstance.Config["models_auto_sync"].(bool)
		if !enabled {
			continue
		}
		if _, err := s.modelInfoRefresher.MergedView(ctx, providerInstance.ID); err != nil {
			s.logWarn("maintenance: model auto-sync failed", "provider_id", providerInstance.ID, "err", err)
		}
	}
}

// logInfo records an informational line when a logger is wired.
func (s *GormService) logInfo(message string, arguments ...any) {
	if s.logger != nil {
		s.logger.Info(message, arguments...)
	}
}

// logWarn records a warning line when a logger is wired.
func (s *GormService) logWarn(message string, arguments ...any) {
	if s.logger != nil {
		s.logger.Warn(message, arguments...)
	}
}

// logError records an error line when a logger is wired.
func (s *GormService) logError(message string, arguments ...any) {
	if s.logger != nil {
		s.logger.Error(message, arguments...)
	}
}

// logDebug records a debug line when a logger is wired.
func (s *GormService) logDebug(message string, arguments ...any) {
	if s.logger != nil {
		s.logger.Debug(message, arguments...)
	}
}
