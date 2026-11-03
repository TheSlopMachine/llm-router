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
package migrations

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

type pluginIdentitySnapshot struct {
	ID               string         `gorm:"primaryKey"`
	DisplayName      string         `gorm:"not null"`
	Author           string         `gorm:"not null"`
	Version          string         `gorm:"not null"`
	PluginAPIVersion string         `gorm:"not null"`
	Description      string         `gorm:"not null"`
	License          string         `gorm:"not null"`
	Unsafe           bool           `gorm:"not null"`
	Disabled         bool           `gorm:"not null"`
	OriginPath       string         `gorm:"not null"`
	OriginManual     bool           `gorm:"not null"`
	Declarations     map[string]any `gorm:"serializer:json"`
	Source           []byte         `gorm:"not null"`
	InstalledAt      time.Time      `gorm:"not null"`
	UpdatedAt        time.Time      `gorm:"not null"`
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
	// Foreign keys in the schema have no ON UPDATE action, so parent
	// identifier updates defer until commit while child rows move.
	if err := transaction.Exec("PRAGMA defer_foreign_keys = ON").Error; err != nil {
		return fmt.Errorf("defer foreign keys: %w", err)
	}
	if err := transaction.Migrator().CreateTable(&pluginIdentityMigrationRecord{}); err != nil {
		return fmt.Errorf("create plugin identity migration table: %w", err)
	}

	var plugins []pluginIdentitySnapshot
	if err := transaction.Find(&plugins).Error; err != nil {
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
			if err := transaction.Where("id = ?", loser.ID).Delete(&pluginIdentitySnapshot{}).Error; err != nil {
				return fmt.Errorf("delete colliding plugin %q: %w", loser.ID, err)
			}
		}
	}

	// Record identifier mappings and rebuild every child first.
	for _, survivor := range survivors {
		newID := buildNewPluginID(survivor.Author, survivor.DisplayName)
		record := pluginIdentityMigrationRecord{OldID: survivor.ID, NewID: newID}
		if err := transaction.Create(&record).Error; err != nil {
			return fmt.Errorf("record migration mapping for %q: %w", survivor.ID, err)
		}
		if survivor.ID == newID {
			continue
		}
		if err := transaction.Model(&pluginStorageIdentitySnapshot{}).Where("plugin_id = ?", survivor.ID).Update("plugin_id", newID).Error; err != nil {
			return fmt.Errorf("rebuild plugin storage identifier for %q: %w", survivor.ID, err)
		}
		if err := transaction.Model(&pluginAllowHostIdentitySnapshot{}).Where("plugin_id = ?", survivor.ID).Update("plugin_id", newID).Error; err != nil {
			return fmt.Errorf("rebuild plugin allow host identifier for %q: %w", survivor.ID, err)
		}
		if err := transaction.Model(&pluginVersionIdentitySnapshot{}).Where("plugin_id = ?", survivor.ID).Update("plugin_id", newID).Error; err != nil {
			return fmt.Errorf("rebuild plugin version identifier for %q: %w", survivor.ID, err)
		}
	}

	// Rebuild the parent row identifiers last.
	for _, survivor := range survivors {
		newID := buildNewPluginID(survivor.Author, survivor.DisplayName)
		if survivor.ID == newID {
			continue
		}
		if err := transaction.Model(&pluginIdentitySnapshot{}).Where("id = ?", survivor.ID).Update("id", newID).Error; err != nil {
			return fmt.Errorf("rebuild plugin identifier for %q: %w", survivor.ID, err)
		}
	}
	return nil
}

func downPluginIdentity(transaction *gorm.DB) error {
	if err := transaction.Exec("PRAGMA defer_foreign_keys = ON").Error; err != nil {
		return fmt.Errorf("defer foreign keys: %w", err)
	}
	var records []pluginIdentityMigrationRecord
	if err := transaction.Find(&records).Error; err != nil {
		return fmt.Errorf("load plugin identity migration records: %w", err)
	}

	for _, record := range records {
		if record.OldID == record.NewID {
			continue
		}
		if err := transaction.Model(&pluginStorageIdentitySnapshot{}).Where("plugin_id = ?", record.NewID).Update("plugin_id", record.OldID).Error; err != nil {
			return fmt.Errorf("restore plugin storage identifier for %q: %w", record.NewID, err)
		}
		if err := transaction.Model(&pluginAllowHostIdentitySnapshot{}).Where("plugin_id = ?", record.NewID).Update("plugin_id", record.OldID).Error; err != nil {
			return fmt.Errorf("restore plugin allow host identifier for %q: %w", record.NewID, err)
		}
		if err := transaction.Model(&pluginVersionIdentitySnapshot{}).Where("plugin_id = ?", record.NewID).Update("plugin_id", record.OldID).Error; err != nil {
			return fmt.Errorf("restore plugin version identifier for %q: %w", record.NewID, err)
		}
		if err := transaction.Model(&pluginIdentitySnapshot{}).Where("id = ?", record.NewID).Update("id", record.OldID).Error; err != nil {
			return fmt.Errorf("restore plugin identifier for %q: %w", record.NewID, err)
		}
	}
	if err := transaction.Migrator().DropTable(&pluginIdentityMigrationRecord{}); err != nil {
		return fmt.Errorf("drop plugin identity migration table: %w", err)
	}
	return nil
}
