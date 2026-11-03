// Plugin identity owns migration version 6 of the durable database.
//
// Version 6 rebuilds plugin identifiers to the author/plugin-name format.
// The previous identifier embedded the installation source (repository id
// or "manual"), which blocked installing the same plugin from another
// source. The new identifier derives from the manifest only, so later
// installs from any source with the same author and plugin name override
// the previous installation. The identity of each row comes from author
// and display name; a collision keeps the most recently updated row and
// deletes the losers with their child rows.
//
// Identifier moves replay rows instead of updating identifiers in place.
// The schema has no ON UPDATE CASCADE, so an in-place parent identifier
// update and a child identifier update each violate the immediate
// foreign-key checks under any ordering. A child row reinsertion after
// the parent copy always references an existing parent, so no deferred
// foreign keys are needed.
package migrations

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type pluginIdentitySnapshot struct {
	ID                 string         `gorm:"primaryKey"`
	DisplayName        string         `gorm:"not null"`
	Author             string         `gorm:"not null"`
	Version            string         `gorm:"not null"`
	PluginAPIVersion   string         `gorm:"not null"`
	Description        string         `gorm:"not null"`
	License            string         `gorm:"not null"`
	Unsafe             bool           `gorm:"not null"`
	Disabled           bool           `gorm:"not null"`
	OriginRepositoryID *string        `gorm:""`
	OriginPath         string         `gorm:"not null"`
	OriginManual       bool           `gorm:"not null"`
	Declarations       map[string]any `gorm:"serializer:json"`
	Source             []byte         `gorm:"not null"`
	InstalledAt        time.Time      `gorm:"not null"`
	UpdatedAt          time.Time      `gorm:"not null"`
}

// TableName pins the plugins table name.
func (pluginIdentitySnapshot) TableName() string {
	return "plugins"
}

type pluginAllowHostIdentitySnapshot struct {
	PluginID string `gorm:"primaryKey"`
	Host     string `gorm:"primaryKey"`
}

// TableName pins the plugin allow hosts table name.
func (pluginAllowHostIdentitySnapshot) TableName() string {
	return "plugin_allow_hosts"
}

type pluginVersionIdentitySnapshot struct {
	ID           string         `gorm:"primaryKey"`
	PluginID     string         `gorm:"not null;index"`
	Version      string         `gorm:"not null"`
	Source       []byte         `gorm:"not null"`
	Declarations map[string]any `gorm:"serializer:json"`
	CreatedAt    time.Time      `gorm:"not null"`
}

// TableName pins the plugin versions table name.
func (pluginVersionIdentitySnapshot) TableName() string {
	return "plugin_versions"
}

type pluginStorageIdentitySnapshot struct {
	PluginID  string     `gorm:"primaryKey"`
	Scope     string     `gorm:"primaryKey"`
	Key       string     `gorm:"primaryKey"`
	Value     []byte     `gorm:"not null"`
	ExpiresAt *time.Time `gorm:"index"`
}

// TableName pins the plugin storage table name.
func (pluginStorageIdentitySnapshot) TableName() string {
	return "plugin_storage"
}

