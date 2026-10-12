package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
	"github.com/TheSlopMachine/llm-router/internal/db/migrations"
	"gorm.io/gorm"
)

// MaxOpenConnections caps each SQLite connection pool.
// SQLite serializes writers on the database lock; the pool raises read
// concurrency, not write concurrency. Decision D4 sets 16.
const MaxOpenConnections = 16

// Database wraps the primary GORM SQLite handle plus the cache handle.
// Handle owns durable configuration: providers, credentials, tokens, and
// installed plugins. Cache owns rebuildable state: sessions, upstream model
// lists, cooldown parks, telemetry, and async compat rows. The cache file
// may be deleted while stopped without losing configuration; startup
// recreates it empty and background passes refill it.
type Database struct {
	Handle *gorm.DB
	Cache  *gorm.DB
}

// OpenSQLite connects to both SQLite files, configures the pools, asserts
// the open state, migrates the cache schema, copies legacy cache tables out
// of a single-file database once, and migrates the primary schema. Any
// failure stops startup.
func OpenSQLite(ctx context.Context, path, cachePath string) (*Database, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("database path is required")
	}
	if strings.TrimSpace(cachePath) == "" {
		return nil, fmt.Errorf("cache path is required")
	}
	if sameFile(path, cachePath) {
		return nil, fmt.Errorf("database and cache paths must differ")
	}
	primary, err := openOne(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("open primary sqlite at %q: %w", path, err)
	}
	cache, err := openOne(ctx, cachePath)
	if err != nil {
		_ = closeHandle(primary)
		return nil, fmt.Errorf("open cache sqlite at %q: %w", cachePath, err)
	}
	if err := migrate.Run(ctx, cache, migrations.RegistryCache); err != nil {
		_ = closeHandle(primary)
		_ = closeHandle(cache)
		return nil, fmt.Errorf("migrate cache sqlite: %w", err)
	}
	if migrations.LegacySingleFile(primary) {
		if err := migrations.CopyLegacyCacheTables(ctx, primary, cache); err != nil {
			_ = closeHandle(primary)
			_ = closeHandle(cache)
			return nil, fmt.Errorf("split legacy cache tables: %w", err)
		}
	}
	if err := migrate.Run(ctx, primary, migrations.RegistryDatabase); err != nil {
		_ = closeHandle(primary)
		_ = closeHandle(cache)
		return nil, fmt.Errorf("migrate primary sqlite: %w", err)
	}
	return &Database{Handle: primary, Cache: cache}, nil
}

// Close checkpoints both write-ahead logs, optimizes both files, and closes
// both pools. Both handles close even when the first reports an error.
func (database *Database) Close() error {
	if database == nil {
		return nil
	}
	primaryErr := closeHandle(database.Handle)
	if err := closeHandle(database.Cache); err != nil && primaryErr == nil {
		primaryErr = err
	}
	return primaryErr
}

// openOne prepares the file, connects the GORM SQLite handle, configures
// the pool, and asserts the open state.
func openOne(ctx context.Context, path string) (*gorm.DB, error) {
	if err := prepareFile(path); err != nil {
		return nil, err
	}
	handle, err := gorm.Open(openSQLiteDialector(BuildSQLiteDSN(path)), NewGormConfiguration())
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %q: %w", path, err)
	}
	sqlDatabase, err := handle.DB()
	if err != nil {
		return nil, fmt.Errorf("resolve sql handle: %w", err)
	}
	sqlDatabase.SetMaxOpenConns(MaxOpenConnections)
	sqlDatabase.SetMaxIdleConns(MaxOpenConnections)
	sqlDatabase.SetConnMaxIdleTime(0)
	sqlDatabase.SetConnMaxLifetime(0)
	if err := AssertOpenState(ctx, handle); err != nil {
		_ = closeHandle(handle)
		return nil, err
	}
	return handle, nil
}

// closeHandle checkpoints the write-ahead log, optimizes the file, and
// closes the pool. A nil handle is not an error.
func closeHandle(handle *gorm.DB) error {
	if handle == nil {
		return nil
	}
	if err := Maintenance(context.Background(), handle); err != nil {
		return err
	}
	sqlDatabase, err := handle.DB()
	if err != nil {
		return fmt.Errorf("resolve sql handle: %w", err)
	}
	return sqlDatabase.Close()
}

// prepareFile ensures the parent directory exists and removes orphaned
// write-ahead sidecars when the main file is missing. A cache file deleted
// while stopped otherwise leaves its -wal/-shm behind, and the fresh file
// must never inherit them.
func prepareFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect sqlite file %s: %w", path, err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove orphaned sqlite sidecar %s: %w", path+suffix, err)
		}
	}
	parent := filepath.Dir(path)
	if parent != "." && parent != "" {
		if err := os.MkdirAll(parent, 0755); err != nil {
			return fmt.Errorf("create sqlite parent dir %s: %w", parent, err)
		}
	}
	return nil
}

// sameFile reports whether both paths address the same file.
func sameFile(first, second string) bool {
	return strings.EqualFold(filepath.Clean(first), filepath.Clean(second))
}
