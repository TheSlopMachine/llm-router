package luaplugin

import (
	"context"
	"fmt"
	"time"

	stdErrors "errors"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/util"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SetStorageSQLite persists one plugin storage entry on the SQLite path in
// one transaction. The composite primary key replaces the NUL-separated
// bucket string; the value column holds JSON bytes and the indexed expiry
// column carries the TTL deadline. Validation mirrors the bbolt backend so
// plugin-visible shapes stay identical.
func (service *Service) SetStorageSQLite(ctx context.Context, pluginID string, scope string, key string, value any, timeToLive time.Duration) error {
	if err := requireStorageKey(scope, key); err != nil {
		return err
	}
	if timeToLive < 0 {
		return fmt.Errorf("storage ttl must not be negative")
	}
	if timeToLive > maxStorageTTL {
		return fmt.Errorf("storage ttl exceeds 90 days")
	}
	if err := service.sqliteWired(); err != nil {
		return err
	}
	encoded, err := storageValueToBytes(value)
	if err != nil {
		return err
	}
	var expiresAt *time.Time
	if timeToLive > 0 {
		expiry := util.Now().Add(timeToLive).UTC()
		expiresAt = &expiry
	}
	storageRow := record.PluginStorage{
		PluginID:  pluginID,
		Scope:     scope,
		Key:       key,
		Value:     encoded,
		ExpiresAt: expiresAt,
	}
	return service.sqliteDatabase.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		rows := []record.PluginStorage{storageRow}
		if err := gorm.G[record.PluginStorage](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "plugin_id"}, {Name: "scope"}, {Name: "key"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return db.Translate(err)
		}
		return nil
	})
}

// GetStorageSQLite returns one plugin storage entry on the SQLite path in
// one transaction. Expired rows read as missing and are removed in the same
// transaction, matching the bbolt envelope behavior. A miss returns nil
// without an error.
func (service *Service) GetStorageSQLite(ctx context.Context, pluginID string, scope string, key string) (any, error) {
	if err := requireStorageKey(scope, key); err != nil {
		return nil, err
	}
	if err := service.sqliteWired(); err != nil {
		return nil, err
	}
	var result any
	if err := service.sqliteDatabase.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		storageRow, err := gorm.G[record.PluginStorage](transaction, clause.Eq{Column: "plugin_id", Value: pluginID}, clause.Eq{Column: "scope", Value: scope}, clause.Eq{Column: "key", Value: key}).First(ctx)
		if err != nil {
			translated := db.Translate(err)
			if stdErrors.Is(translated, apierrors.ErrNotFound) {
				result = nil
				return nil
			}
			return translated
		}
		if storageRow.ExpiresAt != nil && !util.Now().Before(*storageRow.ExpiresAt) {
			if _, err := gorm.G[record.PluginStorage](transaction, clause.Eq{Column: "plugin_id", Value: pluginID}, clause.Eq{Column: "scope", Value: scope}, clause.Eq{Column: "key", Value: key}).Delete(ctx); err != nil {
				return db.Translate(err)
			}
			result = nil
			return nil
		}
		value, err := storageValueFromBytes(storageRow.Value)
		if err != nil {
			return err
		}
		result = value
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteStorageSQLite removes one plugin storage entry on the SQLite path
// in one transaction. A miss is not an error, matching the bbolt delete.
func (service *Service) DeleteStorageSQLite(ctx context.Context, pluginID string, scope string, key string) error {
	if err := requireStorageKey(scope, key); err != nil {
		return err
	}
	if err := service.sqliteWired(); err != nil {
		return err
	}
	return service.sqliteDatabase.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		_, err := gorm.G[record.PluginStorage](transaction, clause.Eq{Column: "plugin_id", Value: pluginID}, clause.Eq{Column: "scope", Value: scope}, clause.Eq{Column: "key", Value: key}).Delete(ctx)
		return db.Translate(err)
	})
}

// DeletePluginStorageSQLite removes every storage entry for one plugin on
// the SQLite path in one transaction. Plugin deletes cascade from the base
// row; this helper serves explicit storage resets.
func (service *Service) DeletePluginStorageSQLite(ctx context.Context, pluginID string) error {
	if err := service.sqliteWired(); err != nil {
		return err
	}
	return service.sqliteDatabase.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		_, err := gorm.G[record.PluginStorage](transaction, clause.Eq{Column: "plugin_id", Value: pluginID}).Delete(ctx)
		return db.Translate(err)
	})
}

// PruneExpiredStorageSQLite removes every expired storage entry on the
// SQLite path in one transaction and returns the removed count. The indexed
// expiry column drives the condition; null expiries never match the
// less-than comparison and stay.
func (service *Service) PruneExpiredStorageSQLite(ctx context.Context) (int64, error) {
	if err := service.sqliteWired(); err != nil {
		return 0, err
	}
	var removed int64
	if err := service.sqliteDatabase.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.PluginStorage](transaction, clause.Lt{Column: "expires_at", Value: util.Now()}, tracker).Delete(ctx)
		if err != nil {
			return db.Translate(err)
		}
		removed = int64(affected)
		if tracker.RowsAffected != int64(affected) {
			removed = tracker.RowsAffected
		}
		return nil
	}); err != nil {
		return 0, err
	}
	return removed, nil
}
