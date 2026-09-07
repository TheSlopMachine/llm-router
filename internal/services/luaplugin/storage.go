package luaplugin

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/db"
	bolt "go.etcd.io/bbolt"
	lua "github.com/yuin/gopher-lua"
)

// storageBackend persists llm_router.storage.* entries in BucketPluginStorage
// under the composite key pluginID + "\x00" + scope + "\x00" + key.
type storageBackend struct {
	database *db.DB
}

func newStorageBackend(database *db.DB) *storageBackend {
	return &storageBackend{database: database}
}

func storageKey(pluginID, scope, key string) string {
	return pluginID + "\x00" + scope + "\x00" + key
}

func (b *storageBackend) set(pluginID, scope, key string, value any) error {
	if strings.TrimSpace(scope) == "" {
		return fmt.Errorf("storage scope is required")
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("storage key is required")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal storage value: %w", err)
	}
	return b.database.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(db.BucketPluginStorage)
		if bkt == nil {
			return fmt.Errorf("bucket %q not found", string(db.BucketPluginStorage))
		}
		return bkt.Put([]byte(storageKey(pluginID, scope, key)), raw)
	})
}

func (b *storageBackend) get(pluginID, scope, key string) (any, error) {
	if strings.TrimSpace(scope) == "" {
		return nil, fmt.Errorf("storage scope is required")
	}
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("storage key is required")
	}
	var raw []byte
	err := b.database.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(db.BucketPluginStorage)
		if bkt == nil {
			return nil
		}
		v := bkt.Get([]byte(storageKey(pluginID, scope, key)))
		if v != nil {
			raw = append([]byte(nil), v...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode storage value: %w", err)
	}
	return out, nil
}

func (b *storageBackend) delete(pluginID, scope, key string) error {
	if strings.TrimSpace(scope) == "" {
		return fmt.Errorf("storage scope is required")
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("storage key is required")
	}
	return b.database.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(db.BucketPluginStorage)
		if bkt == nil {
			return nil
		}
		return bkt.Delete([]byte(storageKey(pluginID, scope, key)))
	})
}

func (b *storageBackend) deletePlugin(pluginID string) error {
	prefix := pluginID + "\x00"
	return b.database.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(db.BucketPluginStorage)
		if bkt == nil {
			return nil
		}
		c := bkt.Cursor()
		var toDelete [][]byte
		for k, _ := c.Seek([]byte(prefix)); k != nil && strings.HasPrefix(string(k), prefix); k, _ = c.Next() {
			toDelete = append(toDelete, append([]byte(nil), k...))
		}
		for _, k := range toDelete {
			if err := bkt.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

func newStorageTable(L *lua.LState, ctx *execContext) *lua.LTable {
	tbl := L.NewTable()
	tbl.RawSetString("set", L.NewFunction(func(L *lua.LState) int {
		scope := L.CheckString(1)
		key := L.CheckString(2)
		val := fromLuaValue(L.Get(3))
		if ctx == nil || ctx.storage == nil {
			L.RaiseError("llm_router.storage: no execution context")
			return 0
		}
		if err := ctx.storage.set(ctx.pluginID, scope, key, val); err != nil {
			L.RaiseError("llm_router.storage.set: %s", err.Error())
			return 0
		}
		L.Push(lua.LTrue)
		return 1
	}))
	tbl.RawSetString("get", L.NewFunction(func(L *lua.LState) int {
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
	tbl.RawSetString("delete", L.NewFunction(func(L *lua.LState) int {
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
	return tbl
}
