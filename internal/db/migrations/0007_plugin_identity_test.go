package migrations

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func openIdentityHandle(t *testing.T, path string) *gorm.DB {
	t.Helper()
	handle, err := gorm.Open(sqlite.Open("file:"+path+"?_pragma=foreign_keys(1)"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open identity handle: %v", err)
	}
	sqlDatabase, err := handle.DB()
	if err != nil {
		t.Fatalf("resolve sql handle: %v", err)
	}
	t.Cleanup(func() {
		sqlDatabase.Close()
	})
	return handle
}

// loadPluginsOrderedById loads every plugin row ordered by identifier.
func loadPluginsOrderedById(handle *gorm.DB) ([]pluginIdentitySnapshot, error) {
	return gorm.G[pluginIdentitySnapshot](handle, clause.OrderBy{
		Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}},
	}).Find(context.Background())
}

// loadAllowHostsOrderedByHost loads every allow-host row ordered by host.
func loadAllowHostsOrderedByHost(handle *gorm.DB) ([]pluginAllowHostIdentitySnapshot, error) {
	return gorm.G[pluginAllowHostIdentitySnapshot](handle, clause.OrderBy{
		Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "host"}}},
	}).Find(context.Background())
}

// seedIdentityRows applies every earlier durable migration and seeds two
// source-distinct installed plugins with child rows.
func seedIdentityRows(t *testing.T, ctx context.Context, primary *gorm.DB, now time.Time) {
	t.Helper()
	if err := migrate.Run(ctx, primary, RegistryDatabase[:5]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	plugins := []pluginIdentitySnapshot{
		{
			ID:               "github/owner/repo/test-author/test-plugin",
			DisplayName:      "Test Plugin",
			Author:           "Test Author",
			Version:          "1.0.0",
			PluginAPIVersion: "1.6",
			Description:      "Test plugin from repo",
			License:          "MIT",
			OriginPath:       "llm-router-plugins/test-plugin.lua",
			Source:           []byte("repo source"),
			InstalledAt:      now.Add(-48 * time.Hour),
			UpdatedAt:        now.Add(-48 * time.Hour),
		},
		{
			ID:               "manual/another-author/another-plugin",
			DisplayName:      "Another Plugin",
			Author:           "Another Author",
			Version:          "2.0.0",
			PluginAPIVersion: "1.6",
			Description:      "Another test plugin",
			License:          "Apache-2.0",
			OriginManual:     true,
			Source:           []byte("manual source"),
			InstalledAt:      now,
			UpdatedAt:        now,
		},
	}
	for _, plugin := range plugins {
		if err := primary.WithContext(ctx).Create(&plugin).Error; err != nil {
			t.Fatalf("seed plugin %q: %v", plugin.ID, err)
		}
	}
	allowHosts := []pluginAllowHostIdentitySnapshot{
		{PluginID: "github/owner/repo/test-author/test-plugin", Host: "example.com"},
		{PluginID: "manual/another-author/another-plugin", Host: "api.example.com"},
	}
	for _, host := range allowHosts {
		if err := primary.WithContext(ctx).Create(&host).Error; err != nil {
			t.Fatalf("seed allow host: %v", err)
		}
	}
	versions := []pluginVersionIdentitySnapshot{
		{ID: "version-1", PluginID: "github/owner/repo/test-author/test-plugin", Version: "0.9.0", Source: []byte("old source"), Declarations: map[string]any{}, CreatedAt: now.Add(-24 * time.Hour)},
	}
	for _, version := range versions {
		if err := primary.WithContext(ctx).Create(&version).Error; err != nil {
			t.Fatalf("seed version: %v", err)
		}
	}
	storage := []pluginStorageIdentitySnapshot{
		{PluginID: "github/owner/repo/test-author/test-plugin", Scope: "scope", Key: "key", Value: []byte(`"value"`)},
	}
	for _, row := range storage {
		if err := primary.WithContext(ctx).Create(&row).Error; err != nil {
			t.Fatalf("seed storage: %v", err)
		}
	}
}

func TestPluginIdentityMigration(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	primary := openIdentityHandle(t, filepath.Join(t.TempDir(), "identity.sqlite3"))
	seedIdentityRows(t, ctx, primary, now)

	if err := primary.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		return upPluginIdentity(transaction)
	}); err != nil {
		t.Fatalf("apply plugin identity migration: %v", err)
	}

	var migratedPlugins []pluginIdentitySnapshot
	migratedPlugins, err := loadPluginsOrderedById(primary.WithContext(ctx))
	if err != nil {
		t.Fatalf("load migrated plugins: %v", err)
	}
	if len(migratedPlugins) != 2 {
		t.Fatalf("plugins hold %d rows, want 2", len(migratedPlugins))
	}
	if got, want := migratedPlugins[0].ID, "Another-Author/Another-Plugin"; got != want {
		t.Errorf("plugin id: got %q, want %q", got, want)
	}
	if got, want := migratedPlugins[1].ID, "Test-Author/Test-Plugin"; got != want {
		t.Errorf("plugin id: got %q, want %q", got, want)
	}
	if got, want := migratedPlugins[1].DisplayName, "Test Plugin"; got != want {
		t.Errorf("display name: got %q, want %q", got, want)
	}
	if got, want := migratedPlugins[1].Author, "Test Author"; got != want {
		t.Errorf("author: got %q, want %q", got, want)
	}

	migratedHosts, err := loadAllowHostsOrderedByHost(primary.WithContext(ctx))
	if err != nil {
		t.Fatalf("load migrated allow hosts: %v", err)
	}
	if len(migratedHosts) != 2 {
		t.Fatalf("allow hosts hold %d rows, want 2", len(migratedHosts))
	}
	hostsByPlugin := map[string]string{}
	for _, host := range migratedHosts {
		hostsByPlugin[host.Host] = host.PluginID
	}
	if got, want := hostsByPlugin["example.com"], "Test-Author/Test-Plugin"; got != want {
		t.Errorf("allow host plugin id: got %q, want %q", got, want)
	}
	if got, want := hostsByPlugin["api.example.com"], "Another-Author/Another-Plugin"; got != want {
		t.Errorf("allow host plugin id: got %q, want %q", got, want)
	}

	var migratedVersions []pluginVersionIdentitySnapshot
	if err := primary.WithContext(ctx).Find(&migratedVersions).Error; err != nil {
		t.Fatalf("load migrated versions: %v", err)
	}
	if len(migratedVersions) != 1 {
		t.Fatalf("versions hold %d rows, want 1", len(migratedVersions))
	}
	if got, want := migratedVersions[0].PluginID, "Test-Author/Test-Plugin"; got != want {
		t.Errorf("version plugin id: got %q, want %q", got, want)
	}

	var migratedStorage []pluginStorageIdentitySnapshot
	if err := primary.WithContext(ctx).Find(&migratedStorage).Error; err != nil {
		t.Fatalf("load migrated storage: %v", err)
	}
	if len(migratedStorage) != 1 {
		t.Fatalf("storage holds %d rows, want 1", len(migratedStorage))
	}
	if got, want := migratedStorage[0].PluginID, "Test-Author/Test-Plugin"; got != want {
		t.Errorf("storage plugin id: got %q, want %q", got, want)
	}
}

