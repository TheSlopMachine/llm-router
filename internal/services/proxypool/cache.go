package proxypool

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	proxypoollib "github.com/TheSlopMachine/proxypool"
	bolt "go.etcd.io/bbolt"
)

// flushChunkSize bounds one persistence transaction: a full feed cycle
// lands in a handful of fsync'd commits instead of one per proxy.
const flushChunkSize = 20000

type dbCache struct {
	mu      sync.RWMutex
	db      *db.DB
	repo    *repository.Repository[proxypoollib.ProxyState]
	items   map[string]proxypoollib.ProxyState
	dirty   map[string]struct{}
	deleted map[string]struct{}
	err     error
}

func newDBCache(database *db.DB) (*dbCache, error) {
	c := &dbCache{
		db:      database,
		repo:    repository.New[proxypoollib.ProxyState](database, db.BucketProxyCache, "proxy cache"),
		items:   map[string]proxypoollib.ProxyState{},
		dirty:   map[string]struct{}{},
		deleted: map[string]struct{}{},
	}
	states, err := c.repo.List()
	if err != nil {
		return nil, fmt.Errorf("load proxy cache: %w", err)
	}
	for _, state := range states {
		if state != nil && state.URL != "" {
			c.items[state.URL] = cloneState(*state)
		}
	}
	return c, nil
}

func (c *dbCache) Get(url string) (proxypoollib.ProxyState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	state, ok := c.items[url]
	return cloneState(state), ok
}

func (c *dbCache) Set(state proxypoollib.ProxyState) {
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
// Returns the first persistence error, if any; callers record it with
// remember so read gates observe it through the usual takeError path.
func (c *dbCache) Flush() error {
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

	if len(pending) == 0 {
		return c.writeChunk(nil, deletions)
	}
	for start := 0; start < len(pending); start += flushChunkSize {
		end := start + flushChunkSize
		if end > len(pending) {
			end = len(pending)
		}
		chunkDeletions := []string(nil)
		if start == 0 {
			chunkDeletions = deletions
		}
		if err := c.writeChunk(pending[start:end], chunkDeletions); err != nil {
			return err
		}
	}
	return nil
}

// writeChunk stores one batch of states in a single transaction, using the
// same JSON encoding as Repository.Put. Unencodable entries are skipped so
// one bad row cannot block the batch.
func (c *dbCache) writeChunk(states []proxypoollib.ProxyState, deletions []string) error {
	type record struct {
		key  string
		data []byte
	}
	records := make([]record, 0, len(states))
	for _, state := range states {
		if state.URL == "" {
			continue
		}
		enc, err := json.Marshal(&state)
		if err != nil {
			c.remember(fmt.Errorf("marshal proxy cache: %w", err))
			continue
		}
		records = append(records, record{key: state.URL, data: enc})
	}
	if len(records) == 0 && len(deletions) == 0 {
		return nil
	}
	return c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketProxyCache)
		if b == nil {
			var err error
			b, err = tx.CreateBucketIfNotExists(db.BucketProxyCache)
			if err != nil {
				return fmt.Errorf("create bucket %q: %w", string(db.BucketProxyCache), err)
			}
		}
		for _, key := range deletions {
			if err := b.Delete([]byte(key)); err != nil {
				return err
			}
		}
		for _, r := range records {
			if err := b.Put([]byte(r.key), r.data); err != nil {
				return err
			}
		}
		return nil
	})
}

// Delete removes a proxy from the in-memory cache and buffers its persistence deletion.
func (c *dbCache) Delete(url string) {
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

func (c *dbCache) All() []proxypoollib.ProxyState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	states := make([]proxypoollib.ProxyState, 0, len(c.items))
	for _, state := range c.items {
		states = append(states, cloneState(state))
	}
	return states
}

func (c *dbCache) Clear() {
	err := c.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(db.BucketProxyCache); err != nil && !errors.Is(err, bolt.ErrBucketNotFound) {
			return err
		}
		_, err := tx.CreateBucket(db.BucketProxyCache)
		return err
	})
	if err != nil {
		c.remember(err)
		return
	}
	c.mu.Lock()
	c.items = map[string]proxypoollib.ProxyState{}
	c.dirty = map[string]struct{}{}
	c.deleted = map[string]struct{}{}
	c.mu.Unlock()
}

func (c *dbCache) remember(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
}

func (c *dbCache) peekError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.err
}

func (c *dbCache) takeError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.err
	c.err = nil
	return err
}

func cloneState(state proxypoollib.ProxyState) proxypoollib.ProxyState {
	if state.Metadata == nil {
		return state
	}
	metadata := make(map[string]string, len(state.Metadata))
	for key, value := range state.Metadata {
		metadata[key] = value
	}
	state.Metadata = metadata
	return state
}
