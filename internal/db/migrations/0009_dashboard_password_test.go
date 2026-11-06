package migrations

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
	"gorm.io/gorm"
)

// migrateWithAccounts applies durable versions 1..7, seeds the accounts and
// the bootstrapped flag, then applies the rest.
func migrateWithAccounts(t *testing.T, accounts []adminUserSnapshot) *gorm.DB {
	t.Helper()
	ctx := context.Background()
	primary := openIdentityHandle(t, filepath.Join(t.TempDir(), "password.sqlite3"))
	if err := migrate.Run(ctx, primary, RegistryDatabase[:7]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	settings := instanceSettingSnapshot{ID: 1, Bootstrapped: len(accounts) > 0, IsClusterNode: true, ModelsFilter: "enabled", UpdatedAt: time.Now().UTC()}
	if err := gorm.G[instanceSettingSnapshot](primary).Create(ctx, &settings); err != nil {
		t.Fatalf("seed instance settings: %v", err)
	}
	if len(accounts) > 0 {
		if err := gorm.G[adminUserSnapshot](primary).CreateInBatches(ctx, &accounts, 10); err != nil {
			t.Fatalf("seed accounts: %v", err)
		}
	}
	if err := migrate.Run(ctx, primary, RegistryDatabase); err != nil {
		t.Fatalf("apply dashboard password migration: %v", err)
	}
	migrator := primary.Migrator()
	if migrator.HasTable("admin_users") {
		t.Error("admin_users survives the migration")
	}
	if migrator.HasColumn(&instanceBootstrappedSnapshot{}, "bootstrapped") {
		t.Error("instance_settings.bootstrapped survives the migration")
	}
	// The table rebuild that drops the column keeps the row.
	kept, err := gorm.G[instanceProxyMaxChecksSnapshot](primary).Find(ctx)
	if err != nil || len(kept) != 1 || kept[0].ID != 1 {
		t.Errorf("instance settings after rebuild: %+v, %v", kept, err)
	}
	return primary
}

func storedDashboardHashes(t *testing.T, primary *gorm.DB) []string {
	t.Helper()
	rows, err := gorm.G[dashboardPasswordSnapshot](primary).Find(context.Background())
	if err != nil {
		t.Fatalf("read dashboard_password: %v", err)
	}
	hashes := make([]string, 0, len(rows))
	for _, row := range rows {
		hashes = append(hashes, row.PasswordHash)
	}
	return hashes
}

func TestDashboardPasswordPrefersAdminAccount(t *testing.T) {
	now := time.Now().UTC()
	primary := migrateWithAccounts(t, []adminUserSnapshot{
		{Username: "operator", PasswordHash: "operator-hash", CreatedAt: now.Add(-time.Hour)},
		{Username: "admin", PasswordHash: "admin-hash", CreatedAt: now},
	})
	if hashes := storedDashboardHashes(t, primary); len(hashes) != 1 || hashes[0] != "admin-hash" {
		t.Errorf("dashboard password holds %v, want [admin-hash]", hashes)
	}
}

func TestDashboardPasswordFallsBackToOldestAccount(t *testing.T) {
	now := time.Now().UTC()
	primary := migrateWithAccounts(t, []adminUserSnapshot{
		{Username: "late", PasswordHash: "late-hash", CreatedAt: now},
		{Username: "early", PasswordHash: "early-hash", CreatedAt: now.Add(-time.Hour)},
	})
	if hashes := storedDashboardHashes(t, primary); len(hashes) != 1 || hashes[0] != "early-hash" {
		t.Errorf("dashboard password holds %v, want [early-hash]", hashes)
	}
}

func TestDashboardPasswordWithoutAccountsStaysUnset(t *testing.T) {
	primary := migrateWithAccounts(t, nil)
	if hashes := storedDashboardHashes(t, primary); len(hashes) != 0 {
		t.Errorf("dashboard password holds %v, want no row", hashes)
	}
}
