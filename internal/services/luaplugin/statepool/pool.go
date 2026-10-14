// Package statepool holds idle Lua states in per-key buckets.
//
// Buckets grow on demand. Acquire pops an idle state or builds one through
// the caller factory. Return stores the state again or closes it when the
// bucket already holds the configured maximum.
package statepool

import (
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// DefaultBucketSize caps idle states stored per bucket.
const DefaultBucketSize = 16

// Pool keeps idle Lua states keyed by bucket.
type Pool struct {
	mutex            sync.Mutex
	buckets          map[string][]*lua.LState
	outstanding      map[*lua.LState]bool
	stored           map[*lua.LState]bool
	maximumPerBucket int
}

// NewPool builds an empty pool holding at most maximumPerBucket idle states
// per bucket. Negative values disable storage.
func NewPool(maximumPerBucket int) *Pool {
	if maximumPerBucket < 0 {
		maximumPerBucket = 0
	}
	return &Pool{
		buckets:          map[string][]*lua.LState{},
		outstanding:      map[*lua.LState]bool{},
		stored:           map[*lua.LState]bool{},
		maximumPerBucket: maximumPerBucket,
	}
}

// Acquire pops an idle state for bucketKey or builds one with createState.
// The factory runs without holding the pool lock. A nil factory on a miss
// returns nil. An empty bucket key bypasses storage and always builds.
func (pool *Pool) Acquire(bucketKey string, createState func() *lua.LState) *lua.LState {
	if pool == nil {
		if createState == nil {
			return nil
		}
		return createState()
	}
	if bucketKey == "" {
		if createState == nil {
			return nil
		}
		return createState()
	}
	pool.mutex.Lock()
	stored := pool.buckets[bucketKey]
	if len(stored) > 0 {
		state := stored[len(stored)-1]
		rest := stored[:len(stored)-1]
		if len(rest) == 0 {
			delete(pool.buckets, bucketKey)
		} else {
			pool.buckets[bucketKey] = rest
		}
		delete(pool.stored, state)
		if state == nil || state.IsClosed() {
			pool.mutex.Unlock()
			if createState == nil {
				return nil
			}
			fresh := createState()
			if fresh == nil {
				return nil
			}
			pool.mutex.Lock()
			pool.outstanding[fresh] = true
			pool.mutex.Unlock()
			return fresh
		}
		pool.outstanding[state] = true
		pool.mutex.Unlock()
		return state
	}
	pool.mutex.Unlock()
	if createState == nil {
		return nil
	}
	fresh := createState()
	if fresh == nil {
		return nil
	}
	pool.mutex.Lock()
	pool.outstanding[fresh] = true
	pool.mutex.Unlock()
	return fresh
}

// Return stores state for bucketKey or closes it when the bucket is full.
// Nil states are ignored. Unknown pointers are closed, never stored, so a
// duplicate return cannot hand one state to two callers. Closed states are
// forgotten without a second close.
func (pool *Pool) Return(bucketKey string, state *lua.LState) {
	if pool == nil || state == nil {
		return
	}
	pool.mutex.Lock()
	if state.IsClosed() {
		delete(pool.outstanding, state)
		delete(pool.stored, state)
		pool.mutex.Unlock()
		return
	}
	if !pool.outstanding[state] {
		if pool.stored[state] {
			pool.mutex.Unlock()
			return
		}
		pool.mutex.Unlock()
		state.Close()
		return
	}
	delete(pool.outstanding, state)
	if bucketKey == "" || pool.maximumPerBucket <= 0 {
		pool.mutex.Unlock()
		state.Close()
		return
	}
	stored := pool.buckets[bucketKey]
	if len(stored) >= pool.maximumPerBucket {
		pool.mutex.Unlock()
		state.Close()
		return
	}
	pool.buckets[bucketKey] = append(stored, state)
	pool.stored[state] = true
	pool.mutex.Unlock()
}

// Invalidate closes every idle state for bucketKey and drops the bucket.
// States checked out before the call stay checked out; they close or store
// on Return under the normal limit rules.
func (pool *Pool) Invalidate(bucketKey string) {
	if pool == nil || bucketKey == "" {
		return
	}
	pool.mutex.Lock()
	stored := pool.buckets[bucketKey]
	delete(pool.buckets, bucketKey)
	for _, state := range stored {
		delete(pool.stored, state)
	}
	pool.mutex.Unlock()
	for _, state := range stored {
		if state != nil && !state.IsClosed() {
			state.Close()
		}
	}
}

// InvalidateAll closes every idle state in every bucket.
func (pool *Pool) InvalidateAll() {
	if pool == nil {
		return
	}
	pool.mutex.Lock()
	stored := make([]*lua.LState, 0)
	for key, entries := range pool.buckets {
		stored = append(stored, entries...)
		delete(pool.buckets, key)
	}
	pool.stored = map[*lua.LState]bool{}
	pool.mutex.Unlock()
	for _, state := range stored {
		if state != nil && !state.IsClosed() {
			state.Close()
		}
	}
}

// SetBucketSize replaces the per-bucket idle limit. Negative disables
// storage. Shrinking closes the newest excess idle states immediately.
func (pool *Pool) SetBucketSize(size int) {
	if pool == nil {
		return
	}
	if size < 0 {
		size = 0
	}
	pool.mutex.Lock()
	pool.maximumPerBucket = size
	var excess []*lua.LState
	for key, entries := range pool.buckets {
		if len(entries) > size {
			excess = append(excess, entries[size:]...)
			for _, state := range entries[size:] {
				delete(pool.stored, state)
			}
			if size == 0 {
				delete(pool.buckets, key)
			} else {
				pool.buckets[key] = entries[:size]
			}
		}
	}
	pool.mutex.Unlock()
	for _, state := range excess {
		if state != nil && !state.IsClosed() {
			state.Close()
		}
	}
}

// BucketSize reports the per-bucket idle limit.
func (pool *Pool) BucketSize() int {
	if pool == nil {
		return 0
	}
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	return pool.maximumPerBucket
}

// Length reports idle states stored for bucketKey.
func (pool *Pool) Length(bucketKey string) int {
	if pool == nil || bucketKey == "" {
		return 0
	}
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	return len(pool.buckets[bucketKey])
}

var defaultPool = NewPool(DefaultBucketSize)

// Acquire pops an idle state for bucketKey from the shared pool or builds
// one with createState.
func Acquire(bucketKey string, createState func() *lua.LState) *lua.LState {
	return defaultPool.Acquire(bucketKey, createState)
}

// Return stores state for bucketKey in the shared pool or closes it when
// the bucket is full.
func Return(bucketKey string, state *lua.LState) {
	defaultPool.Return(bucketKey, state)
}

// Invalidate closes every idle state for bucketKey in the shared pool.
func Invalidate(bucketKey string) {
	defaultPool.Invalidate(bucketKey)
}

// InvalidateAll closes every idle state in the shared pool.
func InvalidateAll() {
	defaultPool.InvalidateAll()
}

// SetBucketSize replaces the shared pool per-bucket idle limit.
func SetBucketSize(size int) {
	defaultPool.SetBucketSize(size)
}

// BucketSize reports the shared pool per-bucket idle limit.
func BucketSize() int {
	return defaultPool.BucketSize()
}

// Length reports idle states the shared pool stores for bucketKey.
func Length(bucketKey string) int {
	return defaultPool.Length(bucketKey)
}
