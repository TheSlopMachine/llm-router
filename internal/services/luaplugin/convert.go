package luaplugin

import (
	"encoding/json"
	"fmt"
	"sort"

	lua "github.com/yuin/gopher-lua"
)

// toLuaValue converts JSON-compatible Go values into Lua values.
// Maps become string-keyed tables, slices become 1-based array tables.
func toLuaValue(L *lua.LState, v any) lua.LValue {
	switch t := v.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(t)
	case string:
		return lua.LString(t)
	case float64:
		return lua.LNumber(t)
	case float32:
		return lua.LNumber(float64(t))
	case int:
		return lua.LNumber(t)
	case int64:
		return lua.LNumber(t)
	case int32:
		return lua.LNumber(t)
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return lua.LNumber(f)
		}
		return lua.LString(t.String())
	case map[string]any:
		tbl := L.NewTable()
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			tbl.RawSetString(k, toLuaValue(L, t[k]))
		}
		return tbl
	case []any:
		tbl := L.NewTable()
		for _, item := range t {
			tbl.Append(toLuaValue(L, item))
		}
		return tbl
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return lua.LNil
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return lua.LNil
		}
		return toLuaValue(L, decoded)
	}
}

// fromLuaValue converts a Lua value into JSON-compatible Go values.
// Tables with only consecutive 1-based integer keys become []any,
// all other tables become map[string]any. Non-string keys are stringified.
func fromLuaValue(v lua.LValue) any {
	switch t := v.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return bool(t)
	case lua.LString:
		return string(t)
	case lua.LNumber:
		return float64(t)
	case *lua.LTable:
		return tableToGo(t)
	default:
		return nil
	}
}

func tableToGo(tbl *lua.LTable) any {
	if tbl == nil {
		return nil
	}
	n := tbl.Len()
	if n == 0 {
		empty := true
		obj := map[string]any{}
		var convErr error
		tbl.ForEach(func(k, v lua.LValue) {
			if convErr != nil {
				return
			}
			empty = false
			ks, ok := k.(lua.LString)
			if !ok {
				convErr = fmt.Errorf("non-string table key of type %s", k.Type().String())
				return
			}
			obj[string(ks)] = fromLuaValue(v)
		})
		if convErr != nil {
			return map[string]any{}
		}
		if empty {
			return map[string]any{}
		}
		return obj
	}
	// Array-like fast path, but verify every index 1..n exists and
	// reject mixed tables (array part plus string keys).
	arr := make([]any, 0, n)
	for i := 1; i <= n; i++ {
		v := tbl.RawGetInt(i)
		if v == lua.LNil {
			return map[string]any{}
		}
		arr = append(arr, fromLuaValue(v))
	}
	mixed := false
	tbl.ForEach(func(k, _ lua.LValue) {
		if _, ok := k.(lua.LNumber); !ok {
			mixed = true
		}
	})
	if mixed {
		obj := map[string]any{}
		tbl.ForEach(func(k, v lua.LValue) {
			ks, ok := k.(lua.LString)
			if !ok {
				return
			}
			obj[string(ks)] = fromLuaValue(v)
		})
		return obj
	}
	return arr
}

// marshalLua encodes a Lua value to JSON via the Go intermediate form.
func marshalLua(v lua.LValue) ([]byte, error) {
	return json.Marshal(fromLuaValue(v))
}

// marshalGoJSON encodes a Go value to JSON.
func marshalGoJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// unmarshalTo unmarshals JSON bytes into target.
func unmarshalTo(raw []byte, target any) error {
	return json.Unmarshal(raw, target)
}

// goToLuaJSON marshals a Go value to JSON, decodes it generically and
// pushes the result as a Lua value. The JSON round trip normalizes
// struct tags and omitempty behavior.
func goToLuaJSON(L *lua.LState, v any) lua.LValue {
	raw, err := json.Marshal(v)
	if err != nil {
		return lua.LNil
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return lua.LNil
	}
	if decoded == nil {
		return lua.LNil
	}
	return toLuaValue(L, decoded)
}

// luaToGo decodes stack value at idx into target via JSON round trip.
func luaToGo(L *lua.LState, idx int, target any) error {
	v := L.Get(idx)
	raw, err := marshalLua(v)
	if err != nil {
		return fmt.Errorf("encode lua value: %w", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode lua value: %w", err)
	}
	return nil
}

// luaValueToMap decodes a Lua table at idx into map[string]any.
func luaValueToMap(L *lua.LState, idx int) (map[string]any, error) {
	v := L.Get(idx)
	if v == lua.LNil {
		return map[string]any{}, nil
	}
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("expected table, got %s", v.Type().String())
	}
	raw, err := marshalLua(tbl)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
