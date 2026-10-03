package luaplugin

import (
	"errors"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// checkAutomation enforces the provider automation switch on shared
// disable/enable writes. Off (or unreadable) denies loudly so plugins fall
// back to private KV parking instead of silently skipping the write.
func (ctx *execContext) checkAutomation(op string) error {
	if ctx == nil {
		return errors.New("no execution context")
	}
	if ctx.automationOn == nil || !ctx.automationOn(ctx.providerID) {
		return errors.New("automation disabled for provider: enable \"Disable failing credentials\" or park the credential instead")
	}
	return nil
}

// newCredentialsTable builds the llm_router.credentials query interface.
// Installed only when the type key serves credentials. list/get serve
// every context; update serves job contexts only and fails elsewhere.
func newCredentialsTable(L *lua.LState, ctx *execContext) *lua.LTable {
	tbl := L.NewTable()
	tbl.RawSetString("list", L.NewFunction(func(L *lua.LState) int {
		if ctx == nil {
			L.RaiseError("llm_router.credentials: no execution context")
			return 0
		}
		if ctx.credList == nil {
			L.RaiseError("llm_router.credentials.list: credential store is not wired")
			return 0
		}
		creds, err := ctx.credList(ctx.providerID)
		if err != nil {
			L.RaiseError("llm_router.credentials.list: %s", err.Error())
			return 0
		}
		out := L.NewTable()
		for _, c := range creds {
			if c == nil || c.Disabled {
				continue
			}
			if !credentialAllowed(ctx, c.ID) {
				continue
			}
			if ctx.credParked != nil {
				if p, _ := ctx.credParked(c.ID); p != nil {
					continue
				}
			}
			out.Append(credTable(L, c))
		}
		L.Push(out)
		return 1
	}))
	tbl.RawSetString("get", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		if ctx == nil {
			L.RaiseError("llm_router.credentials: no execution context")
			return 0
		}
		if ctx.credGet == nil {
			L.RaiseError("llm_router.credentials.get: credential store is not wired")
			return 0
		}
		cred, err := ctx.credGet(id)
		if err != nil || cred == nil || cred.Disabled || !credentialAllowed(ctx, cred.ID) {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(credTable(L, cred))
		return 1
	}))
	tbl.RawSetString("update", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		dataTbl := L.CheckTable(2)
		if ctx == nil {
			L.RaiseError("llm_router.credentials: no execution context")
			return 0
		}
		if !ctx.allowCredentialWrite {
			L.RaiseError("llm_router.credentials.update: allowed in job contexts only")
			return 0
		}
		if ctx.credUpdate == nil {
			L.RaiseError("llm_router.credentials.update: credential store is not wired")
			return 0
		}
		data, err := fromLuaValue(dataTbl)
		if err != nil {
			L.RaiseError("llm_router.credentials.update: %s", err.Error())
			return 0
		}
		dataMap, ok := data.(map[string]any)
		if !ok {
			L.RaiseError("llm_router.credentials.update: data must be a table")
			return 0
		}
		if uerr := ctx.credUpdate(id, dataMap); uerr != nil {
			L.RaiseError("llm_router.credentials.update: %s", uerr.Error())
			return 0
		}
		L.Push(lua.LTrue)
		return 1
	}))
	tbl.RawSetString("disable", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		reason := ""
		if v := L.Get(2); v != lua.LNil {
			s, ok := v.(lua.LString)
			if !ok {
				L.RaiseError("llm_router.credentials.disable: reason must be a string")
				return 0
			}
			reason = string(s)
		}
		if ctx == nil {
			L.RaiseError("llm_router.credentials: no execution context")
			return 0
		}
		if err := ctx.checkAutomation("disable"); err != nil {
			L.RaiseError("llm_router.credentials.disable: %s", err.Error())
			return 0
		}
		if ctx.credDisable == nil {
			L.RaiseError("llm_router.credentials.disable: credential store is not wired")
			return 0
		}
		if uerr := ctx.credDisable(id, reason); uerr != nil {
			L.RaiseError("llm_router.credentials.disable: %s", uerr.Error())
			return 0
		}
		L.Push(lua.LTrue)
		return 1
	}))
	tbl.RawSetString("enable", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		if ctx == nil {
			L.RaiseError("llm_router.credentials: no execution context")
			return 0
		}
		if err := ctx.checkAutomation("enable"); err != nil {
			L.RaiseError("llm_router.credentials.enable: %s", err.Error())
			return 0
		}
		if ctx.credEnable == nil {
			L.RaiseError("llm_router.credentials.enable: credential store is not wired")
			return 0
		}
		if uerr := ctx.credEnable(id); uerr != nil {
			L.RaiseError("llm_router.credentials.enable: %s", uerr.Error())
			return 0
		}
		L.Push(lua.LTrue)
		return 1
	}))
	tbl.RawSetString("park", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		secs, ok := L.Get(2).(lua.LNumber)
		if !ok {
			L.RaiseError("llm_router.credentials.park: ttl must be a number of seconds")
			return 0
		}
		reason := ""
		if v := L.Get(3); v != lua.LNil {
			s, ok := v.(lua.LString)
			if !ok {
				L.RaiseError("llm_router.credentials.park: reason must be a string")
				return 0
			}
			reason = string(s)
		}
		if ctx == nil {
			L.RaiseError("llm_router.credentials: no execution context")
			return 0
		}
		if ctx.credPark == nil {
			L.RaiseError("llm_router.credentials.park: credential store is not wired")
			return 0
		}
		if uerr := ctx.credPark(id, time.Duration(float64(secs))*time.Second, reason); uerr != nil {
			L.RaiseError("llm_router.credentials.park: %s", uerr.Error())
			return 0
		}
		L.Push(lua.LTrue)
		return 1
	}))
	tbl.RawSetString("unpark", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		if ctx == nil {
			L.RaiseError("llm_router.credentials: no execution context")
			return 0
		}
		if ctx.credUnpark == nil {
			L.RaiseError("llm_router.credentials.unpark: credential store is not wired")
			return 0
		}
		if uerr := ctx.credUnpark(id); uerr != nil {
			L.RaiseError("llm_router.credentials.unpark: %s", uerr.Error())
			return 0
		}
		L.Push(lua.LTrue)
		return 1
	}))
	tbl.RawSetString("parked", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		if ctx == nil {
			L.RaiseError("llm_router.credentials: no execution context")
			return 0
		}
		if ctx.credParked == nil {
			L.RaiseError("llm_router.credentials.parked: credential store is not wired")
			return 0
		}
		entry, uerr := ctx.credParked(id)
		if uerr != nil {
			L.RaiseError("llm_router.credentials.parked: %s", uerr.Error())
			return 0
		}
		if entry == nil {
			L.Push(lua.LNil)
			return 1
		}
		out := L.NewTable()
		out.RawSetString("reason", lua.LString(entry.Reason))
		out.RawSetString("until", lua.LNumber(entry.Until.UTC().Unix()))
		L.Push(out)
		return 1
	}))
	return tbl
}