func TestPluginIdentityMigrationCollisionKeepsNewest(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	primary := openIdentityHandle(t, filepath.Join(t.TempDir(), "collision.sqlite3"))
	if err := migrate.Run(ctx, primary, RegistryDatabase[:5]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	older := pluginIdentitySnapshot{
		ID:               "github/owner/repo/test-author/test-plugin",
		DisplayName:      "Test Plugin",
		Author:           "Test Author",
		Version:          "1.0.0",
		PluginAPIVersion: "1.6",
		Description:      "Test plugin from repo",
		License:          "MIT",
		OriginPath:       "llm-router-plugins/test-plugin.lua",
		Source:           []byte("repo source"),
		InstalledAt:      now.Add(-48 * time.Hour),
		UpdatedAt:        now.Add(-48 * time.Hour),
	}
	newer := pluginIdentitySnapshot{
		ID:               "manual/test-author/test-plugin",
		DisplayName:      "Test Plugin",
		Author:           "Test Author",
		Version:          "2.0.0",
		PluginAPIVersion: "1.6",
		Description:      "Test plugin from manual",
		License:          "MIT",
		OriginManual:     true,
		Source:           []byte("manual source"),
		InstalledAt:      now,
		UpdatedAt:        now,
	}
	for _, plugin := range []pluginIdentitySnapshot{older, newer} {
		if err := primary.WithContext(ctx).Create(&plugin).Error; err != nil {
			t.Fatalf("seed plugin %q: %v", plugin.ID, err)
		}
	}

	if err := primary.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		return upPluginIdentity(transaction)
	}); err != nil {
		t.Fatalf("apply plugin identity migration: %v", err)
	}

	var migratedPlugins []pluginIdentitySnapshot
	if err := primary.WithContext(ctx).Find(&migratedPlugins).Error; err != nil {
		t.Fatalf("load migrated plugins: %v", err)
	}
	if len(migratedPlugins) != 1 {
		t.Fatalf("plugins hold %d rows, want 1 survivor", len(migratedPlugins))
	}
	if got, want := migratedPlugins[0].ID, "Test-Author/Test-Plugin"; got != want {
		t.Errorf("plugin id: got %q, want %q", got, want)
	}
	if got, want := migratedPlugins[0].Version, "2.0.0"; got != want {
		t.Errorf("version: got %q, want %q", got, want)
	}
}

