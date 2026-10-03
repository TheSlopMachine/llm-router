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
	providers, err := s.providerSvc.List()
	if err != nil {
		s.logger.Error("maintenance: list providers failed", "err", err)
		return
	}
	type work struct {
		inst    *models.ProviderInstance
		jobName string
	}
	var queue []work
	for _, p := range providers {
		if p == nil || p.Disabled {
			continue
		}
		if s.luaSvc == nil {
			continue
		}
		jobs := s.luaSvc.Jobs(p.TypeKey)
		names := make([]string, 0, len(jobs))
		for name := range jobs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			spec := jobs[name]
			if startup {
				if !spec.RunOnStartup {
					continue
				}
			} else if !s.due(p.ID, name, spec.IntervalSeconds) {
				continue
			}
			if !s.claim(p.ID, name) {
				continue
			}
			queue = append(queue, work{inst: p, jobName: name})
		}
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxJobWorkers)
loop:
	for _, w := range queue {
		select {
		case <-ctx.Done():
			s.release(w.inst.ID, w.jobName)
			break loop
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(w work) {
			defer wg.Done()
			defer func() { <-sem }()
			defer s.release(w.inst.ID, w.jobName)
			s.runOne(ctx, w.inst, w.jobName, map[bool]string{true: "startup", false: "tick"}[startup])
		}(w)
	}
	wg.Wait()
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
func (s *Service) launch(ctx context.Context, inst *models.ProviderInstance, jobName, reason string) error {
	if !s.claim(inst.ID, jobName) {
		return &jobBusyError{providerID: inst.ID, job: jobName}
	}
	defer s.release(inst.ID, jobName)
	s.runOne(ctx, inst, jobName, reason)
	return nil
}

func (s *Service) runOne(ctx context.Context, inst *models.ProviderInstance, jobName, reason string) {
	if err := s.luaSvc.RunJob(ctx, inst.TypeKey, jobName, reason, inst.ID, inst.Config); err != nil {
		s.logger.Error("maintenance: job failed",
			"provider_id", inst.ID, "type", inst.TypeKey, "job", jobName, "err", err)
		return
	}
	s.logger.Debug("maintenance: job completed",
		"provider_id", inst.ID, "type", inst.TypeKey, "job", jobName)
}

// syncProviderModels warms the model metadata cache for providers that opted
// into automatic sync (config.models_auto_sync). The MergedView TTL (1h)
// throttles actual upstream calls; disabled providers are skipped.
func (s *Service) syncProviderModels(ctx context.Context) {
	if s.modelInfoSvc == nil {
		return
	}
	providers, err := s.providerSvc.List()
	if err != nil {
		s.logger.Warn("maintenance: list providers for model sync failed", "err", err)
		return
	}
	for _, p := range providers {
		if ctx.Err() != nil {
			return
		}
		if p.Disabled {
			continue
		}
		enabled, _ := p.Config["models_auto_sync"].(bool)
		if !enabled {
			continue
		}
		if _, err := s.modelInfoSvc.MergedView(ctx, p.ID); err != nil {
			s.logger.Warn("maintenance: model auto-sync failed", "provider_id", p.ID, "err", err)
		}
	}
}
