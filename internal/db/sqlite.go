package db

import (
	"context"
	"fmt"

	"github.com/TheSlopMachine/llm-router/internal/db/migrate"
	"github.com/TheSlopMachine/llm-router/internal/db/migrations"
	"gorm.io/gorm"
)

// MaxOpenConnections caps the SQLite connection pool.
// SQLite serializes writers on the database lock; the pool raises read
// concurrency, not write concurrency. Decision D4 sets 16.
const MaxOpenConnections = 16

// Database wraps the GORM SQLite handle.
type Database struct {
	Handle *gorm.DB
}

// OpenSQLite connects to the SQLite file, configures the pool, asserts the
// open state, and runs migrations. Any failure stops startup.
func OpenSQLite(ctx context.Context, path string) (*Database, error) {
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
		return nil, err
	}
	if err := migrate.Run(ctx, handle, migrations.Registry); err != nil {
		return nil, err
	}
	return &Database{Handle: handle}, nil
}

// Close checkpoints the write-ahead log, optimizes the file, and closes the pool.
func (database *Database) Close() error {
	if database == nil || database.Handle == nil {
		return nil
	}
	if err := Maintenance(context.Background(), database.Handle); err != nil {
		return err
	}
	sqlDatabase, err := database.Handle.DB()
	if err != nil {
		return fmt.Errorf("resolve sql handle: %w", err)
	}
	return sqlDatabase.Close()
}
