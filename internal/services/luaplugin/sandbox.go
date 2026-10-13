package luaplugin

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/events"
	lua "github.com/yuin/gopher-lua"
)

var typeKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// maxLogValueLength caps single plugin log values; maxLogKeys caps the
// keys table per call.
const (
	maxLogValueLength = 2048
	maxLogKeys        = 32
)

// capabilities gates sandbox tables per type key. Credentials resolve from
// the stored record (credential_schema or auth_initiate present); proxies
// from proxy_schema presence.
type capabilities struct {
	credentials bool
	proxies     bool
}

// executionContext carries per-call plugin identity into every sandboxed state.
type executionContext struct {
	pluginID   string
	allowHosts []string
	unsafe     bool
	logger     *slog.Logger
	logSink    func(pluginID, typeKey string, severity int, msg string, keys map[string]string)
	storage    storageBridge
	timeoutMs  int

	// Request identity: the provider instance and type serving the call
	// and the requested model. Credential selection belongs to the
	// plugin; the traffic path carries no credential identity.
	providerID   string
	typeKey      string
	credentialID string
	model        models.ModelId

	// allowCredentialWrite gates credentials.update to job contexts.
	allowCredentialWrite bool
	// allowedCredentials is the router-token allow-list enforced on
	// credentials.list/get; nil means unrestricted.
	allowedCredentials []string
	// jobName carries the running job for crash attribution ("" = request).
	jobName string
	// goCtx carries the caller request context into HTTP requests built by
	// this call's clients, so cancellation propagates (nil = Background).
	goCtx context.Context
	// healthTrigger receives the failed attempt identity for detached
	// health-check dispatch (nil = disabled).
	healthTrigger HealthTrigger
	// markDead excludes one proxy URL until an escalating ban expires
	// (nil = disabled). Called only on structural TLS faults.
	markDead func(url, reason string) bool
	// proxyQuery serves the read-only proxies.query table (nil = disabled).
	proxyQuery func(pool, country string, limit int) ([]models.ProxyView, error)
	// proxyRequire serves proxies.require (nil = disabled).
	proxyRequire func(ctx context.Context, request models.ProxyRequire) (models.ProxyRequireResult, error)
	// delivered is set once stream output has reached the client; after that
	// a transport failure is no longer retryable (nil outside streaming).
	delivered *atomic.Bool
	// proxyDeadline is the wait deadline shared by every proxies.require call
	// of this execution. The first call fixes it.
	proxyDeadline time.Time
	// Credential access: list/get serve every context, update serves jobs.
	// disable/enable serve every context but require the provider
	// automation switch (checkAutomation).
	credentialList    func(ctx context.Context, providerID string) ([]*models.Credential, error)
	credentialGet     func(ctx context.Context, id string) (*models.Credential, error)
	credentialUpdate  func(ctx context.Context, id string, data map[string]any) error
	credentialDisable func(ctx context.Context, id string, reason string) error
	credentialEnable  func(ctx context.Context, id string) error
	credentialPark    func(ctx context.Context, id string, ttl time.Duration, reason string) error
	credentialUnpark  func(ctx context.Context, id string) error
	credentialParked  func(ctx context.Context, id string) (*models.ParkEntry, error)
	// automationOn reports the provider disable_failed_credentials switch
	// for shared disable/enable writes; nil reads off.
	automationOn func(providerID string) bool // capabilities gates the credentials/proxies tables for this type key.
	capabilities capabilities

	registrations map[string]*lua.LTable
	proxySources  map[string]*lua.LTable
}

// openLib opens a single gopher-lua library by name.
func openLib(L *lua.LState, fn lua.LGFunction, name string) {
	L.Push(L.NewFunction(fn))
	L.Push(lua.LString(name))
	L.Call(1, 0)
}

