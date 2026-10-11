// Package migrate owns the forward-only SQLite migration registry and runner.
// Version numbers are never reused. History edits fail startup loudly.
package migrate

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Migration is one numbered schema change.
type Migration struct {
	Version int64
	Name    string
	Up      func(transaction *gorm.DB) error
}

// SchemaMigration records one applied migration.
type SchemaMigration struct {
	Version   int64     `gorm:"primaryKey"`
	Name      string    `gorm:"not null"`
	AppliedAt time.Time `gorm:"not null"`
}

// TableName pins the migrations table name.
func (SchemaMigration) TableName() string {
	return "schema_migrations"
}

// Run applies pending migrations in registry order.
// Each migration runs in one transaction together with its history row.
func Run(ctx context.Context, database *gorm.DB, registry []Migration) error {
	if len(registry) == 0 {
		return fmt.Errorf("migration registry is empty")
	}
	seen := map[int64]string{}
	for index, migration := range registry {
		if migration.Version <= 0 {
			return fmt.Errorf("migration at index %d has invalid version %d", index, migration.Version)
		}
		if migration.Name == "" {
			return fmt.Errorf("migration version %d has no name", migration.Version)
		}
		if migration.Up == nil {
			return fmt.Errorf("migration version %d has no forward step", migration.Version)
		}
		if previous, exists := seen[migration.Version]; exists {
			return fmt.Errorf("duplicate migration version %d (%q and %q)", migration.Version, previous, migration.Name)
		}
		seen[migration.Version] = migration.Name
		if index > 0 && migration.Version != registry[index-1].Version+1 {
			return fmt.Errorf("migration versions are not contiguous at version %d", migration.Version)
		}
	}
	migrator := database.Migrator()
	if !migrator.HasTable(&SchemaMigration{}) {
		if err := migrator.CreateTable(&SchemaMigration{}); err != nil {
			return fmt.Errorf("create schema_migrations: %w", err)
		}
	}
	applied, err := gorm.G[SchemaMigration](database).Find(ctx)
	if err != nil {
		return fmt.Errorf("load applied migrations: %w", err)
	}
	byVersion := map[int64]SchemaMigration{}
	for _, row := range applied {
		byVersion[row.Version] = row
	}
	for _, migration := range registry {
		if row, exists := byVersion[migration.Version]; exists {
			if row.Name != migration.Name {
				return fmt.Errorf("migration version %d name changed: database has %q, registry has %q", migration.Version, row.Name, migration.Name)
			}
			continue
		}
	}
	for _, row := range applied {
		if _, exists := seen[row.Version]; !exists {
			return fmt.Errorf("database has migration version %d %q missing from registry: binary is older than the database", row.Version, row.Name)
		}
	}
	for _, migration := range registry {
		if _, exists := byVersion[migration.Version]; exists {
			continue
		}
		migration := migration
		if err := database.Transaction(func(transaction *gorm.DB) error {
			if err := migration.Up(transaction); err != nil {
				return err
			}
			record := SchemaMigration{Version: migration.Version, Name: migration.Name, AppliedAt: time.Now().UTC()}
			if err := gorm.G[SchemaMigration](transaction, clause.OnConflict{DoNothing: true}).Create(ctx, &record); err != nil {
				return fmt.Errorf("record migration version %d: %w", migration.Version, err)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("apply migration version %d %q: %w", migration.Version, migration.Name, err)
		}
		byVersion[migration.Version] = SchemaMigration{Version: migration.Version, Name: migration.Name}
	}
	return nil
}
