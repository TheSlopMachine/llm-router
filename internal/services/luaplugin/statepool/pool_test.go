package statepool

import (
	"sync"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func newBareState() *lua.LState {
	return lua.NewState()
}

func TestAcquireCallsFactoryOnMiss(t *testing.T) {
	pool := NewPool(DefaultBucketSize)
	calls := 0
	state := pool.Acquire("alpha", func() *lua.LState {
		calls++
		return newBareState()
	})
	if state == nil {
		t.Fatal("Acquire on miss returned nil")
	}
	defer state.Close()
	if calls != 1 {
		t.Fatalf("factory calls = %d, want 1", calls)
	}
}

func TestAcquireReusesReturnedState(t *testing.T) {
	pool := NewPool(DefaultBucketSize)
	firstState := pool.Acquire("alpha", newBareState)
	pool.Return("alpha", firstState)
	calls := 0
	secondState := pool.Acquire("alpha", func() *lua.LState {
		calls++
		return newBareState()
	})
	defer secondState.Close()
	if calls != 0 {
		t.Fatalf("hit called factory %d times, want 0", calls)
	}
	if secondState != firstState {
		t.Fatal("hit did not return the stored state")
	}
}

func TestBucketsIsolateKeys(t *testing.T) {
	pool := NewPool(DefaultBucketSize)
	firstState := pool.Acquire("alpha", newBareState)
	pool.Return("alpha", firstState)
	secondState := pool.Acquire("beta", newBareState)
	if secondState == firstState {
		t.Fatal("distinct keys shared one state")
	}
	if pool.Length("alpha") != 1 {
		t.Fatalf("alpha length = %d, want 1 while beta is checked out", pool.Length("alpha"))
	}
	pool.Return("beta", secondState)
	if pool.Length("alpha") != 1 || pool.Length("beta") != 1 {
		t.Fatal("buckets did not isolate entries")
	}
	pool.InvalidateAll()
}

func TestReturnDiscardsBeyondLimit(t *testing.T) {
	pool := NewPool(2)
	firstState := pool.Acquire("alpha", newBareState)
	secondState := pool.Acquire("alpha", newBareState)
	thirdState := pool.Acquire("alpha", newBareState)
	pool.Return("alpha", firstState)
	pool.Return("alpha", secondState)
	pool.Return("alpha", thirdState)
	if got := pool.Length("alpha"); got != 2 {
		t.Fatalf("length = %d, want 2", got)
	}
	// LIFO: last stored pops first. The third return exceeded the limit,
	// so the next two pops return the second and first states.
	popped := pool.Acquire("alpha", newBareState)
	if popped != secondState {
		t.Fatal("expected second state back first")
	}
	next := pool.Acquire("alpha", newBareState)
	if next != firstState {
		t.Fatal("expected first state back second")
	}
	popped.Close()
	next.Close()
	if !thirdState.IsClosed() {
		thirdState.Close()
		t.Fatal("excess state was not closed on Return")
	}
}

func TestReturnNilIgnored(t *testing.T) {
	pool := NewPool(DefaultBucketSize)
	pool.Return("alpha", nil)
	if got := pool.Length("alpha"); got != 0 {
		t.Fatalf("length = %d, want 0", got)
	}
}

func TestAcquireEmptyKeyBypassesStorage(t *testing.T) {
	pool := NewPool(DefaultBucketSize)
	firstState := pool.Acquire("", newBareState)
	secondState := pool.Acquire("", newBareState)
	if firstState == nil || secondState == nil {
		t.Fatal("empty key Acquire returned nil")
	}
	if firstState == secondState {
		t.Fatal("empty key reused a state, want fresh builds")
	}
	pool.Return("", firstState)
	pool.Return("", secondState)
	if !firstState.IsClosed() || !secondState.IsClosed() {
		t.Fatal("empty key Return did not close")
	}
	if got := pool.Length(""); got != 0 {
		t.Fatalf("length = %d, want 0", got)
	}
}

func TestAcquireNilFactoryReturnsNilOnMiss(t *testing.T) {
	pool := NewPool(DefaultBucketSize)
	if state := pool.Acquire("alpha", nil); state != nil {
		state.Close()
		t.Fatal("nil factory on miss returned a state")
	}
}

func TestFactoryNilResultReturnsNil(t *testing.T) {
	pool := NewPool(DefaultBucketSize)
	if state := pool.Acquire("alpha", func() *lua.LState { return nil }); state != nil {
		t.Fatal("nil factory result returned non-nil")
	}
	if got := pool.Length("alpha"); got != 0 {
		t.Fatalf("length = %d, want 0", got)
	}
}

func TestDuplicateReturnStoresOnce(t *testing.T) {
	pool := NewPool(DefaultBucketSize)
	state := pool.Acquire("alpha", newBareState)
	pool.Return("alpha", state)
	pool.Return("alpha", state)
	if got := pool.Length("alpha"); got != 1 {
		t.Fatalf("length = %d, want 1", got)
	}
	popped := pool.Acquire("alpha", func() *lua.LState {
		t.Error("duplicate return closed the stored state")
		return newBareState()
	})
	defer popped.Close()
	if popped != state {
		t.Fatal("stored state did not survive duplicate return")
	}
}

func TestForeignReturnCloses(t *testing.T) {
	pool := NewPool(DefaultBucketSize)
	foreign := newBareState()
	pool.Return("alpha", foreign)
	if !foreign.IsClosed() {
		foreign.Close()
		t.Fatal("foreign state was not closed")
	}
	if got := pool.Length("alpha"); got != 0 {
		t.Fatalf("length = %d, want 0", got)
	}
}

func TestInvalidateDrainsBucket(t *testing.T) {
	pool := NewPool(DefaultBucketSize)
	firstState := pool.Acquire("alpha", newBareState)
	pool.Return("alpha", firstState)
	secondState := pool.Acquire("beta", newBareState)
	pool.Return("beta", secondState)
	pool.Invalidate("alpha")
	if got := pool.Length("alpha"); got != 0 {
		t.Fatalf("alpha length = %d, want 0", got)
	}
	if got := pool.Length("beta"); got != 1 {
		t.Fatalf("beta length = %d, want 1", got)
	}
	calls := 0
	fresh := pool.Acquire("alpha", func() *lua.LState {
		calls++
		return newBareState()
	})
	defer fresh.Close()
	if calls != 1 {
		t.Fatal("Invalidate did not force a factory rebuild")
	}
	pool.InvalidateAll()
}

func TestInvalidateUnknownKeyNoop(t *testing.T) {
	pool := NewPool(DefaultBucketSize)
	pool.Invalidate("missing")
	if got := pool.Length("missing"); got != 0 {
		t.Fatalf("length = %d, want 0", got)
	}
}

func TestInvalidateAllDrainsEverything(t *testing.T) {
	pool := NewPool(DefaultBucketSize)
	for _, bucketKey := range []string{"alpha", "beta"} {
		state := pool.Acquire(bucketKey, newBareState)
		pool.Return(bucketKey, state)
	}
	pool.InvalidateAll()
	if pool.Length("alpha") != 0 || pool.Length("beta") != 0 {
		t.Fatal("InvalidateAll left entries behind")
	}
}

func TestSetBucketSizeShrinkTrimsIdle(t *testing.T) {
	pool := NewPool(4)
	var stored []*lua.LState
	for range 4 {
		stored = append(stored, pool.Acquire("alpha", newBareState))
	}
	for _, state := range stored {
		pool.Return("alpha", state)
	}
	pool.SetBucketSize(2)
	if got := pool.Length("alpha"); got != 2 {
		t.Fatalf("length = %d, want 2 after shrink", got)
	}
	pool.InvalidateAll()
}

func TestSetBucketSizeNegativeDisables(t *testing.T) {
	pool := NewPool(DefaultBucketSize)
	pool.SetBucketSize(-1)
	if got := pool.BucketSize(); got != 0 {
		t.Fatalf("size = %d, want 0", got)
	}
	state := pool.Acquire("alpha", newBareState)
	pool.Return("alpha", state)
	if !state.IsClosed() {
		state.Close()
		t.Fatal("disabled pool stored a state")
	}
}

func TestConcurrentAcquireReturnHammer(t *testing.T) {
	pool := NewPool(16)
	keys := []string{"alpha", "beta", "gamma", "delta"}
	var group sync.WaitGroup
	for worker := range 16 {
		_ = worker
		group.Add(1)
		go func() {
			defer group.Done()
			for iteration := range 200 {
				bucketKey := keys[iteration%len(keys)]
				state := pool.Acquire(bucketKey, newBareState)
				if state == nil {
					t.Error("Acquire returned nil")
					return
				}
				state.Push(lua.LNumber(1))
				state.Pop(1)
				pool.Return(bucketKey, state)
			}
		}()
	}
	group.Wait()
	for _, bucketKey := range keys {
		if got := pool.Length(bucketKey); got > 16 {
			t.Fatalf("%s length = %d, want at most 16", bucketKey, got)
		}
	}
	pool.InvalidateAll()
}

func TestConcurrentInvalidateDuringUse(t *testing.T) {
	pool := NewPool(16)
	done := make(chan struct{})
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		for iteration := range 200 {
			state := pool.Acquire("alpha", newBareState)
			if state == nil {
				t.Error("Acquire returned nil")
				return
			}
			_ = iteration
			pool.Return("alpha", state)
		}
		close(done)
	}()
	for range 50 {
		pool.Invalidate("alpha")
		pool.SetBucketSize(16)
	}
	<-done
	group.Wait()
	if got := pool.Length("alpha"); got > 16 {
		t.Fatalf("length = %d, want at most 16", got)
	}
	pool.InvalidateAll()
}

func TestSharedPoolFunctions(t *testing.T) {
	t.Cleanup(func() {
		InvalidateAll()
		SetBucketSize(DefaultBucketSize)
	})
	SetBucketSize(4)
	if got := BucketSize(); got != 4 {
		t.Fatalf("size = %d, want 4", got)
	}
	firstState := Acquire("shared-alpha", newBareState)
	Return("shared-alpha", firstState)
	if got := Length("shared-alpha"); got != 1 {
		t.Fatalf("length = %d, want 1", got)
	}
	secondState := Acquire("shared-alpha", func() *lua.LState {
		t.Error("shared hit called factory")
		return newBareState()
	})
	if secondState != firstState {
		t.Fatal("shared hit did not reuse the stored state")
	}
	Return("shared-alpha", secondState)
	Invalidate("shared-alpha")
	if got := Length("shared-alpha"); got != 0 {
		t.Fatalf("length = %d, want 0 after Invalidate", got)
	}
}
