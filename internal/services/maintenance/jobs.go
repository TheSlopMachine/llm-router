package maintenance

import (
	"context"
	"sort"
	"sync"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// runCycle runs due jobs, then syncs opted-in model lists. A provider-list
// failure never skips the model sync: the jobs are independent and share
// only the tick.
func (s *Service) runCycle(ctx context.Context) {
	s.runDueJobs(ctx, false)
	s.syncProviderModels(ctx)
}

// runStartupJobs runs every job flagged run_on_startup once.
func (s *Service) runStartupJobs(ctx context.Context) {
	s.runDueJobs(ctx, true)
}

// runDueJobs launches due jobs through a bounded worker pool. Workers are
// independent: one job's failure never affects the others. Overlap skips:
// a job already running keeps its slot and the tick passes it by.
// Cancellation stops launching new workers; in-flight workers run to
// completion so no job is abandoned mid-write.
func (s *Service) runDueJobs(ctx context.Context, startup bool) {
	providers, err := s.providerService.List()
	if err != nil {
		s.logger.Error("maintenance: list providers failed", "err", err)
		return
	}
	type work struct {
		instance *models.ProviderInstance
		jobName  string
	}
	var queue []work
	for _, provider := range providers {
		if provider == nil || provider.Disabled {
			continue
		}
		if s.luaService == nil {
			continue
		}
		jobs := s.luaService.Jobs(provider.TypeKey)
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
			} else if !s.due(provider.ID, name, jobSpecification.IntervalSeconds) {
				continue
			}
			if !s.claim(provider.ID, name) {
				continue
			}
			queue = append(queue, work{instance: provider, jobName: name})
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
func (s *Service) due(providerID, jobName string, intervalSeconds int64) bool {
	if intervalSeconds <= 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.lastRun[providerID+"\x00"+jobName]
	if !ok {
		return true
	}
	return last.Add(intervalSecondsToDuration(intervalSeconds)).Before(nowUTC())
}

// claim marks a job running. False means another worker owns it.
func (s *Service) claim(providerID, jobName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := providerID + "\x00" + jobName
	if s.running[key] {
		return false
	}
	s.running[key] = true
	return true
}

func (s *Service) release(providerID, jobName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := providerID + "\x00" + jobName
	delete(s.running, key)
	s.lastRun[key] = nowUTC()
}

// launch runs one job synchronously in the caller goroutine (dashboard
// trigger). Overlap refuses with a busy error instead of queueing.
func (s *Service) launch(ctx context.Context, instance *models.ProviderInstance, jobName, reason string) error {
	if !s.claim(instance.ID, jobName) {
		return &jobBusyError{providerID: instance.ID, job: jobName}
	}
	defer s.release(instance.ID, jobName)
	s.runOne(ctx, instance, jobName, reason)
	return nil
}

func (s *Service) runOne(ctx context.Context, instance *models.ProviderInstance, jobName, reason string) {
	if err := s.luaService.RunJob(ctx, instance.TypeKey, jobName, reason, instance.ID, instance.Config); err != nil {
		s.logger.Error("maintenance: job failed",
			"provider_id", instance.ID, "type", instance.TypeKey, "job", jobName, "err", err)
		return
	}
	s.logger.Debug("maintenance: job completed",
		"provider_id", instance.ID, "type", instance.TypeKey, "job", jobName)
}

// syncProviderModels warms the model metadata cache for providers that opted
// into automatic sync (config.models_auto_sync). The MergedView TTL (1h)
// throttles actual upstream calls; disabled providers are skipped.
func (s *Service) syncProviderModels(ctx context.Context) {
	if s.modelInfoRefresher == nil {
		return
	}
	providers, err := s.providerService.List()
	if err != nil {
		s.logger.Warn("maintenance: list providers for model sync failed", "err", err)
		return
	}
	for _, provider := range providers {
		if ctx.Err() != nil {
			return
		}
		if provider.Disabled {
			continue
		}
		enabled, _ := provider.Config["models_auto_sync"].(bool)
		if !enabled {
			continue
		}
		if _, err := s.modelInfoRefresher.MergedView(ctx, provider.ID); err != nil {
			s.logger.Warn("maintenance: model auto-sync failed", "provider_id", provider.ID, "err", err)
		}
	}
}
