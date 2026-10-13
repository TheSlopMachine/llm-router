package luaplugin

import (
	"context"
	"fmt"
	"strings"
	"time"

	stdErrors "errors"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/util"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	lua "github.com/yuin/gopher-lua"
)

// maxStorageTTL bounds entry lifetimes to 90 days.
const maxStorageTTL = 90 * 24 * time.Hour

// StorageKey identifies one plugin storage entry.
type StorageKey struct {
	PluginID string
	Scope    string
	Key      string
}

// requireStorageKey validates scope and key once for all storage operations.
func requireStorageKey(scope, key string) error {
	if strings.TrimSpace(scope) == "" {
		return fmt.Errorf("storage scope is required")
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("storage key is required")
	}
	return nil
}

// storageBridge serves llm_router.storage.* from one execution under its
// call context.
type storageBridge interface {
	set(pluginID, scope, key string, value any, ttl time.Duration) error
	get(pluginID, scope, key string) (any, error)
	delete(pluginID, scope, key string) error
}

// serviceStorage serves execution storage from the SQLite path.
type serviceStorage struct {
	service *Service
	ctx     context.Context
}

func (b *serviceStorage) callContext() context.Context {
	if b == nil || b.ctx == nil {
		return context.Background()
	}
	return b.ctx
}

func (b *serviceStorage) set(pluginID, scope, key string, value any, ttl time.Duration) error {
	return b.service.SetStorageSQLite(b.callContext(), pluginID, scope, key, value, ttl)
}

func (b *serviceStorage) get(pluginID, scope, key string) (any, error) {
	return b.service.GetStorageSQLite(b.callContext(), pluginID, scope, key)
}

func (b *serviceStorage) delete(pluginID, scope, key string) error {
	return b.service.DeleteStorageSQLite(b.callContext(), pluginID, scope, key)
}

func newStorageTable(L *lua.LState, ctx *executionContext) *lua.LTable {
	table := L.NewTable()
	table.RawSetString("set", L.NewFunction(func(L *lua.LState) int {
		scope := L.CheckString(1)
		key := L.CheckString(2)
		val, verr := fromLuaValue(L.Get(3))
		if verr != nil {
			L.RaiseError("llm_router.storage.set: %s", verr.Error())
			return 0
		}
		var ttl time.Duration
		if options := L.Get(4); options != lua.LNil {
			optionsTable, ok := options.(*lua.LTable)
			if !ok {
				L.RaiseError("llm_router.storage.set: options must be a table")
				return 0
			}
			if tv := optionsTable.RawGetString("ttl"); tv != lua.LNil {
				n, ok := tv.(lua.LNumber)
				if !ok {
					L.RaiseError("llm_router.storage.set: ttl must be a number of seconds")
					return 0
				}
				ttl = time.Duration(float64(n) * float64(time.Second))
			}
		}
		if ctx == nil || ctx.storage == nil {
			L.RaiseError("llm_router.storage: no execution context")
			return 0
		}
		if err := ctx.storage.set(ctx.pluginID, scope, key, val, ttl); err != nil {
			L.RaiseError("llm_router.storage.set: %s", err.Error())
			return 0
		}
		L.Push(lua.LTrue)
		return 1
	}))
	table.RawSetString("get", L.NewFunction(func(L *lua.LState) int {
		scope := L.CheckString(1)
		key := L.CheckString(2)
		if ctx == nil || ctx.storage == nil {
			L.RaiseError("llm_router.storage: no execution context")
			return 0
		}
		val, err := ctx.storage.get(ctx.pluginID, scope, key)
		if err != nil {
			L.RaiseError("llm_router.storage.get: %s", err.Error())
			return 0
		}
		if val == nil {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(toLuaValue(L, val))
		return 1
	}))
	table.RawSetString("delete", L.NewFunction(func(L *lua.LState) int {
		scope := L.CheckString(1)
		key := L.CheckString(2)
		if ctx == nil || ctx.storage == nil {
			L.RaiseError("llm_router.storage: no execution context")
			return 0
		}
		if err := ctx.storage.delete(ctx.pluginID, scope, key); err != nil {
			L.RaiseError("llm_router.storage.delete: %s", err.Error())
			return 0
		}
		L.Push(lua.LTrue)
		return 1
	}))
	return table
}

// SetStorageSQLite persists one plugin storage entry in one transaction.
// Entries with a TTL live in the cache file; durable entries live in the
// durable database. The write targets the routed handle first, then removes
// the same key from the other handle, so an interrupted write leaves a
// duplicate instead of a loss. The composite primary key replaces the
// NUL-separated bucket string; the value column holds JSON bytes and the
// indexed expiry column carries the TTL deadline. Validation mirrors the
// previous backend so plugin-visible shapes stay identical.
func (service *Service) SetStorageSQLite(ctx context.Context, pluginID string, scope string, key string, value any, timeToLive time.Duration) error {
	if err := requireStorageKey(scope, key); err != nil {
		service.logStorageStoreFailed(pluginID, scope, key, err)
		return err
	}
	if timeToLive < 0 {
		storeError := fmt.Errorf("storage ttl must not be negative")
		service.logStorageStoreFailed(pluginID, scope, key, storeError)
		return storeError
	}
	if timeToLive > maxStorageTTL {
		storeError := fmt.Errorf("storage ttl exceeds 90 days")
		service.logStorageStoreFailed(pluginID, scope, key, storeError)
		return storeError
	}
	if err := service.requireHandle(); err != nil {
		service.logStorageStoreFailed(pluginID, scope, key, err)
		return err
	}
	encoded, err := storageValueToBytes(value)
	if err != nil {
		service.logStorageStoreFailed(pluginID, scope, key, err)
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
	target, other := service.database, service.cache
	if timeToLive > 0 {
		target, other = other, target
	}
	if err := target.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		rows := []record.PluginStorage{storageRow}
		if err := gorm.G[record.PluginStorage](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "plugin_id"}, {Name: "scope"}, {Name: "key"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return db.Translate(err)
		}
		return nil
	}); err != nil {
		service.logStorageStoreFailed(pluginID, scope, key, err)
		return err
	}
	if err := other.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		_, err := gorm.G[record.PluginStorage](transaction, clause.Eq{Column: "plugin_id", Value: pluginID}, clause.Eq{Column: "scope", Value: scope}, clause.Eq{Column: "key", Value: key}).Delete(ctx)
		return db.Translate(err)
	}); err != nil {
		service.logStorageStoreFailed(pluginID, scope, key, err)
		return err
	}
	service.logStorageStored(pluginID, scope, key, timeToLive)
	return nil
}

