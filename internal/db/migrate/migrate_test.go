package migrate_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
	"github.com/TheSlopMachine/llm-router/internal/db/migrations"
	"gorm.io/gorm"
)

func openHandle(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "migrate.sqlite3")
	database, err := db.OpenSQLite(context.Background(), path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	return database.Handle
}

func TestRegistryVersionsContiguous(t *testing.T) {
	seen := map[int64]bool{}
	for index, migration := range migrations.Registry {
		if migration.Version != int64(index+1) {
			t.Errorf("registry index %d has version %d, want %d", index, migration.Version, index+1)
		}
		if migration.Name == "" {
			t.Errorf("version %d has no name", migration.Version)
		}
		if seen[migration.Version] {
			t.Errorf("duplicate version %d", migration.Version)
		}
		seen[migration.Version] = true
	}
}

func TestRunIsIdempotent(t *testing.T) {
	ctx := context.Background()
	handle := openHandle(t)
	if err := migrate.Run(ctx, handle, migrations.Registry); err != nil {
		t.Fatalf("rerun migrations: %v", err)
	}
}

func TestRejectsOlderBinary(t *testing.T) {
	ctx := context.Background()
	handle := openHandle(t)
	// Simulate an older binary by running a registry missing the applied version.
	empty := []migrate.Migration{}
	if err := migrate.Run(ctx, handle, empty); err == nil {
		t.Errorf("empty registry accepted applied database, want older-binary error")
	}
}

func TestRejectsRenamedHistory(t *testing.T) {
	ctx := context.Background()
	handle := openHandle(t)
	renamed := make([]migrate.Migration, len(migrations.Registry))
	copy(renamed, migrations.Registry)
	renamed[0].Name = "rewritten_history"
	if err := migrate.Run(ctx, handle, renamed); err == nil {
		t.Errorf("renamed history accepted, want mismatch error")
	}
}
