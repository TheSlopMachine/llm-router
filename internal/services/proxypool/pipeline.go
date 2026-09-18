package proxypool

// Streaming check pipeline for list-sourced proxies.
//
// The old flow wrote every fetched candidate into the DB (16k+ rows) and
// checked them later. The new flow keeps candidates in memory only: a fetch
// enqueues them, a bounded worker pool probes them as they arrive, and only
// verified-alive proxies ever reach the DB. Per-source state (status,
// counters) is reported to the dashboard so the pool's work is visible.

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// Source lifecycle statuses reported to the dashboard.
const (
	SourceStatusIdle     = "idle"
	SourceStatusFetching = "fetching"
	SourceStatusChecking = "checking"
)

// queueCapacity bounds the in-memory candidate backlog per feed. The feeder
// goroutine blocks on a full queue, so memory stays flat regardless of list
// size.
const queueCapacity = 4096

// errWorkersNotStarted reports RefreshSource before StartWorkers.
var errWorkersNotStarted = errors.New("proxypool: StartWorkers must be called before RefreshSource")

type checkJob struct {
	source string
	proxy  *models.Proxy
}

// sourceState is the mutable per-source runtime view. Counters reset on
// every fetch; alive is not tracked here: SourceInfo.Alive reports the
// DB-level count of verified proxies so the per-source number always
// matches the pool-wide totals.
type sourceState struct {
	status      string
	total       int
	checked     int
	pending     int
	lastFetchAt time.Time
	lastError   string
}

