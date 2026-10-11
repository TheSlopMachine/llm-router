package testutil

import (
	"testing"
)

// TestSetupTestSQLite pins the shared SQLite fixture: an empty file opens
// with the full migrated schema.
func TestSetupTestSQLite(t *testing.T) {
	database := SetupTestSQLite(t)
	if database == nil || database.Handle == nil {
		t.Fatalf("fixture returned no handle")
	}
	for _, table := range []string{"router_tokens", "provider_instances", "metric_buckets", "plugin_storage"} {
		if !database.Handle.Migrator().HasTable(table) {
			t.Errorf("missing table %s", table)
		}
	}
}
