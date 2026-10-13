package luaplugin

import (
	"context"
	"errors"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// checkAutomation enforces the provider automation switch on shared
// disable/enable writes. Off (or unreadable) denies loudly so plugins fall
// back to private KV parking instead of silently skipping the write.
func (ctx *executionContext) checkAutomation(op string) error {
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
func newCredentialsTable(L *lua.LState, ctx *executionContext) *lua.LTable {
	table := L.NewTable()
	table.RawSetString("list", L.NewFunction(func(L *lua.LState) int {
		if ctx == nil {
			L.RaiseError("llm_router.credentials: no execution context")
			return 0
		}
		if ctx.credentialList == nil {
			L.RaiseError("llm_router.credentials.list: credential store is not wired")
			return 0
		}
		credentials, listError := ctx.credentialList(ctx.callContext(), ctx.providerID)
		if listError != nil {
			L.RaiseError("llm_router.credentials.list: %s", listError.Error())
			return 0
		}
		out := L.NewTable()
		disabledCount := 0
		deniedCount := 0
		parkedCount := 0
		for _, credential := range credentials {
			if credential == nil || credential.Disabled {
				disabledCount++
				continue
			}
			if !credentialAllowed(ctx, credential.ID) {
				deniedCount++
				continue
			}
			if ctx.credentialParked != nil {
				if parked, _ := ctx.credentialParked(ctx.callContext(), credential.ID); parked != nil {
					parkedCount++
					continue
				}
			}
			out.Append(credentialTable(L, credential))
		}
		ctx.logCredentialsListed(len(credentials), disabledCount, deniedCount, parkedCount, out.Len())
		L.Push(out)
		return 1
	}))
	table.RawSetString("get", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		if ctx == nil {
			L.RaiseError("llm_router.credentials: no execution context")
			return 0
		}
		if ctx.credentialGet == nil {
			L.RaiseError("llm_router.credentials.get: credential store is not wired")
			return 0
		}
		credential, getError := ctx.credentialGet(ctx.callContext(), id)
		if getError != nil {
			ctx.logCredentialMissed(id, "error")
			L.Push(lua.LNil)
			return 1
		}
		if credential == nil {
			ctx.logCredentialMissed(id, "missing")
			L.Push(lua.LNil)
			return 1
		}
		if credential.Disabled {
			ctx.logCredentialMissed(id, "disabled")
			L.Push(lua.LNil)
			return 1
		}
		if !credentialAllowed(ctx, credential.ID) {
			ctx.logCredentialMissed(id, "denied")
			L.Push(lua.LNil)
			return 1
		}
		L.Push(credentialTable(L, credential))
		return 1
	}))
	table.RawSetString("update", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		dataTable := L.CheckTable(2)
		if ctx == nil {
			L.RaiseError("llm_router.credentials: no execution context")
			return 0
		}
		if !ctx.allowCredentialWrite {
			L.RaiseError("llm_router.credentials.update: allowed in job contexts only")
			return 0
		}
		if ctx.credentialUpdate == nil {
			L.RaiseError("llm_router.credentials.update: credential store is not wired")
			return 0
		}
		data, err := fromLuaValue(dataTable)
		if err != nil {
			L.RaiseError("llm_router.credentials.update: %s", err.Error())
			return 0
		}
		dataMap, ok := data.(map[string]any)
		if !ok {
			L.RaiseError("llm_router.credentials.update: data must be a table")
			return 0
		}
		if uerr := ctx.credentialUpdate(ctx.callContext(), id, dataMap); uerr != nil {
			L.RaiseError("llm_router.credentials.update: %s", uerr.Error())
			return 0
		}
		ctx.logCredentialUpdated(id)
		L.Push(lua.LTrue)
		return 1
	}))
	table.RawSetString("disable", L.NewFunction(func(L *lua.LState) int {
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
		if ctx.credentialDisable == nil {
			L.RaiseError("llm_router.credentials.disable: credential store is not wired")
			return 0
		}
		if uerr := ctx.credentialDisable(ctx.callContext(), id, reason); uerr != nil {
			L.RaiseError("llm_router.credentials.disable: %s", uerr.Error())
			return 0
		}
		ctx.logCredentialDisabled(id, reason)
		L.Push(lua.LTrue)
		return 1
	}))
	table.RawSetString("enable", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		if ctx == nil {
			L.RaiseError("llm_router.credentials: no execution context")
			return 0
		}
		if err := ctx.checkAutomation("enable"); err != nil {
			L.RaiseError("llm_router.credentials.enable: %s", err.Error())
			return 0
		}
		if ctx.credentialEnable == nil {
			L.RaiseError("llm_router.credentials.enable: credential store is not wired")
			return 0
		}
		if uerr := ctx.credentialEnable(ctx.callContext(), id); uerr != nil {
			L.RaiseError("llm_router.credentials.enable: %s", uerr.Error())
			return 0
		}
		ctx.logCredentialEnabled(id)
		L.Push(lua.LTrue)
		return 1
	}))
	table.RawSetString("park", L.NewFunction(func(L *lua.LState) int {
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
		if ctx.credentialPark == nil {
			L.RaiseError("llm_router.credentials.park: credential store is not wired")
			return 0
		}
		if uerr := ctx.credentialPark(ctx.callContext(), id, time.Duration(float64(secs))*time.Second, reason); uerr != nil {
			L.RaiseError("llm_router.credentials.park: %s", uerr.Error())
			return 0
		}
		ctx.logCredentialParked(id, reason, time.Duration(float64(secs))*time.Second)
		L.Push(lua.LTrue)
		return 1
	}))
	table.RawSetString("unpark", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		if ctx == nil {
			L.RaiseError("llm_router.credentials: no execution context")
			return 0
		}
		if ctx.credentialUnpark == nil {
			L.RaiseError("llm_router.credentials.unpark: credential store is not wired")
			return 0
		}
		if uerr := ctx.credentialUnpark(ctx.callContext(), id); uerr != nil {
			L.RaiseError("llm_router.credentials.unpark: %s", uerr.Error())
			return 0
		}
		ctx.logCredentialUnparked(id)
		L.Push(lua.LTrue)
		return 1
	}))
	table.RawSetString("parked", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		if ctx == nil {
			L.RaiseError("llm_router.credentials: no execution context")
			return 0
		}
		if ctx.credentialParked == nil {
			L.RaiseError("llm_router.credentials.parked: credential store is not wired")
			return 0
		}
		entry, uerr := ctx.credentialParked(ctx.callContext(), id)
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
	return table
}

