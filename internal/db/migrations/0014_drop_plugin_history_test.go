package migrations

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
)

func TestDropPluginHistoryRemovesTables(t *testing.T) {
	ctx := context.Background()
	primary := openIdentityHandle(t, filepath.Join(t.TempDir(), "drop-history.sqlite3"))
	if err := migrate.Run(ctx, primary, RegistryDatabase[:12]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	if err := primary.Exec("CREATE TABLE plugin_api_grants (plugin_id TEXT PRIMARY KEY)").Error; err != nil {
		t.Fatalf("create pre-release grants table: %v", err)
	}
	if !primary.Migrator().HasTable("plugin_versions") {
		t.Fatal("earlier migrations must create plugin_versions")
	}
	if err := migrate.Run(ctx, primary, RegistryDatabase); err != nil {
		t.Fatalf("apply drop plugin history migration: %v", err)
	}
	for _, table := range []string{"plugin_versions", "plugin_api_grants"} {
		if primary.Migrator().HasTable(table) {
			t.Errorf("table %s survived the migration", table)
		}
	}
}
