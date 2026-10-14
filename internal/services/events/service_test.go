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

func TestQueryKeyExcludes(t *testing.T) {
	service := openEvents(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := service.Append(ctx, now, SeverityInfo, "first", map[string]string{"plugin": "x", "provider": "openai"}); err != nil {
		t.Fatalf("append first: %v", err)
	}
	if err := service.Append(ctx, now, SeverityInfo, "second", map[string]string{"provider": "other"}); err != nil {
		t.Fatalf("append second: %v", err)
	}
	if err := service.Append(ctx, now, SeverityInfo, "third", nil); err != nil {
		t.Fatalf("append third: %v", err)
	}
	excluded, err := service.Query(ctx, Query{Excludes: []KeyFilter{{Key: "plugin"}}})
	if err != nil {
		t.Fatalf("exclusion query: %v", err)
	}
	if len(excluded) != 2 {
		t.Fatalf("unexpected exclusion entries %+v", excluded)
	}
	for _, entry := range excluded {
		if entry.Message == "first" {
			t.Fatalf("excluded entry still present %+v", excluded)
		}
	}
	value := "other"
	valued, err := service.Query(ctx, Query{Excludes: []KeyFilter{{Key: "provider", Value: &value}}})
	if err != nil {
		t.Fatalf("valued exclusion query: %v", err)
	}
	if len(valued) != 2 {
		t.Fatalf("unexpected valued exclusion entries %+v", valued)
	}
	for _, entry := range valued {
		if entry.Message == "second" {
			t.Fatalf("valued excluded entry still present %+v", valued)
		}
	}
	combined, err := service.Query(ctx, Query{Filters: []KeyFilter{{Key: "provider"}}, Excludes: []KeyFilter{{Key: "plugin"}}})
	if err != nil {
		t.Fatalf("combined query: %v", err)
	}
	if len(combined) != 1 || combined[0].Message != "second" {
		t.Fatalf("unexpected combined entries %+v", combined)
	}
	union, err := service.Query(ctx, Query{Excludes: []KeyFilter{{Key: "plugin"}, {Key: "provider", Value: &value}}})
	if err != nil {
		t.Fatalf("union query: %v", err)
	}
	if len(union) != 1 || union[0].Message != "third" {
		t.Fatalf("unexpected union entries %+v", union)
	}
	limited, err := service.Query(ctx, Query{Excludes: []KeyFilter{{Key: "plugin"}}, Limit: 1})
	if err != nil {
		t.Fatalf("limited query: %v", err)
	}
	if len(limited) != 1 || limited[0].Message != "third" {
		t.Fatalf("unexpected limited entries %+v", limited)
	}
}

func TestQueryExcludesPaginate(t *testing.T) {
	service := openEvents(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, message := range []string{"plain-1", "plain-2", "plain-3"} {
		if err := service.Append(ctx, now, SeverityInfo, message, nil); err != nil {
			t.Fatalf("append %s: %v", message, err)
		}
	}
	for i := 0; i < excludePageSize+20; i++ {
		if err := service.Append(ctx, now, SeverityInfo, "noise", map[string]string{"noise": "yes"}); err != nil {
			t.Fatalf("append noise %d: %v", i, err)
		}
	}
	entries, err := service.Query(ctx, Query{Excludes: []KeyFilter{{Key: "noise"}}, Limit: 10})
	if err != nil {
		t.Fatalf("paginated query: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("unexpected paginated entries %+v", entries)
	}
	for i, message := range []string{"plain-3", "plain-2", "plain-1"} {
		if entries[i].Message != message {
			t.Fatalf("entry %d holds %q, want %q", i, entries[i].Message, message)
		}
	}
}

func TestQueryIncludesChunked(t *testing.T) {
	service := openEvents(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < idChunkSize+2; i++ {
		keys := map[string]string{"bulk": "yes"}
		if i > 0 {
			keys["drop"] = "yes"
		}
		if err := service.Append(ctx, now, SeverityInfo, "bulk", keys); err != nil {
			t.Fatalf("append bulk %d: %v", i, err)
		}
	}
	value := "yes"
	entries, err := service.Query(ctx, Query{Filters: []KeyFilter{{Key: "bulk", Value: &value}}, Excludes: []KeyFilter{{Key: "drop"}}, Limit: 10})
	if err != nil {
		t.Fatalf("chunked query: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("unexpected chunked entries %+v", entries)
	}
	if len(entries[0].Keys) != 1 || entries[0].Keys[0].Key != "bulk" {
		t.Fatalf("unexpected chunked keys %+v", entries[0].Keys)
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