const (
	defaultProxyRequireTimeout = 60 * time.Second
	maxProxyRequireTimeout     = 5 * time.Minute
	maxProxyRequireLimit       = 50
	maxProxyRequireWant        = 200
	proxyFallbackDirect        = "direct"
	proxyFallbackFail          = "fail"
)

// luaStringList reads an optional array-of-strings option of proxies.require.
func luaStringList(L *lua.LState, optionsTable *lua.LTable, field string) []string {
	v := optionsTable.RawGetString(field)
	if v == lua.LNil {
		return nil
	}
	list, ok := v.(*lua.LTable)
	if !ok {
		L.RaiseError("llm_router.proxies.require: %s must be an array of strings", field)
		return nil
	}
	out := make([]string, 0, list.Len())
	for i := 1; i <= list.Len(); i++ {
		s, ok := list.RawGetInt(i).(lua.LString)
		if !ok {
			L.RaiseError("llm_router.proxies.require: %s[%d] must be a string", field, i)
			return nil
		}
		out = append(out, string(s))
	}
	return out
}

// luaIntOption reads an optional whole-number option within [min, max].
func luaIntOption(L *lua.LState, optionsTable *lua.LTable, field string, min, max int) (int, bool) {
	v := optionsTable.RawGetString(field)
	if v == lua.LNil {
		return 0, false
	}
	n, ok := v.(lua.LNumber)
	if !ok || float64(n) != float64(int(n)) || int(n) < min || int(n) > max {
		L.RaiseError("llm_router.proxies.require: %s must be a whole number between %d and %d", field, min, max)
		return 0, false
	}
	return int(n), true
}

