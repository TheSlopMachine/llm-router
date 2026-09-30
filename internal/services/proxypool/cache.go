package proxypool

import (
	"errors"
	"fmt"
	"sync"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	proxypoollib "github.com/TheSlopMachine/proxypool"
	bolt "go.etcd.io/bbolt"
)

type dbCache struct {
	mu    sync.RWMutex
	db    *db.DB
	repo  *repository.Repository[proxypoollib.ProxyState]
	items map[string]proxypoollib.ProxyState
	err   error
}

func newDBCache(database *db.DB) (*dbCache, error) {
	c := &dbCache{
		db:    database,
		repo:  repository.New[proxypoollib.ProxyState](database, db.BucketProxyCache, "proxy cache"),
		items: map[string]proxypoollib.ProxyState{},
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
	if err := c.repo.Put(state.URL, &state); err != nil {
		c.remember(err)
		return
	}
	c.mu.Lock()
	c.items[state.URL] = state
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
