package events

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
)

func openEvents(t *testing.T) *Service {
	t.Helper()
	directory := t.TempDir()
	database, err := db.OpenSQLite(
		context.Background(),
		filepath.Join(directory, "test.sqlite3"),
		filepath.Join(directory, "test.cache.sqlite3"),
		filepath.Join(directory, "test.events.sqlite3"),
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	service, err := New(database, 3)
	if err != nil {
		t.Fatalf("new events service: %v", err)
	}
	return service
}

func TestAppendAndQueryParameterless(t *testing.T) {
	service := openEvents(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if err := service.Append(ctx, now, SeverityInfo, "hello", map[string]string{"provider": "openai"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	entries, err := service.Query(ctx, Query{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries hold %d rows, want 1", len(entries))
	}
	if entries[0].Message != "hello" || entries[0].Severity != SeverityInfo {
		t.Errorf("unexpected entry %+v", entries[0])
	}
	if len(entries[0].Keys) != 1 || entries[0].Keys[0].Key != "provider" {
		t.Errorf("unexpected keys %+v", entries[0].Keys)
	}
}

func TestQuerySeverityAndTimeRange(t *testing.T) {
	service := openEvents(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if err := service.Append(ctx, base, SeverityInfo, "info", nil); err != nil {
		t.Fatalf("append info: %v", err)
	}
	if err := service.Append(ctx, base, SeverityWarn, "warn", nil); err != nil {
		t.Fatalf("append warn: %v", err)
	}
	severity := SeverityWarn
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 1, 23, 59, 59, 0, time.UTC)
	entries, err := service.Query(ctx, Query{Severity: &severity, Start: &start, End: &end})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 1 || entries[0].Message != "warn" {
		t.Fatalf("unexpected entries %+v", entries)
	}
}

func TestQueryKeyFilters(t *testing.T) {
	service := openEvents(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := service.Append(ctx, now, SeverityInfo, "first", map[string]string{"plugin": "x", "provider": "openai"}); err != nil {
		t.Fatalf("append first: %v", err)
	}
	if err := service.Append(ctx, now, SeverityInfo, "second", map[string]string{"provider": "other"}); err != nil {
		t.Fatalf("append second: %v", err)
	}
	existence, err := service.Query(ctx, Query{Filters: []KeyFilter{{Key: "plugin"}}})
	if err != nil {
		t.Fatalf("existence query: %v", err)
	}
	if len(existence) != 1 || existence[0].Message != "first" {
		t.Fatalf("unexpected existence entries %+v", existence)
	}
	value := "openai"
	matched, err := service.Query(ctx, Query{Filters: []KeyFilter{{Key: "plugin"}, {Key: "provider", Value: &value}}})
	if err != nil {
		t.Fatalf("match query: %v", err)
	}
	if len(matched) != 1 || matched[0].Message != "first" {
		t.Fatalf("unexpected match entries %+v", matched)
	}
}

func TestQueryByID(t *testing.T) {
	service := openEvents(t)
	ctx := context.Background()
	if err := service.Append(ctx, time.Now().UTC(), SeverityInfo, "only", nil); err != nil {
		t.Fatalf("append: %v", err)
	}
	all, err := service.Query(ctx, Query{})
	if err != nil || len(all) != 1 {
		t.Fatalf("list: %+v %v", all, err)
	}
	entries, err := service.Query(ctx, Query{ID: &all[0].ID})
	if err != nil {
		t.Fatalf("query by id: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != all[0].ID {
		t.Fatalf("unexpected entries %+v", entries)
	}
	missing := int64(999999)
	entries, err = service.Query(ctx, Query{ID: &missing})
	if err != nil {
		t.Fatalf("query missing: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("missing id holds %d rows, want 0", len(entries))
	}
}

func TestPruneDeletesOldRows(t *testing.T) {
	service := openEvents(t)
	ctx := context.Background()
	old := time.Now().UTC().Add(-96 * time.Hour)
	fresh := time.Now().UTC()
	if err := service.Append(ctx, old, SeverityInfo, "old", nil); err != nil {
		t.Fatalf("append old: %v", err)
	}
	if err := service.Append(ctx, fresh, SeverityInfo, "fresh", nil); err != nil {
		t.Fatalf("append fresh: %v", err)
	}
	if err := service.Prune(ctx); err != nil {
		t.Fatalf("prune: %v", err)
	}
	entries, err := service.Query(ctx, Query{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 1 || entries[0].Message != "fresh" {
		t.Fatalf("unexpected entries after prune %+v", entries)
	}
}