// newProxiesTable builds the read-only llm_router.proxies query interface.
// Installed only when the type key serves proxy settings.
func newProxiesTable(L *lua.LState, ctx *execContext) *lua.LTable {
	tbl := L.NewTable()
	tbl.RawSetString("query", L.NewFunction(func(L *lua.LState) int {
		var pool, country string
		limit := 0
		if arg := L.Get(1); arg != lua.LNil {
			opts, ok := arg.(*lua.LTable)
			if !ok {
				L.RaiseError("llm_router.proxies.query: options must be a table")
				return 0
			}
			if v := opts.RawGetString("pool"); v != lua.LNil {
				s, ok := v.(lua.LString)
				if !ok {
					L.RaiseError("llm_router.proxies.query: pool must be a string")
					return 0
				}
				pool = string(s)
			}
			if v := opts.RawGetString("country"); v != lua.LNil {
				s, ok := v.(lua.LString)
				if !ok {
					L.RaiseError("llm_router.proxies.query: country must be a string")
					return 0
				}
				country = string(s)
			}
			if v := opts.RawGetString("limit"); v != lua.LNil {
				n, ok := v.(lua.LNumber)
				if !ok {
					L.RaiseError("llm_router.proxies.query: limit must be a number")
					return 0
				}
				limit = int(n)
			}
		}
		if ctx == nil {
			L.RaiseError("llm_router.proxies: no execution context")
			return 0
		}
		if ctx.proxyQuery == nil {
			L.RaiseError("llm_router.proxies.query: proxy pool is not wired")
			return 0
		}
		views, err := ctx.proxyQuery(pool, country, limit)
		if err != nil {
			L.RaiseError("llm_router.proxies.query: %s", err.Error())
			return 0
		}
		out := L.NewTable()
		for _, v := range views {
			row := L.NewTable()
			row.RawSetString("id", lua.LString(v.ID))
			row.RawSetString("url", lua.LString(v.URL))
			if v.Country != "" {
				row.RawSetString("country", lua.LString(v.Country))
			}
			row.RawSetString("pool", lua.LString(v.Pool))
			out.Append(row)
		}
		L.Push(out)
		return 1
	}))
	return tbl
}

// credentialAllowed enforces the router-token allow-list: nil means
// unrestricted (admin probes), otherwise only listed IDs serve.
func credentialAllowed(ctx *execContext, id string) bool {
	if ctx == nil || ctx.allowedCredentials == nil {
		return true
	}
	for _, allowed := range ctx.allowedCredentials {
		if allowed == id {
			return true
		}
	}
	return false
}
