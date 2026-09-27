package luaplugin

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

func contractTable(t *testing.T, fields map[string]lua.LValue) lua.LValue {
	t.Helper()
	L := lua.NewState()
	defer L.Close()
	tbl := L.NewTable()
	for k, v := range fields {
		tbl.RawSetString(k, v)
	}
	return tbl
}

func num(n int64) lua.LValue { return lua.LNumber(n) }

func TestAsProviderErrorStrict(t *testing.T) {
	future := int64(4100000000)
	t.Run("new types parse", func(t *testing.T) {
		for typ, want := range map[string]models.ErrorType{
			"content_policy":    models.ErrorTypeContentPolicy,
			"model_unavailable": models.ErrorTypeModelUnavailable,
			"structural_fault":  models.ErrorTypeStructuralFault,
			"upstream":          models.ErrorTypeUpstream,
			"geo":               models.ErrorTypeGeo,
		} {
			L := lua.NewState()
			defer L.Close()
			tbl := L.NewTable()
			tbl.RawSetString("type", lua.LString(typ))
			tbl.RawSetString("message", lua.LString("m"))
			perr, ok := asProviderError(tbl)
			if !ok || perr.Type != want {
				t.Fatalf("%s: ok=%v perr=%v", typ, ok, perr)
			}
		}
	})
	t.Run("timeout rejected", func(t *testing.T) {
		if _, ok := asProviderError(contractTable(t, map[string]lua.LValue{
			"type": lua.LString("timeout"), "message": lua.LString("m"),
		})); ok {
			t.Fatal("legacy timeout must be invalid")
		}
	})
	t.Run("rate and quota require future retry_after", func(t *testing.T) {
		for _, typ := range []string{"rate_limit", "quota_exceeded"} {
			if _, ok := asProviderError(contractTable(t, map[string]lua.LValue{
				"type": lua.LString(typ), "message": lua.LString("m"),
			})); ok {
				t.Fatalf("%s without retry_after must be invalid", typ)
			}
			if _, ok := asProviderError(contractTable(t, map[string]lua.LValue{
				"type": lua.LString(typ), "message": lua.LString("m"),
				"retry_after": num(1700000000),
			})); ok {
				t.Fatalf("%s with past retry_after must be invalid", typ)
			}
			perr, ok := asProviderError(contractTable(t, map[string]lua.LValue{
				"type": lua.LString(typ), "message": lua.LString("m"),
				"retry_after": num(future),
			}))
			if !ok || perr.RetryAfter == nil {
				t.Fatalf("%s with future retry_after must parse", typ)
			}
		}
	})
	t.Run("retry_after forbidden elsewhere", func(t *testing.T) {
		for _, typ := range []string{"auth", "upstream", "geo", "model_unavailable", "structural_fault", "content_policy", "invalid_request", "not_found", "payment_required"} {
			if _, ok := asProviderError(contractTable(t, map[string]lua.LValue{
				"type": lua.LString(typ), "message": lua.LString("m"),
				"retry_after": num(future),
			})); ok {
				t.Fatalf("%s with retry_after must be invalid", typ)
			}
		}
	})
	t.Run("scope forbidden outside rate and quota", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()
		scope := L.NewTable()
		scope.Append(lua.LString("account"))
		if _, ok := asProviderError(contractTable(t, map[string]lua.LValue{
			"type": lua.LString("auth"), "message": lua.LString("m"),
			"scope": scope,
		})); ok {
			t.Fatal("auth with scope must be invalid")
		}
	})
	t.Run("quota proxy scope rejected", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()
		scope := L.NewTable()
		scope.Append(lua.LString("proxy"))
		if _, ok := asProviderError(contractTable(t, map[string]lua.LValue{
			"type": lua.LString("quota_exceeded"), "message": lua.LString("m"),
			"retry_after": num(future), "scope": scope,
		})); ok {
			t.Fatal("quota with proxy scope must be invalid")
		}
	})
	t.Run("empty message rejected", func(t *testing.T) {
		if _, ok := asProviderError(contractTable(t, map[string]lua.LValue{
			"type": lua.LString("upstream"), "message": lua.LString(""),
		})); ok {
			t.Fatal("empty message must be invalid")
		}
	})
	t.Run("upstream passthrough", func(t *testing.T) {
		perr, ok := asProviderError(contractTable(t, map[string]lua.LValue{
			"type": lua.LString("upstream"), "message": lua.LString("m"),
			"upstream_status": num(500), "upstream_body": lua.LString("body"),
		}))
		if !ok || perr.UpstreamStatus != 500 || perr.UpstreamBody != "body" {
			t.Fatalf("upstream fields must pass through: %v", perr)
		}
	})
}