func TestPluginIdentityMigrationRollbackRestoresSourceIDs(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	primary := openIdentityHandle(t, filepath.Join(t.TempDir(), "rollback.sqlite3"))
	seedIdentityRows(t, ctx, primary, now)

	if err := primary.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		return upPluginIdentity(transaction)
	}); err != nil {
		t.Fatalf("apply plugin identity migration: %v", err)
	}
	var upPlugins []pluginIdentitySnapshot
	upPlugins, err := loadPluginsOrderedById(primary.WithContext(ctx))
	if err != nil {
		t.Fatalf("load migrated plugins: %v", err)
	}
	if len(upPlugins) != 2 {
		t.Fatalf("plugins hold %d rows, want 2", len(upPlugins))
	}
	if got, want := upPlugins[1].ID, "Test-Author/Test-Plugin"; got != want {
		t.Errorf("migrated plugin id: got %q, want %q", got, want)
	}

	if err := primary.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		return downPluginIdentity(transaction)
	}); err != nil {
		t.Fatalf("revert plugin identity migration: %v", err)
	}

	downPlugins, err := loadPluginsOrderedById(primary.WithContext(ctx))
	if err != nil {
		t.Fatalf("load restored plugins: %v", err)
	}
	if len(downPlugins) != 2 {
		t.Fatalf("plugins hold %d rows, want 2", len(downPlugins))
	}
	if got, want := downPlugins[0].ID, "github/owner/repo/test-author/test-plugin"; got != want {
		t.Errorf("restored plugin id: got %q, want %q", got, want)
	}
	if got, want := downPlugins[1].ID, "manual/another-author/another-plugin"; got != want {
		t.Errorf("restored plugin id: got %q, want %q", got, want)
	}
}

func TestSanitizeIDPartForMigration(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Simple", "Simple"},
		{"Two Words", "Two-Words"},
		{"With-Special!Chars", "With-Special-Chars"},
		{"  Spaced  ", "Spaced"},
		{"UPPER CASE", "UPPER-CASE"},
		{"Mix3d C4s3", "Mix3d-C4s3"},
		{"With_Underscore", "With_Underscore"},
		{"With.Dot", "With.Dot"},
	}
	for _, test := range tests {
		if got := sanitizeIDPartForMigration(test.input); got != test.expected {
			t.Errorf("sanitizeIDPartForMigration(%q): got %q, want %q", test.input, got, test.expected)
		}
	}
}
