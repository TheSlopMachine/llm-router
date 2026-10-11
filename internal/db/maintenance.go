package db

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// Maintenance runs the allowlisted SQLite administration statements.
// This file owns every raw statement in the service. No other file may use
// Raw, Exec, or string conditions. Statements use literals, never
// interpolation. The backup destination travels as a bound parameter.
func Maintenance(ctx context.Context, database *gorm.DB) error {
	if err := database.WithContext(ctx).Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		return fmt.Errorf("checkpoint wal: %w", err)
	}
	if err := database.WithContext(ctx).Exec("PRAGMA optimize").Error; err != nil {
		return fmt.Errorf("optimize database: %w", err)
	}
	return nil
}

// AssertOpenState verifies the connection pragmas and SQLite version.
// Any failure stops startup.
func AssertOpenState(ctx context.Context, database *gorm.DB) error {
	var journalMode string
	if err := database.WithContext(ctx).Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		return fmt.Errorf("read journal_mode: %w", err)
	}
	if journalMode != "wal" {
		return fmt.Errorf("journal_mode is %q, want wal", journalMode)
	}
	var foreignKeys int64
	if err := database.WithContext(ctx).Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		return fmt.Errorf("read foreign_keys: %w", err)
	}
	if foreignKeys != 1 {
		return fmt.Errorf("foreign_keys is off, want on")
	}
	var version string
	if err := database.WithContext(ctx).Raw("select sqlite_version()").Scan(&version).Error; err != nil {
		return fmt.Errorf("read sqlite_version: %w", err)
	}
	var major, minor, patch int
	if _, err := fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch); err != nil {
		return fmt.Errorf("parse sqlite_version %q: %w", version, err)
	}
	if major < 3 || (major == 3 && (minor < 35)) {
		return fmt.Errorf("sqlite_version is %q, want at least 3.35.0", version)
	}
	return nil
}

// CheckIntegrity runs the doctor integrity check.
func CheckIntegrity(ctx context.Context, database *gorm.DB) error {
	var message string
	if err := database.WithContext(ctx).Raw("PRAGMA integrity_check").Scan(&message).Error; err != nil {
		return fmt.Errorf("run integrity_check: %w", err)
	}
	if message != "ok" {
		return fmt.Errorf("integrity_check reports %q", message)
	}
	return nil
}

// BackupTo writes a backup copy to the given destination path.
// The destination travels as a bound parameter.
func BackupTo(ctx context.Context, database *gorm.DB, destination string) error {
	if err := database.WithContext(ctx).Exec("VACUUM INTO ?", destination).Error; err != nil {
		return fmt.Errorf("vacuum into backup: %w", err)
	}
	return nil
}
