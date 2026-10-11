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

// SetStorageSQLite persists one plugin storage entry on the SQLite path in
// one transaction. The composite primary key replaces the NUL-separated
// bucket string; the value column holds JSON bytes and the indexed expiry
// column carries the TTL deadline. Validation mirrors the previous backend
// so plugin-visible shapes stay identical.
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
	if err := service.requireHandle(); err != nil {
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
	return service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		rows := []record.PluginStorage{storageRow}
		if err := gorm.G[record.PluginStorage](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "plugin_id"}, {Name: "scope"}, {Name: "key"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return db.Translate(err)
		}
		return nil
	})
}

// GetStorageSQLite returns one plugin storage entry on the SQLite path in
// one transaction. Expired rows read as missing and are removed in the same
// transaction, matching the previous envelope behavior. A miss returns nil
// without an error.
func (service *Service) GetStorageSQLite(ctx context.Context, pluginID string, scope string, key string) (any, error) {
	if err := requireStorageKey(scope, key); err != nil {
		return nil, err
	}
	if err := service.requireHandle(); err != nil {
		return nil, err
	}
	var result any
	if err := service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
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
// in one transaction. A miss is not an error, matching the previous delete.
func (service *Service) DeleteStorageSQLite(ctx context.Context, pluginID string, scope string, key string) error {
	if err := requireStorageKey(scope, key); err != nil {
		return err
	}
	if err := service.requireHandle(); err != nil {
		return err
	}
	return service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		_, err := gorm.G[record.PluginStorage](transaction, clause.Eq{Column: "plugin_id", Value: pluginID}, clause.Eq{Column: "scope", Value: scope}, clause.Eq{Column: "key", Value: key}).Delete(ctx)
		return db.Translate(err)
	})
}

// DeletePluginStorageSQLite removes every storage entry for one plugin on
// the SQLite path in one transaction. Plugin deletes cascade from the base
// row; this helper serves explicit storage resets.
func (service *Service) DeletePluginStorageSQLite(ctx context.Context, pluginID string) error {
	if err := service.requireHandle(); err != nil {
		return err
	}
	return service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		_, err := gorm.G[record.PluginStorage](transaction, clause.Eq{Column: "plugin_id", Value: pluginID}).Delete(ctx)
		return db.Translate(err)
	})
}

// PruneExpiredStorageSQLite removes every expired storage entry on the
// SQLite path in one transaction and returns the removed count. The indexed
// expiry column drives the condition; null expiries never match the
// less-than comparison and stay.
func (service *Service) PruneExpiredStorageSQLite(ctx context.Context) (int64, error) {
	if err := service.requireHandle(); err != nil {
		return 0, err
	}
	var removed int64
	if err := service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
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
