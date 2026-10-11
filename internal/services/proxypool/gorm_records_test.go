package proxypool

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	proxypoollib "github.com/TheSlopMachine/proxypool"
)

func buildLibraryState(now time.Time) proxypoollib.ProxyState {
	return proxypoollib.ProxyState{
		URL:              "http://192.0.2.20:8080",
		IP:               "192.0.2.20",
		Port:             8080,
		Location:         "DE",
		IsDead:           true,
		Source:           "feed",
		FailReason:       proxypoollib.FailTimeout,
		SoftFails:        2,
		Suspect:          true,
		LastSeenInSource: now,
		Penalty:          1.5,
		Score:            7.25,
		ReviveAt:         now.Add(time.Minute),
		LastCheckedAt:    now,
		Latency:          150 * time.Millisecond,
		Metadata:         map[string]string{"zone": "test"},
	}
}

func checkStateRoundTrip(testing *testing.T, state proxypoollib.ProxyState) {
	testing.Helper()
	persisted := proxyStateToRecord(state)
	restored := recordToProxyState(persisted)
	if restored.URL != state.URL || restored.IP != state.IP || restored.Port != state.Port {
		testing.Fatalf("identity is %+v, want %+v", restored, state)
	}
	if restored.Location != state.Location || restored.IsDead != state.IsDead || restored.Source != state.Source {
		testing.Fatalf("attributes are %+v, want %+v", restored, state)
	}
	if restored.FailReason != state.FailReason || restored.SoftFails != state.SoftFails || restored.Suspect != state.Suspect {
		testing.Fatalf("health is %+v, want %+v", restored, state)
	}
	if !restored.LastSeenInSource.Equal(state.LastSeenInSource) || !restored.ReviveAt.Equal(state.ReviveAt) || !restored.LastCheckedAt.Equal(state.LastCheckedAt) {
		testing.Fatalf("timestamps are %+v, want %+v", restored, state)
	}
	if restored.Penalty != state.Penalty || restored.Score != state.Score || restored.Latency != state.Latency {
		testing.Fatalf("scoring is %+v, want %+v", restored, state)
	}
	if !reflect.DeepEqual(restored.Metadata, state.Metadata) {
		testing.Fatalf("metadata is %v, want %v", restored.Metadata, state.Metadata)
	}
}

func TestProxyStateRecordRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	checkStateRoundTrip(t, buildLibraryState(now))
}

func TestProxyStateRecordMetadataIsolated(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	state := buildLibraryState(now)
	persisted := proxyStateToRecord(state)
	state.Metadata["zone"] = "mutated"
	if persisted.Metadata["zone"] != "test" {
		t.Fatalf("record metadata is %q after source mutation, want test", persisted.Metadata["zone"])
	}
	restored := recordToProxyState(persisted)
	persisted.Metadata["zone"] = "mutated"
	if restored.Metadata["zone"] != "test" {
		t.Fatalf("restored metadata is %q after record mutation, want test", restored.Metadata["zone"])
	}
}

func TestCustomPoolRecordsRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	pool := models.CustomProxyPool{
		ID:   "office",
		Name: "Office",
		Entries: []models.ProxyEntry{
			{URL: "http://192.0.2.21:8080", Country: "DE"},
			{URL: "socks5://192.0.2.22:1080", Country: "US"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	persisted, entries := customPoolToRecords(pool)
	if persisted.ID != "office" || persisted.Name != "Office" || persisted.Builtin {
		t.Fatalf("pool row is %+v, want office/Office/non-builtin", persisted)
	}
	if len(entries) != 2 || entries[0].Position != 0 || entries[1].Position != 1 {
		t.Fatalf("entries are %+v, want positions 0 and 1", entries)
	}
	if entries[0].PoolID != "office" || entries[1].PoolID != "office" {
		t.Fatalf("entries are %+v, want pool office", entries)
	}
	restored := recordsToCustomPool(persisted, []record.ProxyPoolEntry{entries[1], entries[0]})
	if restored.ID != pool.ID || restored.Name != pool.Name {
		t.Fatalf("restored pool is %+v, want %+v", restored, pool)
	}
	if !reflect.DeepEqual(restored.Entries, pool.Entries) {
		t.Fatalf("restored entries are %+v, want %+v", restored.Entries, pool.Entries)
	}
	if !restored.CreatedAt.Equal(now) || !restored.UpdatedAt.Equal(now) {
		t.Fatalf("restored timestamps are %+v, want %v", restored, now)
	}
}

func TestGormCacheBuffersInMemory(t *testing.T) {
	ctx := context.Background()
	database := testutil.SetupTestSQLite(t)
	loaded, err := newGormCache(ctx, database.Handle)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	first := buildLibraryState(now)
	second := first
	second.URL = "http://192.0.2.23:8080"
	second.IP = "192.0.2.23"
	loaded.Set(first)
	loaded.Set(second)
	if got := len(loaded.All()); got != 2 {
		t.Fatalf("All holds %d states, want 2", got)
	}
	state, ok := loaded.Get(first.URL)
	if !ok || state.IP != first.IP {
		t.Fatalf("Get returned %+v, %v, want first state", state, ok)
	}
	loaded.Delete(first.URL)
	if _, ok := loaded.Get(first.URL); ok {
		t.Fatal("deleted proxy must leave the in-memory cache")
	}
	if err := loaded.Flush(); err != nil {
		t.Fatalf("stubbed flush: %v", err)
	}
	loaded.Clear()
	if got := len(loaded.All()); got != 0 {
		t.Fatalf("All holds %d states after clear, want 0", got)
	}
	if err := loaded.takeError(); err != nil {
		t.Fatalf("takeError: %v", err)
	}
}

func TestGormCacheLoadsPersistedStates(t *testing.T) {
	ctx := context.Background()
	database := testutil.SetupTestSQLite(t)
	now := time.Now().UTC().Truncate(time.Second)
	persisted := proxyStateToRecord(buildLibraryState(now))
	states := repository.NewGormRepository[record.ProxyState](database.Handle, "url")
	if err := states.Upsert(ctx, persisted); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	loaded, err := newGormCache(ctx, database.Handle)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	state, ok := loaded.Get(persisted.URL)
	if !ok {
		t.Fatalf("Get missed persisted %q", persisted.URL)
	}
	if state.FailReason != proxypoollib.FailTimeout || state.Latency != 150*time.Millisecond {
		t.Fatalf("loaded state is %+v, want timeout/150ms", state)
	}
	if !reflect.DeepEqual(state.Metadata, map[string]string{"zone": "test"}) {
		t.Fatalf("loaded metadata is %v, want zone=test", state.Metadata)
	}
}
