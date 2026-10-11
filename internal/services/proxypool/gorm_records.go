package proxypool

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	proxypoollib "github.com/TheSlopMachine/proxypool"
	"gorm.io/gorm"
)

// Write-path batch bounds. Wave 7 owns Flush and Clear; these constants pin
// the agreed sizes so the stubs below document them.
const (
	gormStateUpsertBatchSize      = 500
	gormStateFlushTransactionSize = 20000
	gormStateDeleteBatchSize      = 500
)

// gormCache buffers proxy states in memory and serves the library CacheSource
// contract from SQLite records. The buffering shape mirrors dbCache: items
// holds the working set, dirty and deleted stage the next write, Flush
// persists the staged sets, remember surfaces persistence failures through
// takeError.
type gormCache struct {
	mu      sync.RWMutex
	states  *repository.GormRepository[record.ProxyState]
	items   map[string]proxypoollib.ProxyState
	dirty   map[string]struct{}
	deleted map[string]struct{}
	err     error
}

// gormCache implements proxypoollib.CacheSource.
var _ proxypoollib.CacheSource = (*gormCache)(nil)

// newGormCache loads every persisted proxy state into memory. Writes stay
// buffered until Wave 7 implements Flush.
func newGormCache(ctx context.Context, database *gorm.DB) (*gormCache, error) {
	states := repository.NewGormRepository[record.ProxyState](database, "url")
	loaded := &gormCache{
		states:  states,
		items:   map[string]proxypoollib.ProxyState{},
		dirty:   map[string]struct{}{},
		deleted: map[string]struct{}{},
	}
	rows, err := states.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("load proxy states: %w", err)
	}
	for _, persisted := range rows {
		if persisted.URL == "" {
			continue
		}
		loaded.items[persisted.URL] = recordToProxyState(persisted)
	}
	return loaded, nil
}

// Get returns one buffered state by canonical URL.
func (c *gormCache) Get(url string) (proxypoollib.ProxyState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	state, ok := c.items[url]
	return cloneState(state), ok
}

// Set stages one state for the next Flush.
func (c *gormCache) Set(state proxypoollib.ProxyState) {
	if state.URL == "" {
		c.remember(fmt.Errorf("proxy cache: empty URL"))
		return
	}
	state = cloneState(state)
	c.mu.Lock()
	c.items[state.URL] = state
	if c.dirty == nil {
		c.dirty = map[string]struct{}{}
	}
	c.dirty[state.URL] = struct{}{}
	if c.deleted == nil {
		c.deleted = map[string]struct{}{}
	}
	delete(c.deleted, state.URL)
	c.mu.Unlock()
}

// Flush persists every buffered write. Stubbed: Wave 7 owns the batching.
func (c *gormCache) Flush() error {
	// TODO(Wave 7): upsert dirty states in 500-row batches inside
	// 20000-state transactions and remove deleted URLs with IN clauses in
	// 500s. Draining dirty/deleted stays with the write so a failed batch
	// keeps its staged sets.
	return nil
}

// Delete removes a proxy from the in-memory cache and buffers its persistence deletion.
func (c *gormCache) Delete(url string) {
	if url == "" {
		return
	}
	c.mu.Lock()
	delete(c.items, url)
	if c.deleted == nil {
		c.deleted = map[string]struct{}{}
	}
	delete(c.dirty, url)
	c.deleted[url] = struct{}{}
	c.mu.Unlock()
}

// All returns every buffered state.
func (c *gormCache) All() []proxypoollib.ProxyState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	states := make([]proxypoollib.ProxyState, 0, len(c.items))
	for _, state := range c.items {
		states = append(states, cloneState(state))
	}
	return states
}

// Clear drops the in-memory set and its staged writes.
func (c *gormCache) Clear() {
	// TODO(Wave 7): delete persisted states through GormRepository.Clear,
	// the one path allowed global deletes under AllowGlobalUpdate.
	c.mu.Lock()
	c.items = map[string]proxypoollib.ProxyState{}
	c.dirty = map[string]struct{}{}
	c.deleted = map[string]struct{}{}
	c.mu.Unlock()
}

