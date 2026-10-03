package luaplugin

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	lua "github.com/yuin/gopher-lua"
	bolt "go.etcd.io/bbolt"
)

// maxStorageTTL bounds entry lifetimes to 90 days.
const maxStorageTTL = 90 * 24 * time.Hour

// storageBackend persists llm_router.storage.* entries in BucketPluginStorage
// under the composite key pluginID + "\x00" + scope + "\x00" + key.
// Values carry an optional expiry: expired rows read as missing and are
// removed on read.
type storageBackend struct {
	database *db.DB
}

func newStorageBackend(database *db.DB) *storageBackend {
	return &storageBackend{database: database}
}

func storageKey(pluginID, scope, key string) string {
	return pluginID + "\x00" + scope + "\x00" + key
}

// ParseStorageKey splits a storage key into plugin ID, scope and key.
// It reports false when the key was not built by storageKey, so callers
// (e.g. database inspection) never reimplement the separator.
func ParseStorageKey(raw string) (pluginID, scope, key string, ok bool) {
	parts := strings.SplitN(raw, "\x00", 3)
	if len(parts) != 3 || parts[0] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// storageEnvelope wraps every stored value with an optional expiry.
type storageEnvelope struct {
	Value     any        `json:"value"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (b *storageBackend) set(pluginID, scope, key string, value any, ttl time.Duration) error {
	if strings.TrimSpace(scope) == "" {
		return fmt.Errorf("storage scope is required")
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("storage key is required")
	}
	if ttl < 0 {
		return fmt.Errorf("storage ttl must not be negative")
	}
	if ttl > maxStorageTTL {
		return fmt.Errorf("storage ttl exceeds 90 days")
	}
	env := storageEnvelope{Value: value}
	if ttl > 0 {
		exp := time.Now().UTC().Add(ttl)
		env.ExpiresAt = &exp
	}
	raw, err := json.Marshal(env)
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
	var shape map[string]any
	if err := json.Unmarshal(raw, &shape); err != nil || shape == nil {
		// Pre-0.7.0 rows hold bare scalars: serve them without expiry.
		var bare any
		if berr := json.Unmarshal(raw, &bare); berr != nil {
			return nil, fmt.Errorf("decode storage value: %w", err)
		}
		return bare, nil
	}
	if _, ok := shape["value"]; !ok {
		// Pre-0.7.0 rows hold bare objects: serve them without expiry.
		var bare any
		if berr := json.Unmarshal(raw, &bare); berr != nil {
			return nil, fmt.Errorf("decode storage value: %w", err)
		}
		return bare, nil
	}
	var env storageEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode storage value: %w", err)
	}
	if env.ExpiresAt != nil && !time.Now().UTC().Before(*env.ExpiresAt) {
		_ = b.delete(pluginID, scope, key)
		return nil, nil
	}
	return env.Value, nil
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
		val, verr := fromLuaValue(L.Get(3))
		if verr != nil {
			L.RaiseError("llm_router.storage.set: %s", verr.Error())
			return 0
		}
		var ttl time.Duration
		if opts := L.Get(4); opts != lua.LNil {
			optsTbl, ok := opts.(*lua.LTable)
			if !ok {
				L.RaiseError("llm_router.storage.set: options must be a table")
				return 0
			}
			if tv := optsTbl.RawGetString("ttl"); tv != lua.LNil {
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
