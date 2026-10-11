package db_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/db"
)

func openTemp(t *testing.T) (*db.Database, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "foundation.sqlite3")
	database, err := db.OpenSQLite(context.Background(), path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	return database, path
}

func TestOpenCreatesSchema(t *testing.T) {
	database, _ := openTemp(t)
	tables := []string{
		"schema_migrations",
		"instance_settings",
		"admin_users",
		"sessions",
		"router_tokens",
		"token_allowed_providers",
		"token_allowed_models",
		"token_allowed_credentials",
		"token_credential_scopes",
		"token_scope_credentials",
		"proxy_pools",
		"provider_instances",
		"credentials",
		"credential_parks",
		"credential_health",
		"model_overrides",
		"model_override_capabilities",
		"model_override_modalities",
		"model_info_snapshots",
		"model_infos",
		"model_info_capabilities",
		"model_info_modalities",
		"model_info_endpoints",
		"virtual_models",
		"virtual_model_members",
		"proxy_pool_entries",
		"proxy_states",
		"plugin_repositories",
		"plugins",
		"plugin_allow_hosts",
		"plugin_versions",
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
	migrator := database.Handle.Migrator()
	for _, table := range tables {
		if !migrator.HasTable(table) {
			t.Errorf("missing table %s", table)
		}
	}
	if len(tables) != 42 {
		t.Errorf("schema holds %d tables, want 42", len(tables))
	}
}

func TestReopenIsNoOp(t *testing.T) {
	_, path := openTemp(t)
	second, err := db.OpenSQLite(context.Background(), path)
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
	database, _ := openTemp(t)
	ctx := context.Background()
	if err := db.Maintenance(ctx, database.Handle); err != nil {
		t.Fatalf("maintenance: %v", err)
	}
	if err := db.CheckIntegrity(ctx, database.Handle); err != nil {
		t.Fatalf("integrity: %v", err)
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