// movePluginChildRows replays one child table from an old plugin
// identifier onto the new one: loaded rows delete first, rewrite their
// plugin identifier, and reinsert under the new identifier. The parent
// copy with the new identifier already exists when this runs, so every
// intermediate state passes the immediate foreign-key checks.
func movePluginChildRows[Row any](ctx context.Context, transaction *gorm.DB, oldID, newID string, table string, setPluginID func(row *Row)) error {
	rows, err := gorm.G[Row](transaction, clause.Eq{Column: "plugin_id", Value: oldID}).Find(ctx)
	if err != nil {
		return fmt.Errorf("load %s rows for %q: %w", table, oldID, err)
	}
	if _, err := gorm.G[Row](transaction, clause.Eq{Column: "plugin_id", Value: oldID}).Delete(ctx); err != nil {
		return fmt.Errorf("delete %s rows for %q: %w", table, oldID, err)
	}
	for index := range rows {
		setPluginID(&rows[index])
	}
	if len(rows) > 0 {
		if err := gorm.G[Row](transaction, clause.OnConflict{UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return fmt.Errorf("reinsert %s rows under %q: %w", table, newID, err)
		}
	}
	return nil
}

// pluginIdentityMigrationRecord maps one old plugin identifier onto its
// rebuilt identifier. The table lets a rollback restore source-derived
// identifiers; collision losers cannot be restored because they no longer
// exist.
type pluginIdentityMigrationRecord struct {
	OldID string `gorm:"primaryKey"`
	NewID string `gorm:"not null"`
}

// TableName pins the plugin identity migration table name.
func (pluginIdentityMigrationRecord) TableName() string {
	return "plugin_identity_migration"
}

// sanitizeIDPartForMigration converts one author or plugin name into the
// project identifier alphabet: letters, digits, dash, underscore and dot.
// Space and unknown characters collapse into a single dash.
func sanitizeIDPartForMigration(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	prevDash := false
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9', character == '-', character == '_', character == '.':
			b.WriteRune(character)
			prevDash = false
		default:
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// buildNewPluginID constructs the new identifier from author and display
// name. An empty result means the row cannot be migrated.
func buildNewPluginID(author, displayName string) string {
	sanitizedAuthor := sanitizeIDPartForMigration(author)
	sanitizedPlugin := sanitizeIDPartForMigration(displayName)
	if sanitizedAuthor == "" || sanitizedPlugin == "" {
		return ""
	}
	return sanitizedAuthor + "/" + sanitizedPlugin
}

func upPluginIdentity(transaction *gorm.DB) error {
	ctx := context.Background()
	if err := transaction.Migrator().CreateTable(&pluginIdentityMigrationRecord{}); err != nil {
		return fmt.Errorf("create plugin identity migration table: %w", err)
	}

	plugins, err := gorm.G[pluginIdentitySnapshot](transaction).Find(ctx)
	if err != nil {
		return fmt.Errorf("load plugins: %w", err)
	}

	// Group rows by rebuilt identifier.
	groups := map[string][]pluginIdentitySnapshot{}
	for _, plugin := range plugins {
		newID := buildNewPluginID(plugin.Author, plugin.DisplayName)
		if newID == "" {
			return fmt.Errorf("plugin %q: empty rebuilt identifier from author %q and display name %q", plugin.ID, plugin.Author, plugin.DisplayName)
		}
		groups[newID] = append(groups[newID], plugin)
	}

	// Resolve collisions: one row survives per rebuilt identifier. Ties
	// break deterministically by older install and then smaller old id.
	var survivors []pluginIdentitySnapshot
	for _, group := range groups {
		sort.Slice(group, func(first, second int) bool {
			if !group[first].UpdatedAt.Equal(group[second].UpdatedAt) {
				return group[first].UpdatedAt.After(group[second].UpdatedAt)
			}
			if !group[first].InstalledAt.Equal(group[second].InstalledAt) {
				return group[first].InstalledAt.After(group[second].InstalledAt)
			}
			return group[first].ID < group[second].ID
		})
		survivors = append(survivors, group[0])
		for _, loser := range group[1:] {
			if _, err := gorm.G[pluginIdentitySnapshot](transaction, clause.Eq{Column: "id", Value: loser.ID}).Delete(ctx); err != nil {
				return fmt.Errorf("delete colliding plugin %q: %w", loser.ID, err)
			}
		}
	}

	// Record identifier mappings. A surviving row keeps its identifier
	// when author and display name already spell it.
	for _, survivor := range survivors {
		newID := buildNewPluginID(survivor.Author, survivor.DisplayName)
		record := pluginIdentityMigrationRecord{OldID: survivor.ID, NewID: newID}
		if err := transaction.Create(&record).Error; err != nil {
			return fmt.Errorf("record migration mapping for %q: %w", survivor.ID, err)
		}
	}

	return rebuildPluginIDs(ctx, transaction, survivors)
}

// rebuildPluginIDs replays every surviving plugin row and its children
// onto the rebuilt identifier. For each row the parent copy under the
// new identifier inserts first, then the child rows replay, then the
// old parent row deletes.
func rebuildPluginIDs(ctx context.Context, transaction *gorm.DB, survivors []pluginIdentitySnapshot) error {
	for _, survivor := range survivors {
		newID := buildNewPluginID(survivor.Author, survivor.DisplayName)
		if survivor.ID == newID {
			continue
		}
		parent := survivor
		parent.ID = newID
		if err := gorm.G[pluginIdentitySnapshot](transaction).Create(ctx, &parent); err != nil {
			return fmt.Errorf("insert plugin copy for %q: %w", survivor.ID, err)
		}
		if err := movePluginChildRows(ctx, transaction, survivor.ID, newID, "plugin storage", func(row *pluginStorageIdentitySnapshot) { row.PluginID = newID }); err != nil {
			return err
		}
		if err := movePluginChildRows(ctx, transaction, survivor.ID, newID, "plugin allow host", func(row *pluginAllowHostIdentitySnapshot) { row.PluginID = newID }); err != nil {
			return err
		}
		if err := movePluginChildRows(ctx, transaction, survivor.ID, newID, "plugin version", func(row *pluginVersionIdentitySnapshot) { row.PluginID = newID }); err != nil {
			return err
		}
		if _, err := gorm.G[pluginIdentitySnapshot](transaction, clause.Eq{Column: "id", Value: survivor.ID}).Delete(ctx); err != nil {
			return fmt.Errorf("delete old plugin row for %q: %w", survivor.ID, err)
		}
	}
	return nil
}

func downPluginIdentity(transaction *gorm.DB) error {
	ctx := context.Background()
	records, err := gorm.G[pluginIdentityMigrationRecord](transaction).Find(ctx)
	if err != nil {
		return fmt.Errorf("load plugin identity migration records: %w", err)
	}

	for _, record := range records {
		if record.OldID == record.NewID {
			continue
		}
		rows, err := gorm.G[pluginIdentitySnapshot](transaction, clause.Eq{Column: "id", Value: record.NewID}).Find(ctx)
		if err != nil {
			return fmt.Errorf("load plugin row for %q: %w", record.NewID, err)
		}
		if len(rows) == 0 {
			return fmt.Errorf("plugin row for %q: not found", record.NewID)
		}
		parent := rows[0]
		parent.ID = record.OldID
		if err := gorm.G[pluginIdentitySnapshot](transaction).Create(ctx, &parent); err != nil {
			return fmt.Errorf("restore plugin row for %q: %w", record.OldID, err)
		}
		if err := movePluginChildRows(ctx, transaction, record.NewID, record.OldID, "plugin storage", func(row *pluginStorageIdentitySnapshot) { row.PluginID = record.OldID }); err != nil {
			return err
		}
		if err := movePluginChildRows(ctx, transaction, record.NewID, record.OldID, "plugin allow host", func(row *pluginAllowHostIdentitySnapshot) { row.PluginID = record.OldID }); err != nil {
			return err
		}
		if err := movePluginChildRows(ctx, transaction, record.NewID, record.OldID, "plugin version", func(row *pluginVersionIdentitySnapshot) { row.PluginID = record.OldID }); err != nil {
			return err
		}
		if _, err := gorm.G[pluginIdentitySnapshot](transaction, clause.Eq{Column: "id", Value: record.NewID}).Delete(ctx); err != nil {
			return fmt.Errorf("delete rebuilt plugin row for %q: %w", record.NewID, err)
		}
	}

	if err := transaction.Migrator().DropTable(&pluginIdentityMigrationRecord{}); err != nil {
		return fmt.Errorf("drop plugin identity migration table: %w", err)
	}
	return nil
}
