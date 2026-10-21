package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
	"github.com/TheSlopMachine/llm-router/internal/db/migrations"
	"gorm.io/gorm"
)

// ErrParentMissing reports a missing parent directory for a SQLite file.
// Managed XDG parents are created by the configuration layer before open;
// custom parents must exist. Match with errors.Is.
var ErrParentMissing = errors.New("parent directory missing")

// MaxOpenConnections caps each SQLite connection pool.
// SQLite serializes writers on the database lock; the pool raises read
// concurrency, not write concurrency. Decision D4 sets 16.
const MaxOpenConnections = 16

// Database wraps the primary GORM SQLite handle plus the cache and events
// handles. Handle owns durable configuration: providers, credentials,
// tokens, and installed plugins. Cache owns rebuildable state: sessions,
// upstream model lists, cooldown parks, telemetry, and async compat rows.
// Events owns operator telemetry only: deleting it while stopped loses log
// history and nothing else. The cache and events files may be deleted while
// stopped; startup recreates them empty.
type Database struct {
	Handle *gorm.DB
	Cache  *gorm.DB
	Events *gorm.DB
}

// OpenSQLite connects to all three SQLite files, configures the pools,
// asserts the open state, migrates each schema, copies legacy cache tables
// out of a single-file database once, and migrates the primary schema. Any
// failure stops startup.
func OpenSQLite(ctx context.Context, path, cachePath, eventsPath string) (*Database, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("database path is required")
	}
	if strings.TrimSpace(cachePath) == "" {
		return nil, fmt.Errorf("cache path is required")
	}
	if strings.TrimSpace(eventsPath) == "" {
		return nil, fmt.Errorf("events path is required")
	}
	if sameFile(path, cachePath) || sameFile(path, eventsPath) || sameFile(cachePath, eventsPath) {
		return nil, fmt.Errorf("database, cache and events paths must differ")
	}
	primary, err := openOne(ctx, path, BuildSQLiteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open primary sqlite at %q: %w", path, err)
	}
	cache, err := openOne(ctx, cachePath, BuildSQLiteDSN(cachePath))
	if err != nil {
		_ = closeHandle(primary)
		return nil, fmt.Errorf("open cache sqlite at %q: %w", cachePath, err)
	}
	events, err := openOne(ctx, eventsPath, BuildEventsDSN(eventsPath))
	if err != nil {
		_ = closeHandle(primary)
		_ = closeHandle(cache)
		return nil, fmt.Errorf("open events sqlite at %q: %w", eventsPath, err)
	}
	if err := migrate.Run(ctx, cache, migrations.RegistryCache); err != nil {
		_ = closeHandle(primary)
		_ = closeHandle(cache)
		_ = closeHandle(events)
		return nil, fmt.Errorf("migrate cache sqlite: %w", err)
	}
	if err := migrate.Run(ctx, events, migrations.RegistryEvents); err != nil {
		_ = closeHandle(primary)
		_ = closeHandle(cache)
		_ = closeHandle(events)
		return nil, fmt.Errorf("migrate events sqlite: %w", err)
	}
	if migrations.LegacySingleFile(primary) {
		if err := migrations.CopyLegacyCacheTables(ctx, primary, cache); err != nil {
			_ = closeHandle(primary)
			_ = closeHandle(cache)
			_ = closeHandle(events)
			return nil, fmt.Errorf("split legacy cache tables: %w", err)
		}
	}
	if err := migrate.Run(ctx, primary, migrations.RegistryDatabase); err != nil {
		_ = closeHandle(primary)
		_ = closeHandle(cache)
		_ = closeHandle(events)
		return nil, fmt.Errorf("migrate primary sqlite: %w", err)
	}
	return &Database{Handle: primary, Cache: cache, Events: events}, nil
}

