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

// checkWindowSize is how many candidates of one source get probed per fetch
// cycle. Lists are walked in rotating windows: every fetch checks the next
// slice and eventually covers the whole list.
const checkWindowSize = 100

// minSourceAlive triggers a reserve refill: when a source drops below this
// many verified proxies, the next window is checked right away instead of
// waiting for the scheduled fetch.
const minSourceAlive = 10

// errWorkersNotStarted reports RefreshSource before StartWorkers.
var errWorkersNotStarted = errors.New("proxypool: StartWorkers must be called before RefreshSource")

type checkJob struct {
	source string
	proxy  *models.Proxy
}

// sourceState is the mutable per-source runtime view. Counters reset on
// every fetch; alive is not tracked here: SourceInfo.Alive reports the
// DB-level count of verified proxies so the per-source number always
// matches the pool-wide totals. candidates holds the last fetched list in
// RAM (never the DB) and feeds rotating check windows.
type sourceState struct {
	status      string
	total       int
	checked     int
	pending     int
	lastFetchAt time.Time
	lastError   string
	candidates  []*models.Proxy
	offset      int
	checkedIDs  map[string]bool // candidates probed since the last fetch
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
		old, gerr := s.repo.Get(p.ID)
		isNew := gerr != nil || old == nil
		if isNew {
			// Ground truth beats list metadata, but only on first insert:
			// re-checks verify liveness only and never re-probe geography.
			if country, derr := DetectExitCountry(context.WithoutCancel(ctx), p.URL); derr == nil && country != "" {
				p.Country = country
			}
		} else {
			p.ProviderHealth = old.ProviderHealth
			p.CreatedAt = old.CreatedAt
			if p.Country == "" {
				p.Country = old.Country
			}
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
	st.checkedIDs[id] = true
	if st.pending > 0 || st.status != SourceStatusChecking {
		return
	}
	st.pending = 0
	// Reserve refill: a source running low on verified proxies gets the
	// next window right away instead of idling until the scheduled fetch.
	// Refill walks candidates not yet probed in this fetch cycle, so a
	// list of duds can never spin the checker forever.
	alive, err := s.repo.ListFiltered(func(p *models.Proxy) bool {
		return p.Source == source && p.Alive
	})
	if err == nil && len(alive) < minSourceAlive && len(st.candidates) > len(st.checkedIDs) {
		s.enqueueWindowLocked(source, st)
		if st.pending > 0 {
			return
		}
	}
	st.status = SourceStatusIdle
}

// enqueueWindowLocked queues the next check window of a source's candidate
// list, rotating the offset past candidates already probed in this fetch
// cycle. Callers hold pipeline.mu.
func (s *Service) enqueueWindowLocked(source string, st *sourceState) int {
	n := len(st.candidates)
	if n == 0 {
		return 0
	}
	start := st.offset % n
	enqueued := 0
	for i := 0; i < n && enqueued < checkWindowSize; i++ {
		p := st.candidates[(start+i)%n]
		if st.checkedIDs[p.ID] || s.pipeline.pending[p.ID] {
			continue
		}
		s.pipeline.pending[p.ID] = true
		st.pending++
		enqueued++
		go func(job checkJob) {
			s.pipeline.jobs <- job
		}(checkJob{source: source, proxy: p})
	}
	st.offset = (start + checkWindowSize) % n
	st.status = SourceStatusChecking
	return enqueued
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

// RefreshSource stores the fetched list in RAM and enqueues the next check
// window. Only verified proxies are persisted; nothing unverified touches
// the DB. Candidates start flowing to workers as soon as the list arrives.
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

	st.candidates = st.candidates[:0]
	st.checkedIDs = map[string]bool{}
	for _, c := range candidates {
		p, err := candidateToProxy(c, source)
		if err != nil {
			continue
		}
		st.candidates = append(st.candidates, p)
	}
	offset := 0
	if meta, err := s.meta.Get(source); err == nil && meta != nil {
		offset = meta.Offset
	}
	st.offset = offset
	enqueued := s.enqueueWindowLocked(source, st)
	_ = s.meta.Put(source, &sourceFetchMeta{Total: len(candidates), Offset: st.offset, LastFetchAt: st.lastFetchAt})
	if st.pending == 0 {
		st.status = SourceStatusIdle
	}
	s.pipeline.mu.Unlock()
	return enqueued, nil
}

// SourceInfos snapshots the runtime state of the given source keys. Alive
// is the DB-level count of verified proxies per source, so the per-source
// numbers sum to the pool-wide totals the dashboard header reports. Fetch
// totals fall back to the persisted last-fetch meta when the RAM pipeline
// has not seen a fetch since startup.
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
		} else if meta, err := s.meta.Get(ListSource(key)); err == nil && meta != nil {
			info.Total = meta.Total
			info.LastFetchAt = meta.LastFetchAt
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

// PoolTotals reports the dashboard header counters: total is every proxy
// the pool knows about (manual entries plus the last fetched totals of all
// list sources), alive is the DB-level verified count.
func (s *Service) PoolTotals() (total, alive int, err error) {
	all, err := s.repo.List()
	if err != nil {
		return 0, 0, err
	}
	for _, p := range all {
		if p.Alive {
			alive++
		}
		if p.Source == ManualSource {
			total++
		}
	}
	// Sum the last fetched total of every known source, whether it still
	// has verified proxies pooled or not.
	metas, err := s.meta.List()
	if err != nil {
		return 0, 0, err
	}
	for _, m := range metas {
		total += m.Total
	}
	return total, alive, nil
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