// GetStorageSQLite returns one plugin storage entry in one transaction per
// handle. The durable database answers first; the cache answers second with
// the expiry check. Expired rows read as missing and are removed in the
// same transaction, matching the previous envelope behavior. A miss returns
// nil without an error.
func (service *Service) GetStorageSQLite(ctx context.Context, pluginID string, scope string, key string) (any, error) {
	if err := requireStorageKey(scope, key); err != nil {
		service.logStorageLoadFailed(pluginID, scope, key, err)
		return nil, err
	}
	if err := service.requireHandle(); err != nil {
		service.logStorageLoadFailed(pluginID, scope, key, err)
		return nil, err
	}
	conditions := []clause.Expression{clause.Eq{Column: "plugin_id", Value: pluginID}, clause.Eq{Column: "scope", Value: scope}, clause.Eq{Column: "key", Value: key}}
	if row, err := gorm.G[record.PluginStorage](service.database, conditions...).First(ctx); err == nil {
		value, decodeError := storageValueFromBytes(row.Value)
		if decodeError != nil {
			service.logStorageLoadFailed(pluginID, scope, key, decodeError)
			return nil, decodeError
		}
		service.logStorageLoaded(pluginID, scope, key)
		return value, nil
	} else if translated := db.Translate(err); !stdErrors.Is(translated, apierrors.ErrNotFound) {
		service.logStorageLoadFailed(pluginID, scope, key, translated)
		return nil, translated
	}
	var result any
	if err := service.cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		storageRow, err := gorm.G[record.PluginStorage](transaction, conditions...).First(ctx)
		if err != nil {
			translated := db.Translate(err)
			if stdErrors.Is(translated, apierrors.ErrNotFound) {
				result = nil
				return nil
			}
			return translated
		}
		if storageRow.ExpiresAt != nil && !util.Now().Before(*storageRow.ExpiresAt) {
			if _, err := gorm.G[record.PluginStorage](transaction, conditions...).Delete(ctx); err != nil {
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
		service.logStorageLoadFailed(pluginID, scope, key, err)
		return nil, err
	}
	if result == nil {
		service.logStorageMissing(pluginID, scope, key)
		return nil, nil
	}
	service.logStorageLoaded(pluginID, scope, key)
	return result, nil
}

// DeleteStorageSQLite removes one plugin storage entry from both handles in
// one transaction each. A miss is not an error, matching the previous
// delete.
func (service *Service) DeleteStorageSQLite(ctx context.Context, pluginID string, scope string, key string) error {
	if err := requireStorageKey(scope, key); err != nil {
		service.logStorageRemoveFailed(pluginID, scope, key, err)
		return err
	}
	if err := service.requireHandle(); err != nil {
		service.logStorageRemoveFailed(pluginID, scope, key, err)
		return err
	}
	for _, handle := range []*gorm.DB{service.database, service.cache} {
		if err := handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			_, err := gorm.G[record.PluginStorage](transaction, clause.Eq{Column: "plugin_id", Value: pluginID}, clause.Eq{Column: "scope", Value: scope}, clause.Eq{Column: "key", Value: key}).Delete(ctx)
			return db.Translate(err)
		}); err != nil {
			service.logStorageRemoveFailed(pluginID, scope, key, err)
			return err
		}
	}
	service.logStorageRemoved(pluginID, scope, key)
	return nil
}

