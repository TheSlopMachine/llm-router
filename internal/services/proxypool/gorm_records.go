package proxypool

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	proxypoollib "github.com/TheSlopMachine/proxypool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	mu       sync.RWMutex
	database *gorm.DB
	states   *repository.GormRepository[record.ProxyState]
	items    map[string]proxypoollib.ProxyState
	dirty    map[string]struct{}
	deleted  map[string]struct{}
	err      error
}

// gormCache implements proxypoollib.CacheSource.
var _ proxypoollib.CacheSource = (*gormCache)(nil)

// newGormCache loads every persisted proxy state into memory. Writes stay
// buffered until Flush persists them.
func newGormCache(ctx context.Context, database *gorm.DB) (*gormCache, error) {
	states := repository.NewGormRepository[record.ProxyState](database, "url")
	loaded := &gormCache{
		database: database,
		states:   states,
		items:    map[string]proxypoollib.ProxyState{},
		dirty:    map[string]struct{}{},
		deleted:  map[string]struct{}{},
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

// Flush persists every buffered write in chunked transactions. States set
// concurrently with a flush re-dirty their keys and land on the next call.
// Dirty states upsert in 500-row batches inside one transaction per 20000
// states; deleted URLs remove through IN clauses in 500s. The staged sets
// drain before the write and restore on failure, so a failed batch keeps
// its staged sets for the next call. Persistence failures surface through
// remember and the returned error.
func (c *gormCache) Flush() error {
	c.mu.Lock()
	if len(c.dirty) == 0 && len(c.deleted) == 0 {
		c.mu.Unlock()
		return nil
	}
	pending := make([]proxypoollib.ProxyState, 0, len(c.dirty))
	for url := range c.dirty {
		state, ok := c.items[url]
		if !ok {
			continue
		}
		pending = append(pending, cloneState(state))
	}
	deletions := make([]string, 0, len(c.deleted))
	for url := range c.deleted {
		deletions = append(deletions, url)
	}
	c.dirty = map[string]struct{}{}
	c.deleted = map[string]struct{}{}
	c.mu.Unlock()

	ctx := context.Background()
	for start := 0; start < len(pending); start += gormStateFlushTransactionSize {
		end := start + gormStateFlushTransactionSize
		if end > len(pending) {
			end = len(pending)
		}
		records := make([]record.ProxyState, 0, end-start)
		for _, state := range pending[start:end] {
			if state.URL == "" {
				continue
			}
			records = append(records, proxyStateToRecord(state))
		}
		if len(records) == 0 {
			continue
		}
		if err := c.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			if err := gorm.G[record.ProxyState](transaction, clause.OnConflict{
				Columns:   []clause.Column{{Name: "url"}},
				UpdateAll: true,
			}).CreateInBatches(ctx, &records, gormStateUpsertBatchSize); err != nil {
				return db.Translate(err)
			}
			return nil
		}); err != nil {
			c.restage(pending, deletions)
			c.remember(err)
			return err
		}
	}
	for start := 0; start < len(deletions); start += gormStateDeleteBatchSize {
		end := start + gormStateDeleteBatchSize
		if end > len(deletions) {
			end = len(deletions)
		}
		values := make([]any, 0, end-start)
		for _, url := range deletions[start:end] {
			values = append(values, url)
		}
		if len(values) == 0 {
			continue
		}
		if err := c.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			if _, err := gorm.G[record.ProxyState](transaction,
				clause.IN{Column: clause.Column{Name: "url"}, Values: values}).Delete(ctx); err != nil {
				return db.Translate(err)
			}
			return nil
		}); err != nil {
			c.restage(pending, deletions)
			c.remember(err)
			return err
		}
	}
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

// Clear drops the persisted states through GormRepository.Clear, the one
// path allowed global deletes under AllowGlobalUpdate, then drops the
// in-memory set and its staged writes. A persistence failure surfaces
// through remember and keeps the in-memory set.
func (c *gormCache) Clear() {
	if err := c.states.Clear(context.Background()); err != nil {
		c.remember(err)
		return
	}
	c.mu.Lock()
	c.items = map[string]proxypoollib.ProxyState{}
	c.dirty = map[string]struct{}{}
	c.deleted = map[string]struct{}{}
	c.mu.Unlock()
}

// restage returns snapshot sets to the staged maps after a failed write.
// Entries set concurrently with the flush stay staged: merging keeps every
// key. Items already hold the latest values, so only the flags restore.
func (c *gormCache) restage(pending []proxypoollib.ProxyState, deletions []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirty == nil {
		c.dirty = map[string]struct{}{}
	}
	for _, state := range pending {
		if state.URL == "" {
			continue
		}
		if _, ok := c.items[state.URL]; ok {
			c.dirty[state.URL] = struct{}{}
		}
	}
	if c.deleted == nil {
		c.deleted = map[string]struct{}{}
	}
	for _, url := range deletions {
		if url == "" {
			continue
		}
		if _, ok := c.items[url]; !ok {
			c.deleted[url] = struct{}{}
		}
	}
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