// requireProxies implements llm_router.proxies.require.
func requireProxies(L *lua.LState, ctx *executionContext) int {
	request := models.ProxyRequire{Limit: 1}
	fallback := proxyFallbackFail
	timeout := defaultProxyRequireTimeout
	if arg := L.Get(1); arg != lua.LNil {
		optionsTable, ok := arg.(*lua.LTable)
		if !ok {
			L.RaiseError("llm_router.proxies.require: options must be a table")
			return 0
		}
		if v := optionsTable.RawGetString("pool"); v != lua.LNil {
			s, ok := v.(lua.LString)
			if !ok {
				L.RaiseError("llm_router.proxies.require: pool must be a string")
				return 0
			}
			request.Pool = string(s)
		}
		request.Countries = luaStringList(L, optionsTable, "countries")
		request.Exclude = luaStringList(L, optionsTable, "exclude")
		if n, ok := luaIntOption(L, optionsTable, "limit", 1, maxProxyRequireLimit); ok {
			request.Limit = n
		}
		if n, ok := luaIntOption(L, optionsTable, "want", 1, maxProxyRequireWant); ok {
			request.Want = n
		}
		if n, ok := luaIntOption(L, optionsTable, "timeout_ms", 1, int(maxProxyRequireTimeout/time.Millisecond)); ok {
			timeout = time.Duration(n) * time.Millisecond
		}
		if v := optionsTable.RawGetString("fallback"); v != lua.LNil {
			s, ok := v.(lua.LString)
			if !ok || (string(s) != proxyFallbackDirect && string(s) != proxyFallbackFail) {
				L.RaiseError("llm_router.proxies.require: fallback must be %q or %q", proxyFallbackDirect, proxyFallbackFail)
				return 0
			}
			fallback = string(s)
		}
	}
	if ctx == nil {
		L.RaiseError("llm_router.proxies: no execution context")
		return 0
	}
	if ctx.proxyRequire == nil {
		L.RaiseError("llm_router.proxies.require: proxy pool is not wired")
		return 0
	}

	// One wait deadline covers every require call of this execution: the first
	// call fixes it, later calls only consume what is left.
	now := time.Now()
	if ctx.proxyDeadline.IsZero() {
		ctx.proxyDeadline = now.Add(timeout)
	}
	request.Timeout = min(timeout, max(ctx.proxyDeadline.Sub(now), time.Millisecond))

	goCtx := ctx.goCtx
	if goCtx == nil {
		goCtx = context.Background()
	}
	res, err := ctx.proxyRequire(goCtx, request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			ctx.logProxyRequireCanceled(request, err)
			L.Push(lua.LNil)
			L.Push(lua.LString("canceled"))
			return 2
		}
		L.RaiseError("llm_router.proxies.require: %s", err.Error())
		return 0
	}
	if len(res.Proxies) == 0 && !res.Direct && fallback == proxyFallbackFail {
		code := "no_match"
		if res.TimedOut {
			code = "timeout"
		}
		ctx.logProxyRequireEmpty(request, code, res.TimedOut)
		L.Push(lua.LNil)
		L.Push(lua.LString(code))
		return 2
	}
	ctx.logProxyRequireSucceeded(request, len(res.Proxies), res.TimedOut, res.Direct)
	proxies := L.NewTable()
	for _, v := range res.Proxies {
		row := L.NewTable()
		row.RawSetString("id", lua.LString(v.ID))
		row.RawSetString("url", lua.LString(v.URL))
		if v.Country != "" {
			row.RawSetString("country", lua.LString(v.Country))
		}
		row.RawSetString("pool", lua.LString(v.Pool))
		proxies.Append(row)
	}
	out := L.NewTable()
	out.RawSetString("proxies", proxies)
	out.RawSetString("timed_out", lua.LBool(res.TimedOut))
	out.RawSetString("direct", lua.LBool(res.Direct))
	L.Push(out)
	return 1
}