// DeletePluginStorageSQLite removes every storage entry for one plugin from
// both handles in one transaction each. Durable deletes cascade from the
// base row; this helper serves explicit storage resets plus the cache
// slice, which references the plugin softly.
func (service *Service) DeletePluginStorageSQLite(ctx context.Context, pluginID string) error {
	if err := service.requireHandle(); err != nil {
		return err
	}
	for _, handle := range []*gorm.DB{service.database, service.cache} {
		if err := handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			_, err := gorm.G[record.PluginStorage](transaction, clause.Eq{Column: "plugin_id", Value: pluginID}).Delete(ctx)
			return db.Translate(err)
		}); err != nil {
			return err
		}
	}
	return nil
}

// PruneExpiredStorageSQLite removes every expired storage entry from the
// cache handle in one transaction and returns the removed count. The
// indexed expiry column drives the condition; null expiries live in the
// durable database and never match.
func (service *Service) PruneExpiredStorageSQLite(ctx context.Context) (int64, error) {
	if err := service.requireHandle(); err != nil {
		return 0, err
	}
	var removed int64
	if err := service.cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
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

func storageLogKeys(pluginID, scope, key, operation string) []any {
	return []any{"plugin_id", pluginID, "operation", operation, "scope", scope, "key", key}
}

func (service *Service) logStorageStored(pluginID, scope, key string, timeToLive time.Duration) {
	if service.logger == nil {
		return
	}
	arguments := append(storageLogKeys(pluginID, scope, key, "set"), "wait", int64(timeToLive/time.Second))
	service.logger.Debug("Plugin storage entry stored", arguments...)
}

func (service *Service) logStorageStoreFailed(pluginID, scope, key string, storeError error) {
	if service.logger == nil || storeError == nil {
		return
	}
	arguments := append(storageLogKeys(pluginID, scope, key, "set"), "error", storeError.Error())
	service.logger.Error("Plugin storage entry store failed", arguments...)
}

func (service *Service) logStorageLoaded(pluginID, scope, key string) {
	if service.logger == nil {
		return
	}
	service.logger.Debug("Plugin storage entry loaded", storageLogKeys(pluginID, scope, key, "get")...)
}

func (service *Service) logStorageMissing(pluginID, scope, key string) {
	if service.logger == nil {
		return
	}
	service.logger.Debug("Plugin storage entry missing", storageLogKeys(pluginID, scope, key, "get")...)
}

func (service *Service) logStorageLoadFailed(pluginID, scope, key string, loadError error) {
	if service.logger == nil || loadError == nil {
		return
	}
	arguments := append(storageLogKeys(pluginID, scope, key, "get"), "error", loadError.Error())
	service.logger.Error("Plugin storage entry load failed", arguments...)
}

func (service *Service) logStorageRemoved(pluginID, scope, key string) {
	if service.logger == nil {
		return
	}
	service.logger.Debug("Plugin storage entry removed", storageLogKeys(pluginID, scope, key, "delete")...)
}

func (service *Service) logStorageRemoveFailed(pluginID, scope, key string, removeError error) {
	if service.logger == nil || removeError == nil {
		return
	}
	arguments := append(storageLogKeys(pluginID, scope, key, "delete"), "error", removeError.Error())
	service.logger.Error("Plugin storage entry remove failed", arguments...)
}
