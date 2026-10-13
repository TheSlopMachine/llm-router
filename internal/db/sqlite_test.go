package db_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func openTemp(t *testing.T) (*db.Database, string, string, string) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "foundation.sqlite3")
	cachePath := filepath.Join(directory, "foundation.cache.sqlite3")
	eventsPath := filepath.Join(directory, "foundation.events.sqlite3")
	database, err := db.OpenSQLite(context.Background(), path, cachePath, eventsPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	return database, path, cachePath, eventsPath
}

func TestOpenCreatesSchema(t *testing.T) {
	database, _, _, _ := openTemp(t)
	primaryTables := []string{
		"schema_migrations",
		"instance_settings",
		"admin_users",
		"router_tokens",
		"token_allowed_providers",
		"token_allowed_models",
		"token_allowed_credentials",
		"token_credential_scopes",
		"token_scope_credentials",
		"proxy_pools",
		"provider_instances",
		"credentials",
		"model_overrides",
		"model_override_capabilities",
		"model_override_modalities",
		"virtual_models",
		"virtual_model_members",
		"proxy_pool_entries",
		"plugin_repositories",
		"plugins",
		"plugin_allow_hosts",
		"plugin_versions",
		"plugin_storage",
	}
	primaryMigrator := database.Handle.Migrator()
	for _, table := range primaryTables {
		if !primaryMigrator.HasTable(table) {
			t.Errorf("primary missing table %s", table)
		}
	}
	if len(primaryTables) != 23 {
		t.Errorf("primary schema holds %d tables, want 23", len(primaryTables))
	}
	cacheTables := []string{
		"schema_migrations",
		"sessions",
		"credential_parks",
		"credential_health",
		"model_info_snapshots",
		"model_infos",
		"model_info_capabilities",
		"model_info_modalities",
		"model_info_endpoints",
		"proxy_states",
		"plugin_storage",
		"video_jobs",
		"response_records",
		"message_batches",
		"message_batch_entries",
		"message_batch_results",
		"metric_buckets",
		"metric_bucket_providers",
		"metric_bucket_models",
		"metric_bucket_tokens",
		"metric_bucket_error_types",
	}
	cacheMigrator := database.Cache.Migrator()
	for _, table := range cacheTables {
		if !cacheMigrator.HasTable(table) {
			t.Errorf("cache missing table %s", table)
		}
	}
	if len(cacheTables) != 21 {
		t.Errorf("cache schema holds %d tables, want 21", len(cacheTables))
	}
	eventsMigrator := database.Events.Migrator()
	for _, table := range []string{"schema_migrations", "event_records", "event_keys"} {
		if !eventsMigrator.HasTable(table) {
			t.Errorf("events missing table %s", table)
		}
	}
}

func TestReopenIsNoOp(t *testing.T) {
	_, path, cachePath, eventsPath := openTemp(t)
	second, err := db.OpenSQLite(context.Background(), path, cachePath, eventsPath)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	second.Close()
}

func TestDSNCarriesPragmas(t *testing.T) {
	dsn := db.BuildSQLiteDSN("example.sqlite3")
	for _, fragment := range []string{"foreign_keys(1)", "journal_mode(WAL)", "synchronous(NORMAL)", "busy_timeout(5000)", "_txlock=immediate", "_time_format=sqlite"} {
		if !strings.Contains(dsn, fragment) {
			t.Errorf("dsn misses %q: %s", fragment, dsn)
		}
	}
}

func TestMaintenanceRoundTrip(t *testing.T) {
	database, _, _, _ := openTemp(t)
	ctx := context.Background()
	for _, handle := range []*gorm.DB{database.Handle, database.Cache, database.Events} {
		if err := db.Maintenance(ctx, handle); err != nil {
			t.Fatalf("maintenance: %v", err)
		}
		if err := db.CheckIntegrity(ctx, handle); err != nil {
			t.Fatalf("integrity: %v", err)
		}
	}
	backup := filepath.Join(t.TempDir(), "backup.sqlite3")
	if err := db.BackupTo(ctx, database.Handle, backup); err != nil {
		t.Fatalf("backup: %v", err)
	}
	info, err := os.Stat(backup)
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("backup is empty")
	}
}

func TestCacheDeleteRecovers(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	path := filepath.Join(directory, "recovery.sqlite3")
	cachePath := filepath.Join(directory, "recovery.cache.sqlite3")
	eventsPath := filepath.Join(directory, "recovery.events.sqlite3")
	first, err := db.OpenSQLite(ctx, path, cachePath, eventsPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	now := time.Now().UTC()
	session := record.Session{TokenHash: "doomed", Username: "operator", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := gorm.G[record.Session](first.Cache).Create(ctx, &session); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
	if err := os.Remove(cachePath); err != nil {
		t.Fatalf("delete cache: %v", err)
	}
	second, err := db.OpenSQLite(ctx, path, cachePath, eventsPath)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	t.Cleanup(func() {
		second.Close()
	})
	if !second.Cache.Migrator().HasTable("sessions") {
		t.Errorf("recreated cache misses sessions")
	}
	if !second.Handle.Migrator().HasTable("provider_instances") {
		t.Errorf("primary misses provider_instances after cache delete")
	}
	fresh := record.Session{TokenHash: "fresh", Username: "operator", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := gorm.G[record.Session](second.Cache).Create(ctx, &fresh); err != nil {
		t.Fatalf("write session after cache delete: %v", err)
	}
}

func TestCloseVacuumsAllFiles(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	path := filepath.Join(directory, "vacuum.sqlite3")
	cachePath := filepath.Join(directory, "vacuum.cache.sqlite3")
	eventsPath := filepath.Join(directory, "vacuum.events.sqlite3")
	first, err := db.OpenSQLite(ctx, path, cachePath, eventsPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	now := time.Now().UTC()
	rows := make([]record.EventRecord, 0, 2000)
	for i := 0; i < 2000; i++ {
		rows = append(rows, record.EventRecord{Timestamp: now, Severity: 1, Message: "filler row for vacuum with payload padding to span pages"})
	}
	if err := gorm.G[record.EventRecord](first.Events).CreateInBatches(ctx, &rows, 500); err != nil {
		t.Fatalf("seed events: %v", err)
	}
	if _, err := gorm.G[record.EventRecord](first.Events, clause.Gte{Column: "id", Value: 0}).Delete(ctx); err != nil {
		t.Fatalf("delete events: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
	second, err := db.OpenSQLite(ctx, path, cachePath, eventsPath)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	t.Cleanup(func() {
		second.Close()
	})
	for name, handle := range map[string]*gorm.DB{"primary": second.Handle, "cache": second.Cache, "events": second.Events} {
		count, err := db.FreelistCount(ctx, handle)
		if err != nil {
			t.Fatalf("freelist %s: %v", name, err)
		}
		if count != 0 {
			t.Errorf("%s freelist holds %d pages after close, want 0 vacuumed", name, count)
		}
	}
}
