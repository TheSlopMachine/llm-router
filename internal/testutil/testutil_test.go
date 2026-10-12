package testutil

import (
	"testing"
)

// TestSetupTestSQLite pins the shared SQLite fixture: empty database and
// cache files open with their full migrated schemas.
func TestSetupTestSQLite(t *testing.T) {
	database := SetupTestSQLite(t)
	if database == nil || database.Handle == nil || database.Cache == nil {
		t.Fatalf("fixture returned no handle pair")
	}
	for _, table := range []string{"router_tokens", "provider_instances", "plugin_storage"} {
		if !database.Handle.Migrator().HasTable(table) {
			t.Errorf("primary missing table %s", table)
		}
	}
	for _, table := range []string{"sessions", "metric_buckets", "plugin_storage"} {
		if !database.Cache.Migrator().HasTable(table) {
			t.Errorf("cache missing table %s", table)
		}
	}
}