// SourceInfo is the dashboard-facing snapshot of one source.
type SourceInfo struct {
	Key         string    `json:"key"`
	Status      string    `json:"status"`
	Total       int       `json:"total"`
	Checked     int       `json:"checked"`
	Alive       int       `json:"alive"`
	Pending     int       `json:"pending"`
	LastFetchAt time.Time `json:"last_fetch_at,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
}

// pipeline holds the queue, workers and per-source states.
type pipeline struct {
	mu      sync.Mutex
	states  map[string]*sourceState
	jobs    chan checkJob
	pending map[string]bool // proxy IDs already queued, dedupes overlapping refreshes
	started bool
}

func newPipeline() *pipeline {
	return &pipeline{
		states:  map[string]*sourceState{},
		jobs:    make(chan checkJob, queueCapacity),
		pending: map[string]bool{},
	}
}

// startWorkers launches the check worker pool. It must be called once before
// the first RefreshSource; the workers stop with ctx.
func (s *Service) startWorkers(ctx context.Context) {
	s.pipeline.mu.Lock()
	if s.pipeline.started {
		s.pipeline.mu.Unlock()
		return
	}
	s.pipeline.started = true
	s.pipeline.mu.Unlock()
	for i := 0; i < checkConcurrency; i++ {
		go s.worker(ctx)
	}
}

// worker probes queued candidates. Alive proxies are persisted (health
// memory preserved); dead ones are culled from the DB when present.
func (s *Service) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-s.pipeline.jobs:
			s.checkCandidate(ctx, job)
		}
	}
}

func (s *Service) checkCandidate(ctx context.Context, job checkJob) {
	p := job.proxy
	probeCtx, cancel := context.WithTimeout(ctx, s.DialTimeout)
	start := time.Now()
	err := probeThrough(probeCtx, p.URL, s.CheckURL)
	cancel()
	p.LatencyMs = time.Since(start).Milliseconds()
	p.LastCheckAt = util.Now()
	alive := err == nil

	if alive {
		// Ground truth beats list metadata: resolve the real exit country.
		if country, derr := DetectExitCountry(context.WithoutCancel(ctx), p.URL); derr == nil && country != "" {
			p.Country = country
		}
		if old, gerr := s.repo.Get(p.ID); gerr == nil && old != nil {
			p.ProviderHealth = old.ProviderHealth
			p.CreatedAt = old.CreatedAt
		}
		p.Alive = true
		_ = s.repo.Put(p.ID, p)
	} else if p.Source != ManualSource {
		// A list proxy that answers no more leaves the pool entirely.
		_ = s.repo.Delete(p.ID)
	}
	s.finishJob(job.source, p.ID)
}

func (s *Service) finishJob(source, id string) {
	s.pipeline.mu.Lock()
	defer s.pipeline.mu.Unlock()
	delete(s.pipeline.pending, id)
	st := s.pipeline.states[source]
	if st == nil {
		return
	}
	st.checked++
	st.pending--
	if st.pending <= 0 && st.status == SourceStatusChecking {
		st.pending = 0
		st.status = SourceStatusIdle
	}
}

// BeginFetch marks a source as fetching. Pair with RefreshSource on success
// or FailFetch on error.
func (s *Service) BeginFetch(sourceKey string) {
	s.pipeline.mu.Lock()
	defer s.pipeline.mu.Unlock()
	st := s.stateFor(ListSource(sourceKey))
	st.status = SourceStatusFetching
	st.lastError = ""
}

// FailFetch records a fetch error and returns the source to idle.
func (s *Service) FailFetch(sourceKey string, err error) {
	s.pipeline.mu.Lock()
	defer s.pipeline.mu.Unlock()
	st := s.stateFor(ListSource(sourceKey))
	st.status = SourceStatusIdle
	st.lastError = err.Error()
}

// RefreshSource enqueues freshly fetched candidates for checking. Only
// verified proxies are persisted; nothing unverified touches the DB. The
// feed runs in a detached goroutine and returns immediately: candidates
// start flowing to workers as soon as the first list arrives.
func (s *Service) RefreshSource(sourceKey string, candidates []models.ProxyCandidate) (int, error) {
	source := ListSource(sourceKey)
	s.pipeline.mu.Lock()
	if !s.pipeline.started {
		s.pipeline.mu.Unlock()
		return 0, errWorkersNotStarted
	}
	st := s.stateFor(source)
	st.status = SourceStatusChecking
	st.total = len(candidates)
	st.checked = 0
	st.pending = 0
	st.lastFetchAt = util.Now()
	st.lastError = ""

	jobs := make([]checkJob, 0, len(candidates))
	enqueued := 0
	for _, c := range candidates {
		p, err := candidateToProxy(c, source)
		if err != nil {
			continue
		}
		if s.pipeline.pending[p.ID] {
			continue
		}
		s.pipeline.pending[p.ID] = true
		jobs = append(jobs, checkJob{source: source, proxy: p})
		enqueued++
	}
	st.pending = enqueued
	if enqueued == 0 {
		st.status = SourceStatusIdle
	}
	s.pipeline.mu.Unlock()

	go func() {
		for _, job := range jobs {
			s.pipeline.jobs <- job
		}
	}()
	return enqueued, nil
}

// SourceInfos snapshots the runtime state of the given source keys. Alive
// is the DB-level count of verified proxies per source, so the per-source
// numbers sum to the pool-wide totals the dashboard header reports.
func (s *Service) SourceInfos(keys []string) []SourceInfo {
	s.pipeline.mu.Lock()
	defer s.pipeline.mu.Unlock()
	out := make([]SourceInfo, 0, len(keys))
	for _, key := range keys {
		info := SourceInfo{Key: key, Status: SourceStatusIdle}
		if st, ok := s.pipeline.states[ListSource(key)]; ok {
			info.Status = st.status
			info.Total = st.total
			info.Checked = st.checked
			info.Pending = st.pending
			info.LastFetchAt = st.lastFetchAt
			info.LastError = st.lastError
		}
		alive, err := s.repo.ListFiltered(func(p *models.Proxy) bool {
			return p.Source == ListSource(key) && p.Alive
		})
		if err == nil {
			info.Alive = len(alive)
		}
		out = append(out, info)
	}
	return out
}

// SourceProxies returns alive list-sourced proxies of one source, fastest
// first, paginated. The second return value is the total alive count.
func (s *Service) SourceProxies(sourceKey string, offset, limit int) ([]*models.Proxy, int, error) {
	source := ListSource(sourceKey)
	alive, err := s.repo.ListFiltered(func(p *models.Proxy) bool {
		return p.Source == source && p.Alive
	})
	if err != nil {
		return nil, 0, err
	}
	sort.Slice(alive, func(i, j int) bool { return alive[i].LatencyMs < alive[j].LatencyMs })
	total := len(alive)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}
	page := alive[offset:end]
	if page == nil {
		page = []*models.Proxy{}
	}
	return page, total, nil
}

func (s *Service) stateFor(source string) *sourceState {
	st, ok := s.pipeline.states[source]
	if !ok {
		st = &sourceState{status: SourceStatusIdle}
		s.pipeline.states[source] = st
	}
	return st
}
