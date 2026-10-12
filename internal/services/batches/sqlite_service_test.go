package batches

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	"gorm.io/gorm/clause"
)

func buildSQLiteBatch(id string) *models.BatchRecord {
	return &models.BatchRecord{
		ID:     id,
		Model:  "mock/m",
		Status: StatusInProgress,
		Entries: []models.BatchEntry{
			{CustomID: "first", Params: json.RawMessage(`{"model":"mock/m","max_tokens":1}`)},
			{CustomID: "second", Params: json.RawMessage(`{"model":"mock/m","max_tokens":2}`)},
		},
		Results: []models.BatchResultLine{
			{CustomID: "first", Result: json.RawMessage(`{"type":"succeeded"}`)},
		},
	}
}

func countSQLiteChildren(t *testing.T, service *SQLiteService, id string) (int, int) {
	t.Helper()
	ctx := context.Background()
	entries := repository.NewGormRepository[record.MessageBatchEntry](service.database.Cache, "batch_id", "position")
	entryRows, err := entries.List(ctx, clause.Eq{Column: "batch_id", Value: id})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	results := repository.NewGormRepository[record.MessageBatchResult](service.database.Cache, "batch_id", "position")
	resultRows, err := results.List(ctx, clause.Eq{Column: "batch_id", Value: id})
	if err != nil {
		t.Fatalf("list results: %v", err)
	}
	return len(entryRows), len(resultRows)
}

func TestSQLite_CRUDRoundTrip(t *testing.T) {
	service := NewSQLiteService(testutil.SetupTestSQLite(t))
	ctx := context.Background()
	batch := buildSQLiteBatch("batch_1")
	if err := service.Create(ctx, batch); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := service.Get(ctx, "batch_1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Model != "mock/m" || got.Status != StatusInProgress {
		t.Fatalf("header: %+v", got)
	}
	if len(got.Entries) != 2 || got.Entries[0].CustomID != "first" || got.Entries[1].CustomID != "second" {
		t.Fatalf("entries order: %+v", got.Entries)
	}
	if string(got.Entries[1].Params) != `{"model":"mock/m","max_tokens":2}` {
		t.Fatalf("entry params: %s", got.Entries[1].Params)
	}
	if len(got.Results) != 1 || string(got.Results[0].Result) != `{"type":"succeeded"}` {
		t.Fatalf("results: %+v", got.Results)
	}
	if _, err := service.Get(ctx, "missing"); !errors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("missing returns %v, want not found", err)
	}
}

func TestSQLite_UpdateReplacesChildren(t *testing.T) {
	service := NewSQLiteService(testutil.SetupTestSQLite(t))
	ctx := context.Background()
	if err := service.Create(ctx, buildSQLiteBatch("batch_1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	updated := &models.BatchRecord{
		ID:     "batch_1",
		Model:  "mock/m",
		Status: StatusEnded,
		Entries: []models.BatchEntry{
			{CustomID: "only", Params: json.RawMessage(`{"model":"mock/m","max_tokens":3}`)},
		},
	}
	if err := service.Update(ctx, updated); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := service.Get(ctx, "batch_1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusEnded {
		t.Fatalf("status: %q", got.Status)
	}
	if len(got.Entries) != 1 || got.Entries[0].CustomID != "only" {
		t.Fatalf("entries after update: %+v", got.Entries)
	}
	if len(got.Results) != 0 {
		t.Fatalf("results after update: %+v", got.Results)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.Before(got.CreatedAt) {
		t.Fatalf("timestamps: created %v updated %v", got.CreatedAt, got.UpdatedAt)
	}
	if err := service.Update(ctx, &models.BatchRecord{ID: "missing", Status: StatusEnded}); !errors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("update missing returns %v, want not found", err)
	}
}

func TestSQLite_DeleteCascadesChildren(t *testing.T) {
	service := NewSQLiteService(testutil.SetupTestSQLite(t))
	ctx := context.Background()
	if err := service.Create(ctx, buildSQLiteBatch("batch_1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := service.Delete(ctx, "batch_1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := service.Get(ctx, "batch_1"); !errors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("get after delete returns %v, want not found", err)
	}
	if entries, results := countSQLiteChildren(t, service, "batch_1"); entries != 0 || results != 0 {
		t.Fatalf("children after delete: %d entries %d results, want 0 0", entries, results)
	}
	if err := service.Delete(ctx, "missing"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestSQLite_ListOldestFirst(t *testing.T) {
	service := NewSQLiteService(testutil.SetupTestSQLite(t))
	ctx := context.Background()
	headers := repository.NewGormRepository[record.MessageBatch](service.database.Cache, "id")
	if err := service.Create(ctx, buildSQLiteBatch("batch_new")); err != nil {
		t.Fatalf("create new: %v", err)
	}
	stored, err := headers.Get(ctx, clause.Eq{Column: "id", Value: "batch_new"})
	if err != nil {
		t.Fatalf("get header: %v", err)
	}
	older := toBatchHeader(buildSQLiteBatch("batch_old"))
	older.CreatedAt = stored.CreatedAt.Add(-time.Hour)
	older.UpdatedAt = stored.CreatedAt.Add(-time.Hour)
	if err := headers.Upsert(ctx, older); err != nil {
		t.Fatalf("seed old: %v", err)
	}
	list, err := service.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list holds %d batches, want 2", len(list))
	}
	if list[0].ID != "batch_old" || list[1].ID != "batch_new" {
		t.Fatalf("list order: %q %q, want batch_old batch_new", list[0].ID, list[1].ID)
	}
	if len(list[1].Entries) != 2 || len(list[1].Results) != 1 {
		t.Fatalf("list hydrates children: %+v", list[1])
	}
}

func TestSQLite_Validation(t *testing.T) {
	service := NewSQLiteService(testutil.SetupTestSQLite(t))
	ctx := context.Background()
	if err := service.Create(ctx, nil); err == nil {
		t.Fatal("nil create must fail")
	}
	if err := service.Create(ctx, &models.BatchRecord{}); err == nil {
		t.Fatal("empty id create must fail")
	}
	if err := service.Update(ctx, nil); err == nil {
		t.Fatal("nil update must fail")
	}
	if err := service.Update(ctx, &models.BatchRecord{}); err == nil {
		t.Fatal("empty id update must fail")
	}
}