// newSandboxState builds a fresh locked-down Lua state for one plugin call.
func newSandboxState(ctx *executionContext) *lua.LState {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})

	openLib(L, lua.OpenBase, lua.BaseLibName)
	openLib(L, lua.OpenTable, lua.TabLibName)
	openLib(L, lua.OpenString, lua.StringLibName)
	openLib(L, lua.OpenMath, lua.MathLibName)
	openLib(L, lua.OpenOs, lua.OsLibName)

	curatesBase(L)
	curatesOs(L)
	installPrint(L, ctx)
	installJSON(L)
	installRouterTable(L, ctx)

	return L
}

func curatesBase(L *lua.LState) {
	keep := map[string]bool{
		"pairs": true, "ipairs": true, "next": true, "select": true,
		"type": true, "tostring": true, "tonumber": true, "error": true,
		"pcall": true, "xpcall": true, "setmetatable": true, "getmetatable": true,
		"rawget": true, "rawset": true, "rawequal": true, "assert": true,
	}
	for _, name := range []string{
		"dofile", "loadfile", "load", "loadstring", "require", "module",
		"collectgarbage", "print", "getfenv", "setfenv", "newproxy",
		"unpack", "rawlen",
	} {
		if !keep[name] {
			L.SetGlobal(name, lua.LNil)
		}
	}
	// Drop anything else unexpected in _G that is callable and not allowlisted,
	// except the curated libs and our own globals installed later.
	for _, name := range []string{"io", "debug", "package", "coroutine", "channel", "file"} {
		L.SetGlobal(name, lua.LNil)
	}
}

func curatesOs(L *lua.LState) {
	osValue := L.GetGlobal("os")
	osTable, ok := osValue.(*lua.LTable)
	if !ok {
		return
	}
	keep := map[string]bool{"time": true, "clock": true, "date": true}
	var drop []string
	osTable.ForEach(func(k, _ lua.LValue) {
		if ks, ok := k.(lua.LString); ok && !keep[string(ks)] {
			drop = append(drop, string(ks))
		}
	})
	for _, k := range drop {
		osTable.RawSetString(k, lua.LNil)
	}
}

func installPrint(L *lua.LState, ctx *executionContext) {
	L.SetGlobal("print", L.NewFunction(func(L *lua.LState) int {
		top := L.GetTop()
		parts := make([]string, 0, top)
		for i := 1; i <= top; i++ {
			parts = append(parts, luaValueText(L.Get(i), L))
		}
		emitPluginLog(L, ctx, "print", events.SeverityInfo, strings.Join(parts, "\t"), nil)
		return 0
	}))
}

// emitPluginLog routes one plugin log line to the sink and the display
// logger. The sink persists; the display logs at the matching level so
// LOG_LEVEL filtering applies per severity.
func emitPluginLog(L *lua.LState, ctx *executionContext, caller string, severity int, msg string, keys map[string]string) {
	if len(msg) > maxLogValueLength {
		msg = msg[:maxLogValueLength] + "…[truncated]"
	}
	if ctx == nil {
		return
	}
	if ctx.logSink != nil {
		ctx.logSink(ctx.pluginID, ctx.typeKey, severity, msg, keys)
	}
	if ctx.logger == nil {
		return
	}
	arguments := []any{"plugin_id", ctx.pluginID, "type", ctx.typeKey, "msg", msg}
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		arguments = append(arguments, name, keys[name])
	}
	ctx.logger.Log(context.Background(), slogLevelForSeverity(severity), "plugin log", arguments...)
}

