// Package db manages the embedded database handles.
//
// The primary handle plus the cache handle in sqlite.go serve all traffic.
// New code uses the pair through OpenSQLite: durable configuration on the
// primary handle, rebuildable state on the cache handle.
package db

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SQLiteFileSuffix marks the new database filename.
const SQLiteFileSuffix = ".sqlite3"

// BuildSQLiteDSN composes the SQLite DSN for the given file path.
// Foreign keys, WAL mode, synchronous NORMAL, busy timeout, immediate
// transaction lock, and SQLite time format are fixed by design.
func BuildSQLiteDSN(path string) string {
	return fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_txlock=immediate&_time_format=sqlite",
		path,
	)
}

// BuildEventsDSN composes the SQLite DSN for the events file. It carries
// the base pragmas plus incremental auto-vacuum so retention pruning
// reclaims disk pages without a full vacuum rewrite.
func BuildEventsDSN(path string) string {
	return fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=auto_vacuum(INCREMENTAL)&_txlock=immediate&_time_format=sqlite",
		path,
	)
}

// BuildEventsReadOnlyDSN composes the DSN for read-only access to the
// events file: no journal mode change, no schema work.
func BuildEventsReadOnlyDSN(path string) string {
	return fmt.Sprintf(
		"file:%s?mode=ro&_pragma=busy_timeout(5000)&_time_format=sqlite",
		path,
	)
}

// NewGormConfiguration builds the shared GORM configuration.
// Timestamps use UTC. Bound values stay out of logs.
func NewGormConfiguration() *gorm.Config {
	gormLogger := logger.New(
		log.New(os.Stderr, "", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
		},
	)
	return &gorm.Config{
		SkipDefaultTransaction: true,
		TranslateError:         true,
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
		Logger: gormLogger,
	}
}
