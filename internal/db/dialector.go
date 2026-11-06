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

// SavePoint forwards to the wrapped dialector. The embedded interface hides
// it, and nested transactions need it: the SQLite migrator rebuilds a table
// inside one when it drops a column.
func (dial sqliteDialector) SavePoint(tx *gorm.DB, name string) error {
	savePointer, ok := dial.Dialector.(gorm.SavePointerDialectorInterface)
	if !ok {
		return gorm.ErrUnsupportedDriver
	}
	return savePointer.SavePoint(tx, name)
}

// RollbackTo forwards to the wrapped dialector, see SavePoint.
func (dial sqliteDialector) RollbackTo(tx *gorm.DB, name string) error {
	savePointer, ok := dial.Dialector.(gorm.SavePointerDialectorInterface)
	if !ok {
		return gorm.ErrUnsupportedDriver
	}
	return savePointer.RollbackTo(tx, name)
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
