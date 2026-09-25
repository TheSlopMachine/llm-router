package luaplugin

import (
	"errors"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func newLuaState(t *testing.T) *lua.LState {
	t.Helper()
	return lua.NewState()
}

func TestFromLuaValueScalars(t *testing.T) {
	L := newLuaState(t)
	defer L.Close()
	cases := []struct {
		value lua.LValue
		want  any
	}{
		{lua.LNil, nil},
		{lua.LBool(true), true},
		{lua.LString("x"), "x"},
		{lua.LNumber(3), float64(3)},
	}
	for _, tc := range cases {
		got, err := fromLuaValue(tc.value)
		if err != nil {
			t.Fatalf("value %v: %v", tc.value, err)
		}
		if got != tc.want {
			t.Errorf("value %v: got %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestFromLuaValueRejectsFunctions(t *testing.T) {
	L := newLuaState(t)
	defer L.Close()
	if _, err := fromLuaValue(L.NewFunction(func(L *lua.LState) int { return 0 })); !errors.Is(err, ErrUnsupportedLuaType) {
		t.Fatalf("expected ErrUnsupportedLuaType, got %v", err)
	}
}

func TestTableToGoRejectsSparseArray(t *testing.T) {
	L := newLuaState(t)
	defer L.Close()
	tbl := L.NewTable()
	tbl.RawSetInt(1, lua.LString("a"))
	tbl.RawSetInt(3, lua.LString("c"))
	if _, err := fromLuaValue(tbl); !errors.Is(err, ErrSparseArray) {
		t.Fatalf("expected ErrSparseArray, got %v", err)
	}
}

func TestTableToGoRejectsMixedTable(t *testing.T) {
	L := newLuaState(t)
	defer L.Close()
	tbl := L.NewTable()
	tbl.Append(lua.LString("a"))
	tbl.RawSetString("extra", lua.LString("b"))
	if _, err := fromLuaValue(tbl); !errors.Is(err, ErrMixedTable) {
		t.Fatalf("expected ErrMixedTable, got %v", err)
	}
}

func TestTableToGoRejectsNonStringKey(t *testing.T) {
	L := newLuaState(t)
	defer L.Close()
	tbl := L.NewTable()
	tbl.RawSet(lua.LNumber(1.5), lua.LString("x"))
	if _, err := fromLuaValue(tbl); !errors.Is(err, ErrNonStringKey) {
		t.Fatalf("expected ErrNonStringKey, got %v", err)
	}
}

func TestMarshalLuaSurfacesConversionError(t *testing.T) {
	L := newLuaState(t)
	defer L.Close()
	tbl := L.NewTable()
	tbl.RawSetString("fn", L.NewFunction(func(L *lua.LState) int { return 0 }))
	if _, err := marshalLua(tbl); !errors.Is(err, ErrUnsupportedLuaType) {
		t.Fatalf("expected ErrUnsupportedLuaType, got %v", err)
	}
}

func TestTableToGoRoundTrip(t *testing.T) {
	L := newLuaState(t)
	defer L.Close()
	tbl := L.NewTable()
	tbl.RawSetString("model", lua.LString("gpt-5"))
	arr := L.NewTable()
	arr.Append(lua.LString("a"))
	arr.Append(lua.LNumber(2))
	tbl.RawSetString("input", arr)
	got, err := fromLuaValue(tbl)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok || m["model"] != "gpt-5" {
		t.Fatalf("object: got %#v", got)
	}
	items, ok := m["input"].([]any)
	if !ok || len(items) != 2 || items[0] != "a" || items[1] != float64(2) {
		t.Fatalf("array: got %#v", m["input"])
	}
}