// Close checkpoints all write-ahead logs, vacuums all files, optimizes
// them, and closes all pools. All handles close even when the first
// reports an error.
func (database *Database) Close() error {
	if database == nil {
		return nil
	}
	eventsErr := closeHandle(database.Events)
	primaryErr := closeHandle(database.Handle)
	if err := closeHandle(database.Cache); err != nil && primaryErr == nil && eventsErr == nil {
		primaryErr = err
	}
	if eventsErr != nil {
		return eventsErr
	}
	return primaryErr
}

// Release closes all pools without checkpoint, vacuum or optimize. Short
// commands use it while a running server owns the files: maintenance
// belongs to the owner, and a vacuum here would stall its writers.
func (database *Database) Release() error {
	if database == nil {
		return nil
	}
	var first error
	for _, handle := range []*gorm.DB{database.Events, database.Handle, database.Cache} {
		if handle == nil {
			continue
		}
		sqlDatabase, err := handle.DB()
		if err != nil {
			if first == nil {
				first = fmt.Errorf("resolve sql handle: %w", err)
			}
			continue
		}
		if err := sqlDatabase.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// OpenEventsReadOnly opens only the events file in read-only mode. It
// never creates, migrates or vacuums, so it is safe next to a running
// server. The returned Database carries the Events handle only.
func OpenEventsReadOnly(path string) (*Database, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("events path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("events database %s: %w", path, err)
	}
	handle, err := gorm.Open(openSQLiteDialector(BuildEventsReadOnlyDSN(path)), NewGormConfiguration())
	if err != nil {
		return nil, fmt.Errorf("open events sqlite at %q: %w", path, err)
	}
	sqlDatabase, err := handle.DB()
	if err != nil {
		return nil, fmt.Errorf("resolve sql handle: %w", err)
	}
	sqlDatabase.SetMaxOpenConns(2)
	return &Database{Events: handle}, nil
}

// openOne prepares the file, connects the GORM SQLite handle, configures
// the pool, and asserts the open state.
func openOne(ctx context.Context, path, dsn string) (*gorm.DB, error) {
	if err := prepareFile(path); err != nil {
		return nil, err
	}
	handle, err := gorm.Open(openSQLiteDialector(dsn), NewGormConfiguration())
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

// closeHandle checkpoints the write-ahead log, vacuums the file, optimizes
// it, and closes the pool. Vacuum runs last among writers: Close is
// deferred after Run returns, when tickers, flush loops and request
// handlers have all stopped. A vacuum failure never skips the remaining
// steps. A nil handle is not an error.
func closeHandle(handle *gorm.DB) error {
	if handle == nil {
		return nil
	}
	ctx := context.Background()
	if err := checkpoint(ctx, handle); err != nil {
		return err
	}
	vacuumErr := Vacuum(ctx, handle)
	if err := optimize(ctx, handle); err != nil {
		return err
	}
	sqlDatabase, err := handle.DB()
	if err != nil {
		return fmt.Errorf("resolve sql handle: %w", err)
	}
	if err := sqlDatabase.Close(); err != nil {
		return err
	}
	return vacuumErr
}

// prepareFile requires the parent directory and removes orphaned
// write-ahead sidecars when the main file is missing. A cache file deleted
// while stopped otherwise leaves its -wal/-shm behind, and the fresh file
// must never inherit them. Directory creation lives in the configuration
// layer for managed XDG paths only; this helper never creates directories.
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
	parent := filepath.Dir(filepath.Clean(path))
	if parent == "." || parent == "" {
		return nil
	}
	if info, err := os.Stat(parent); err == nil {
		if info.IsDir() {
			return nil
		}
		return fmt.Errorf("parent path %s is not a directory: %w", parent, ErrParentMissing)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect sqlite parent dir %s: %w", parent, err)
	}
	return fmt.Errorf("parent directory %s does not exist: %w", parent, ErrParentMissing)
}

// sameFile reports whether both paths address the same file.
func sameFile(first, second string) bool {
	return strings.EqualFold(filepath.Clean(first), filepath.Clean(second))
}
