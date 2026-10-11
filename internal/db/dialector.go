package db

import (
	"errors"

	gosqlite "github.com/glebarez/go-sqlite"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	sqlite3 "modernc.org/sqlite/lib"
)

// sqliteDialector wraps the glebarez dialector with complete constraint
// translation. Upstream Translate maps UNIQUE, PRIMARYKEY, and FOREIGNKEY
// extended codes only. In this build foreign key violations surface as
// TRIGGER (1811) and check violations (275) fall through unmapped, so the
// wrapper finishes the mapping. One owner for driver knowledge: no other
// file inspects driver error types.
type sqliteDialector struct {
	gorm.Dialector
}

// openSQLiteDialector builds the translating dialector for the given DSN.
func openSQLiteDialector(dsn string) gorm.Dialector {
	return sqliteDialector{Dialector: sqlite.Open(dsn)}
}

// Translate implements gorm.ErrorTranslator. Matching uses errors.As on
// the driver type plus extended result codes, never message text.
func (dial sqliteDialector) Translate(err error) error {
	if err == nil {
		return nil
	}
	if translator, ok := dial.Dialector.(gorm.ErrorTranslator); ok {
		if mapped := translator.Translate(err); errors.Is(mapped, gorm.ErrDuplicatedKey) ||
			errors.Is(mapped, gorm.ErrForeignKeyViolated) ||
			errors.Is(mapped, gorm.ErrCheckConstraintViolated) {
			return mapped
		}
	}
	var driverError *gosqlite.Error
	if !errors.As(err, &driverError) {
		return err
	}
	switch driverError.Code() {
	case sqlite3.SQLITE_CONSTRAINT_UNIQUE,
		sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
		return gorm.ErrDuplicatedKey
	case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY,
		sqlite3.SQLITE_CONSTRAINT_TRIGGER:
		// Trigger-reported failures are foreign key violations
		// surfaced through enforcement: this schema owns no triggers.
		return gorm.ErrForeignKeyViolated
	default:
		if driverError.Code()&0xFF == sqlite3.SQLITE_CONSTRAINT {
			return gorm.ErrCheckConstraintViolated
		}
	}
	return err
}