func (c *gormCache) remember(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
}

func (c *gormCache) peekError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.err
}

func (c *gormCache) takeError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.err
	c.err = nil
	return err
}

// proxyStateToRecord converts a library proxy state to its SQLite record.
// Latency maps to nanoseconds; FailReason maps to its string code, which the
// proxy_states check constraint accepts verbatim.
func proxyStateToRecord(state proxypoollib.ProxyState) record.ProxyState {
	return record.ProxyState{
		URL:                state.URL,
		IP:                 state.IP,
		Port:               state.Port,
		Location:           state.Location,
		IsDead:             state.IsDead,
		Source:             state.Source,
		FailReason:         string(state.FailReason),
		SoftFails:          state.SoftFails,
		Suspect:            state.Suspect,
		LastSeenInSource:   state.LastSeenInSource,
		Penalty:            state.Penalty,
		Score:              state.Score,
		ReviveAt:           state.ReviveAt,
		LastCheckedAt:      state.LastCheckedAt,
		LatencyNanoseconds: int64(state.Latency),
		Metadata:           cloneMetadata(state.Metadata),
	}
}

// recordToProxyState converts a SQLite record to its library proxy state.
func recordToProxyState(persisted record.ProxyState) proxypoollib.ProxyState {
	return proxypoollib.ProxyState{
		URL:              persisted.URL,
		IP:               persisted.IP,
		Port:             persisted.Port,
		Location:         persisted.Location,
		IsDead:           persisted.IsDead,
		Source:           persisted.Source,
		FailReason:       proxypoollib.FailReason(persisted.FailReason),
		SoftFails:        persisted.SoftFails,
		Suspect:          persisted.Suspect,
		LastSeenInSource: persisted.LastSeenInSource,
		Penalty:          persisted.Penalty,
		Score:            persisted.Score,
		ReviveAt:         persisted.ReviveAt,
		LastCheckedAt:    persisted.LastCheckedAt,
		Latency:          time.Duration(persisted.LatencyNanoseconds),
		Metadata:         cloneMetadata(persisted.Metadata),
	}
}

// customPoolToRecords splits a custom pool into its pool row and ordered
// entries. Position follows the entries order. Custom pools are never builtin.
func customPoolToRecords(pool models.CustomProxyPool) (record.ProxyPool, []record.ProxyPoolEntry) {
	persisted := record.ProxyPool{
		ID:        pool.ID,
		Name:      pool.Name,
		Builtin:   false,
		CreatedAt: pool.CreatedAt,
		UpdatedAt: pool.UpdatedAt,
	}
	entries := make([]record.ProxyPoolEntry, 0, len(pool.Entries))
	for position, entry := range pool.Entries {
		entries = append(entries, record.ProxyPoolEntry{
			PoolID:   pool.ID,
			Position: position,
			URL:      entry.URL,
			Country:  entry.Country,
		})
	}
	return persisted, entries
}

// recordsToCustomPool joins a pool row with its entries. Entries sort by
// position; URL and country pass through verbatim.
func recordsToCustomPool(pool record.ProxyPool, entries []record.ProxyPoolEntry) models.CustomProxyPool {
	ordered := make([]record.ProxyPoolEntry, len(entries))
	copy(ordered, entries)
	sort.SliceStable(ordered, func(first, second int) bool {
		return ordered[first].Position < ordered[second].Position
	})
	restored := models.CustomProxyPool{
		ID:        pool.ID,
		Name:      pool.Name,
		Entries:   make([]models.ProxyEntry, 0, len(ordered)),
		CreatedAt: pool.CreatedAt,
		UpdatedAt: pool.UpdatedAt,
	}
	for _, entry := range ordered {
		restored.Entries = append(restored.Entries, models.ProxyEntry{URL: entry.URL, Country: entry.Country})
	}
	return restored
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