// newProxiesTable builds the read-only llm_router.proxies query interface.
// Installed only when the type key serves proxy settings.
func newProxiesTable(L *lua.LState, ctx *executionContext) *lua.LTable {
	table := L.NewTable()
	table.RawSetString("require", L.NewFunction(func(L *lua.LState) int {
		return requireProxies(L, ctx)
	}))
	table.RawSetString("query", L.NewFunction(func(L *lua.LState) int {
		var pool, country string
		limit := 0
		if arg := L.Get(1); arg != lua.LNil {
			optionsTable, ok := arg.(*lua.LTable)
			if !ok {
				L.RaiseError("llm_router.proxies.query: options must be a table")
				return 0
			}
			if v := optionsTable.RawGetString("pool"); v != lua.LNil {
				s, ok := v.(lua.LString)
				if !ok {
					L.RaiseError("llm_router.proxies.query: pool must be a string")
					return 0
				}
				pool = string(s)
			}
			if v := optionsTable.RawGetString("country"); v != lua.LNil {
				s, ok := v.(lua.LString)
				if !ok {
					L.RaiseError("llm_router.proxies.query: country must be a string")
					return 0
				}
				country = string(s)
			}
			if v := optionsTable.RawGetString("limit"); v != lua.LNil {
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
		ctx.logProxyQueried(pool, country, limit, len(views))
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
	return table
}

// credentialAllowed enforces the router-token allow-list: nil means
// unrestricted (admin probes), otherwise only listed IDs serve.
func credentialAllowed(ctx *executionContext, id string) bool {
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

func (ctx *executionContext) credentialLogBase() []any {
	if ctx == nil {
		return nil
	}
	return []any{"plugin_id", ctx.pluginID, "type", ctx.typeKey}
}

func (ctx *executionContext) logCredentialsListed(total, disabled, denied, parked, returned int) {
	if ctx == nil || ctx.logger == nil {
		return
	}
	arguments := append(ctx.credentialLogBase(),
		"count", returned,
		"disabled", disabled,
		"denied", denied,
		"parked", parked,
		"total", total,
	)
	ctx.logger.Debug("Credentials listed", arguments...)
}

func (ctx *executionContext) logCredentialMissed(credentialID, reason string) {
	if ctx == nil || ctx.logger == nil {
		return
	}
	arguments := append(ctx.credentialLogBase(), "cred_id", credentialID, "reason", reason)
	ctx.logger.Debug("Credential lookup missed", arguments...)
}

func (ctx *executionContext) logCredentialUpdated(credentialID string) {
	if ctx == nil || ctx.logger == nil {
		return
	}
	arguments := append(ctx.credentialLogBase(), "cred_id", credentialID)
	ctx.logger.Info("Credential updated", arguments...)
}

func (ctx *executionContext) logCredentialDisabled(credentialID, reason string) {
	if ctx == nil || ctx.logger == nil {
		return
	}
	arguments := append(ctx.credentialLogBase(), "cred_id", credentialID)
	if reason != "" {
		arguments = append(arguments, "reason", reason)
	}
	ctx.logger.Info("Credential disabled", arguments...)
}

func (ctx *executionContext) logCredentialEnabled(credentialID string) {
	if ctx == nil || ctx.logger == nil {
		return
	}
	arguments := append(ctx.credentialLogBase(), "cred_id", credentialID)
	ctx.logger.Info("Credential enabled", arguments...)
}

func (ctx *executionContext) logCredentialParked(credentialID, reason string, ttl time.Duration) {
	if ctx == nil || ctx.logger == nil {
		return
	}
	arguments := append(ctx.credentialLogBase(), "cred_id", credentialID, "wait", int64(ttl/time.Second))
	if reason != "" {
		arguments = append(arguments, "reason", reason)
	}
	ctx.logger.Debug("Credential parked", arguments...)
}

func (ctx *executionContext) logCredentialUnparked(credentialID string) {
	if ctx == nil || ctx.logger == nil {
		return
	}
	arguments := append(ctx.credentialLogBase(), "cred_id", credentialID)
	ctx.logger.Debug("Credential unparked", arguments...)
}

func (ctx *executionContext) logProxyRequireCanceled(request models.ProxyRequire, requireError error) {
	if ctx == nil || ctx.logger == nil {
		return
	}
	arguments := append(ctx.credentialLogBase(),
		"operation", "require",
		"reason", "canceled",
		"error", requireError.Error(),
		"count", request.Limit,
	)
	ctx.logger.Warn("Proxy request canceled", arguments...)
}

func (ctx *executionContext) logProxyRequireEmpty(request models.ProxyRequire, code string, timedOut bool) {
	if ctx == nil || ctx.logger == nil {
		return
	}
	arguments := append(ctx.credentialLogBase(),
		"operation", "require",
		"code", code,
		"count", 0,
	)
	if timedOut {
		ctx.logger.Warn("Proxy request timed out", arguments...)
		return
	}
	ctx.logger.Debug("Proxy request found no match", arguments...)
}

func (ctx *executionContext) logProxyRequireSucceeded(request models.ProxyRequire, count int, timedOut, direct bool) {
	if ctx == nil || ctx.logger == nil {
		return
	}
	arguments := append(ctx.credentialLogBase(),
		"operation", "require",
		"count", count,
		"direct", direct,
		"timed_out", timedOut,
	)
	_ = request
	ctx.logger.Debug("Proxies required", arguments...)
}

func (ctx *executionContext) logProxyQueried(pool, country string, limit, count int) {
	if ctx == nil || ctx.logger == nil {
		return
	}
	arguments := append(ctx.credentialLogBase(),
		"operation", "query",
		"count", count,
		"limit", limit,
	)
	if pool != "" {
		arguments = append(arguments, "pool", pool)
	}
	if country != "" {
		arguments = append(arguments, "country", country)
	}
	ctx.logger.Debug("Proxies queried", arguments...)
}