func slogLevelForSeverity(severity int) slog.Level {
	switch severity {
	case events.SeverityDebug:
		return slog.LevelDebug
	case events.SeverityWarn:
		return slog.LevelWarn
	case events.SeverityError, events.SeverityFatal:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// parseLogSeverity maps a plugin severity string to an event severity.
func parseLogSeverity(caller string, L *lua.LState, value lua.LValue) int {
	name, ok := value.(lua.LString)
	if !ok {
		L.RaiseError("%s: severity must be a string", caller)
		return events.SeverityInfo
	}
	switch strings.ToLower(strings.TrimSpace(string(name))) {
	case "debug":
		return events.SeverityDebug
	case "info":
		return events.SeverityInfo
	case "warn", "warning":
		return events.SeverityWarn
	case "error":
		return events.SeverityError
	case "fatal":
		return events.SeverityFatal
	default:
		L.RaiseError("%s: unknown severity %q: want debug, info, warn, error or fatal", caller, string(name))
		return events.SeverityInfo
	}
}

// parseLogKeys converts the optional keys table to string pairs. Reserved
// names plugin and type are rejected: the router owns that prefix.
func parseLogKeys(caller string, L *lua.LState, value lua.LValue) map[string]string {
	if value == lua.LNil {
		return nil
	}
	table, ok := value.(*lua.LTable)
	if !ok {
		L.RaiseError("%s: keys must be a table", caller)
		return nil
	}
	keys := map[string]string{}
	table.ForEach(func(k, v lua.LValue) {
		name, ok := k.(lua.LString)
		if !ok {
			L.RaiseError("%s: keys must have string keys", caller)
			return
		}
		key := string(name)
		if key == "plugin" || key == "type" {
			L.RaiseError("%s: key %q is reserved", caller, key)
			return
		}
		var text string
		switch typed := v.(type) {
		case lua.LString:
			text = string(typed)
		case lua.LNumber, lua.LBool:
			text = typed.String()
		case *lua.LTable:
			raw, err := marshalLua(typed)
			if err != nil {
				text = "<table>"
			} else {
				text = string(raw)
			}
		case *lua.LNilType:
			return
		default:
			text = v.String()
		}
		if len(text) > maxLogValueLength {
			text = text[:maxLogValueLength] + "…[truncated]"
		}
		keys[key] = text
	})
	if len(keys) > maxLogKeys {
		L.RaiseError("%s: at most %d keys", caller, maxLogKeys)
		return nil
	}
	return keys
}

// luaValueText renders one Lua value for log messages. Tables encode as
// JSON; every value truncates at maxLogValueLength.
func luaValueText(value lua.LValue, L *lua.LState) string {
	var text string
	switch typed := value.(type) {
	case lua.LString:
		text = string(typed)
	case lua.LNumber, lua.LBool:
		text = typed.String()
	case *lua.LTable:
		raw, err := marshalLua(typed)
		if err != nil {
			text = "<table>"
		} else {
			text = string(raw)
		}
	default:
		text = value.String()
	}
	if len(text) > maxLogValueLength {
		text = text[:maxLogValueLength] + "…[truncated]"
	}
	return text
}

func installJSON(L *lua.LState) {
	table := L.NewTable()
	table.RawSetString("encode", L.NewFunction(func(L *lua.LState) int {
		v := L.Get(1)
		raw, err := marshalLua(v)
		if err != nil {
			L.RaiseError("json.encode: %s", err.Error())
			return 0
		}
		L.Push(lua.LString(string(raw)))
		return 1
	}))
	table.RawSetString("decode", L.NewFunction(func(L *lua.LState) int {
		text := L.CheckString(1)
		var decoded any
		if err := unmarshalTo([]byte(text), &decoded); err != nil {
			L.RaiseError("json.decode: %s", err.Error())
			return 0
		}
		if decoded == nil {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(toLuaValue(L, decoded))
		return 1
	}))
	L.SetGlobal("json", table)
}

func installRouterTable(L *lua.LState, ctx *executionContext) {
	router := L.NewTable()

	router.RawSetString("register", L.NewFunction(func(L *lua.LState) int {
		typeKey := L.CheckString(1)
		handlers := L.CheckTable(2)
		if ctx == nil {
			L.RaiseError("llm_router.register: no execution context")
			return 0
		}
		if !typeKeyPattern.MatchString(typeKey) {
			L.RaiseError("llm_router.register: invalid type key %q", typeKey)
			return 0
		}
		if _, exists := ctx.registrations[typeKey]; exists {
			L.RaiseError("llm_router.register: duplicate type key %q", typeKey)
			return 0
		}
		complete := handlers.RawGetString("complete")
		if complete == lua.LNil {
			L.RaiseError("llm_router.register: handler \"complete\" is required for type %q", typeKey)
			return 0
		}
		if _, ok := complete.(*lua.LFunction); !ok {
			L.RaiseError("llm_router.register: handler \"complete\" must be a function")
			return 0
		}
		for _, name := range OptionalHandlerNames() {
			if v := handlers.RawGetString(name); v != lua.LNil {
				if _, ok := v.(*lua.LFunction); !ok {
					L.RaiseError("llm_router.register: handler %q must be a function", name)
					return 0
				}
			}
		}
		for _, kind := range schemaKinds {
			if v := handlers.RawGetString(kind); v != lua.LNil {
				if _, ok := v.(*lua.LTable); !ok {
					L.RaiseError("llm_router.register: %s must be a table", kind)
					return 0
				}
			}
		}
		if v := handlers.RawGetString("locales"); v != lua.LNil {
			if _, ok := v.(*lua.LTable); !ok {
				L.RaiseError("llm_router.register: locales must be a table")
				return 0
			}
		}
		if v := handlers.RawGetString("jobs"); v != lua.LNil {
			jobsTbl, ok := v.(*lua.LTable)
			if !ok {
				L.RaiseError("llm_router.register: jobs must be a table")
				return 0
			}
			if _, err := parseJobSpecifications(jobsTbl, typeKey); err != nil {
				L.RaiseError("llm_router.register: %s", err.Error())
				return 0
			}
		}
		ctx.registrations[typeKey] = handlers
		return 0
	}))

	router.RawSetString("register_proxy_source", L.NewFunction(func(L *lua.LState) int {
		typeKey := L.CheckString(1)
		handlers := L.CheckTable(2)
		if ctx == nil {
			L.RaiseError("llm_router.register_proxy_source: no execution context")
			return 0
		}
		if !typeKeyPattern.MatchString(typeKey) {
			L.RaiseError("llm_router.register_proxy_source: invalid type key %q", typeKey)
			return 0
		}
		if _, exists := ctx.registrations[typeKey]; exists {
			L.RaiseError("llm_router.register_proxy_source: duplicate type key %q", typeKey)
			return 0
		}
		if _, exists := ctx.proxySources[typeKey]; exists {
			L.RaiseError("llm_router.register_proxy_source: duplicate source key %q", typeKey)
			return 0
		}
		fetch := handlers.RawGetString("fetch_proxies")
		if fetch == lua.LNil {
			L.RaiseError("llm_router.register_proxy_source: handler \"fetch_proxies\" is required for %q", typeKey)
			return 0
		}
		if _, ok := fetch.(*lua.LFunction); !ok {
			L.RaiseError("llm_router.register_proxy_source: handler \"fetch_proxies\" must be a function")
			return 0
		}
		ctx.proxySources[typeKey] = handlers
		return 0
	}))

	router.RawSetString("http_client", L.NewFunction(func(L *lua.LState) int {
		if ctx == nil {
			L.RaiseError("llm_router.http_client: no execution context")
			return 0
		}
		timeoutMs := defaultHTTPTimeoutMs
		if L.GetTop() >= 1 && L.Get(1) != lua.LNil {
			options, ok := L.Get(1).(*lua.LTable)
			if !ok {
				L.RaiseError("llm_router.http_client: options must be a table")
				return 0
			}
			if v := options.RawGetString("timeout_ms"); v != lua.LNil {
				if n, ok := v.(lua.LNumber); ok {
					timeoutMs = int(n)
				} else {
					L.RaiseError("llm_router.http_client: timeout_ms must be a number")
					return 0
				}
			}
		}
		if timeoutMs < minHTTPTimeoutMs {
			timeoutMs = minHTTPTimeoutMs
		}
		if timeoutMs > maxHTTPTimeoutMs {
			timeoutMs = maxHTTPTimeoutMs
		}
		client := newPluginHTTPClient(ctx, timeoutMs)
		L.Push(client.toLua(L))
		return 1
	}))

	router.RawSetString("storage", newStorageTable(L, ctx))

	router.RawSetString("log", L.NewFunction(func(L *lua.LState) int {
		if ctx == nil {
			L.RaiseError("llm_router.log: no execution context")
			return 0
		}
		severity := parseLogSeverity("llm_router.log", L, L.Get(1))
		if L.GetTop() < 2 || L.Get(2) == lua.LNil {
			L.RaiseError("llm_router.log: message is required")
			return 0
		}
		message := luaValueText(L.Get(2), L)
		var keys map[string]string
		if L.GetTop() >= 3 && L.Get(3) != lua.LNil {
			keys = parseLogKeys("llm_router.log", L, L.Get(3))
		}
		emitPluginLog(L, ctx, "llm_router.log", severity, message, keys)
		return 0
	}))

	if ctx != nil && ctx.capabilities.credentials {
		router.RawSetString("credentials", newCredentialsTable(L, ctx))
	}

	if ctx != nil && ctx.capabilities.proxies {
		router.RawSetString("proxies", newProxiesTable(L, ctx))
	}

	router.RawSetString("uuid_v5", L.NewFunction(func(L *lua.LState) int {
		namespace := L.CheckString(1)
		name := L.CheckString(2)
		id, err := uuidV5(namespace, name)
		if err != nil {
			L.RaiseError("llm_router.uuid_v5: %s", err.Error())
			return 0
		}
		L.Push(lua.LString(id))
		return 1
	}))

	router.RawSetString("random_hex", L.NewFunction(func(L *lua.LState) int {
		n := L.CheckInt(1)
		s, err := randomHex(n)
		if err != nil {
			L.RaiseError("llm_router.random_hex: %s", err.Error())
			return 0
		}
		L.Push(lua.LString(s))
		return 1
	}))

	router.RawSetString("multipart", L.NewFunction(func(L *lua.LState) int {
		parts := L.CheckTable(1)
		body, contentType, err := buildMultipart(parts)
		if err != nil {
			L.RaiseError("llm_router.multipart: %s", err.Error())
			return 0
		}
		L.Push(lua.LString(body))
		L.Push(lua.LString(contentType))
		return 2
	}))

	router.RawSetString("base64_encode", L.NewFunction(func(L *lua.LState) int {
		s := L.CheckString(1)
		L.Push(lua.LString(base64.StdEncoding.EncodeToString([]byte(s))))
		return 1
	}))

	router.RawSetString("base64_decode", L.NewFunction(func(L *lua.LState) int {
		s := L.CheckString(1)
		raw, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			L.RaiseError("llm_router.base64_decode: %s", err.Error())
			return 0
		}
		L.Push(lua.LString(string(raw)))
		return 1
	}))

	L.SetGlobal("llm_router", router)
}

// callContext returns the caller request context for plugin-initiated
// storage and credential calls, or Background when the call runs detached
// (dry-run installs, startup jobs without a request).
func (ctx *executionContext) callContext() context.Context {
	if ctx == nil || ctx.goCtx == nil {
		return context.Background()
	}
	return ctx.goCtx
}

// luaErrorString renders a Lua error value for PluginInternalError causes.
func luaErrorString(v lua.LValue) string {
	switch t := v.(type) {
	case lua.LString:
		s := string(t)
		if len(s) > 1024 {
			s = s[:1024] + "…[truncated]"
		}
		return s
	case *lua.LTable:
		raw, err := marshalLua(t)
		if err != nil {
			return t.String()
		}
		s := string(raw)
		if len(s) > 1024 {
			s = s[:1024] + "…[truncated]"
		}
		return s
	default:
		return fmt.Sprintf("%v", v)
	}
}

// streamDelivered reports whether stream output already reached the client.
func (ctx *executionContext) streamDelivered() bool {
	return ctx != nil && ctx.delivered != nil && ctx.delivered.Load()
}
