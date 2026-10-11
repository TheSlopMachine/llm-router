// Package db manages the embedded database handles.
//
// The SQLite handle in this set of files serves all traffic. New code uses
// the SQLite handle through OpenSQLite.
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
