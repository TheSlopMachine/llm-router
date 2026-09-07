package luaplugin

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

var typeKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// execContext carries per-call plugin identity into every sandboxed state.
type execContext struct {
	pluginID   string
	allowHosts []string
	unsafe     bool
	logger     *slog.Logger
	logSink    func(pluginID, msg string)
	storage    *storageBackend
	timeoutMs  int

	registrations map[string]*lua.LTable
}

// openLib opens a single gopher-lua library by name.
func openLib(L *lua.LState, fn lua.LGFunction, name string) {
	L.Push(L.NewFunction(fn))
	L.Push(lua.LString(name))
	L.Call(1, 0)
}

// newSandboxState builds a fresh locked-down Lua state for one plugin call.
func newSandboxState(ctx *execContext) *lua.LState {
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
	osVal := L.GetGlobal("os")
	osTbl, ok := osVal.(*lua.LTable)
	if !ok {
		return
	}
	keep := map[string]bool{"time": true, "clock": true, "date": true}
	var drop []string
	osTbl.ForEach(func(k, _ lua.LValue) {
		if ks, ok := k.(lua.LString); ok && !keep[string(ks)] {
			drop = append(drop, string(ks))
		}
	})
	for _, k := range drop {
		osTbl.RawSetString(k, lua.LNil)
	}
}

func installPrint(L *lua.LState, ctx *execContext) {
	L.SetGlobal("print", L.NewFunction(func(L *lua.LState) int {
		n := L.GetTop()
		parts := make([]string, 0, n)
		for i := 1; i <= n; i++ {
			v := L.Get(i)
			var s string
			switch t := v.(type) {
			case lua.LString:
				s = string(t)
			case lua.LNumber, lua.LBool:
				s = t.String()
			case *lua.LTable:
				raw, err := marshalLua(t)
				if err != nil {
					s = "<table>"
				} else {
					s = string(raw)
				}
			default:
				s = v.String()
			}
			if len(s) > 2048 {
				s = s[:2048] + "…[truncated]"
			}
			parts = append(parts, s)
		}
		msg := strings.Join(parts, "\t")
		if ctx != nil {
			if ctx.logSink != nil {
				ctx.logSink(ctx.pluginID, msg)
			}
			if ctx.logger != nil {
				ctx.logger.Info("plugin log", "plugin_id", ctx.pluginID, "msg", msg)
			}
		}
		return 0
	}))
}

func installJSON(L *lua.LState) {
	tbl := L.NewTable()
	tbl.RawSetString("encode", L.NewFunction(func(L *lua.LState) int {
		v := L.Get(1)
		raw, err := marshalLua(v)
		if err != nil {
			L.RaiseError("json.encode: %s", err.Error())
			return 0
		}
		L.Push(lua.LString(string(raw)))
		return 1
	}))
	tbl.RawSetString("decode", L.NewFunction(func(L *lua.LState) int {
		s := L.CheckString(1)
		var decoded any
		if err := unmarshalTo([]byte(s), &decoded); err != nil {
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
	L.SetGlobal("json", tbl)
}

func installRouterTable(L *lua.LState, ctx *execContext) {
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
		for _, name := range []string{
			"complete_stream", "validate_credentials", "get_model_infos",
			"needs_refresh", "refresh_credential", "config_schema",
			"credential_schema", "auth_initiate", "auth_step",
		} {
			if v := handlers.RawGetString(name); v != lua.LNil {
				if _, ok := v.(*lua.LFunction); !ok {
					L.RaiseError("llm_router.register: handler %q must be a function", name)
					return 0
				}
			}
		}
		ctx.registrations[typeKey] = handlers
		return 0
	}))

	router.RawSetString("create_http_client", L.NewFunction(func(L *lua.LState) int {
		if ctx == nil {
			L.RaiseError("llm_router.create_http_client: no execution context")
			return 0
		}
		timeoutMs := 60000
		if L.GetTop() >= 1 && L.Get(1) != lua.LNil {
			opts, ok := L.Get(1).(*lua.LTable)
			if !ok {
				L.RaiseError("llm_router.create_http_client: options must be a table")
				return 0
			}
			if v := opts.RawGetString("timeout_ms"); v != lua.LNil {
				if n, ok := v.(lua.LNumber); ok {
					timeoutMs = int(n)
				} else {
					L.RaiseError("llm_router.create_http_client: timeout_ms must be a number")
					return 0
				}
			}
		}
		if timeoutMs < 1000 {
			timeoutMs = 1000
		}
		if timeoutMs > 300000 {
			timeoutMs = 300000
		}
		client := newPluginHTTPClient(ctx, timeoutMs)
		L.Push(client.toLua(L))
		return 1
	}))

	router.RawSetString("storage", newStorageTable(L, ctx))

	L.SetGlobal("llm_router", router)
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
