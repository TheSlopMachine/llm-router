package repository_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"gorm.io/gorm/clause"
)

func openRepository(t *testing.T) *repository.GormRepository[record.ProxyState] {
	t.Helper()
	directory := t.TempDir()
	database, err := db.OpenSQLite(context.Background(), filepath.Join(directory, "repository.sqlite3"), filepath.Join(directory, "repository.cache.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	return repository.NewGormRepository[record.ProxyState](database.Cache, "url")
}

func buildState(url string) record.ProxyState {
	now := time.Now().UTC()
	return record.ProxyState{
		URL: url, IP: "10.0.0.1", Port: 8080, Source: "test",
		LastSeenInSource: now, ReviveAt: now, LastCheckedAt: now,
	}
}

func TestUpsertGetListCountDelete(t *testing.T) {
	ctx := context.Background()
	store := openRepository(t)
	if err := store.Upsert(ctx, buildState("http://10.0.0.1:8080")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	row, err := store.Get(ctx, clause.Eq{Column: "url", Value: "http://10.0.0.1:8080"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.IP != "10.0.0.1" {
		t.Errorf("ip is %q, want 10.0.0.1", row.IP)
	}
	updated := buildState("http://10.0.0.1:8080")
	updated.Score = 7.5
	if err := store.Upsert(ctx, updated); err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	row, err = store.Get(ctx, clause.Eq{Column: "url", Value: "http://10.0.0.1:8080"})
	if err != nil {
		t.Fatalf("get updated: %v", err)
	}
	if row.Score != 7.5 {
		t.Errorf("score is %v, want 7.5", row.Score)
	}
	rows, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("list holds %d rows, want 1", len(rows))
	}
	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("count is %d, want 1", count)
	}
	affected, err := store.Delete(ctx, clause.Eq{Column: "url", Value: "http://10.0.0.1:8080"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if affected != 1 {
		t.Errorf("delete affected %d, want 1", affected)
	}
	if _, err := store.Get(ctx, clause.Eq{Column: "url", Value: "http://10.0.0.1:8080"}); err == nil {
		t.Errorf("get after delete succeeded, want not-found")
	}
}

func TestCheckConstraintRejectsBadFailReason(t *testing.T) {
	ctx := context.Background()
	store := openRepository(t)
	state := buildState("http://10.0.0.2:8080")
	state.FailReason = "not_a_reason"
	if err := store.Upsert(ctx, state); err == nil {
		t.Errorf("upsert with bad fail_reason succeeded, want check violation")
	}
}
