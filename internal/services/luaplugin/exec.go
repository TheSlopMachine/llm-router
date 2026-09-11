package luaplugin

import (
	"context"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// handlerCall loads plugin source in a fresh state and invokes one handler.
// Lua error tables matching the (type, message, retry_after) contract become
// ProviderError; everything else becomes PluginInternalError.
func (s *Service) handlerCall(
	goCtx context.Context,
	rec *PluginRecord,
	typeKey, handler string,
	pushArgs func(L *lua.LState),
	nret int,
	applyRet func(L *lua.LState) error,
	providerConfig map[string]any,
) (found bool, err error) {
	ctx := &execContext{
		pluginID:      rec.ID,
		allowHosts:    rec.AllowHosts,
		unsafe:        rec.Unsafe,
		logger:        s.logger,
		logSink:       s.appendLog,
		storage:       s.storage,
		registrations: map[string]*lua.LTable{},
		proxySources:  map[string]*lua.LTable{},
	}
	if s.proxyResolver != nil {
		ctx.proxyID, ctx.proxyURL = s.proxyResolver(rec, providerConfig)
	}
	if ctx.proxyID != "" && s.proxyOutcome != nil {
		proxyID, tk := ctx.proxyID, typeKey
		ctx.onProxyResult = func(ok bool, latencyMs int64) {
			s.proxyOutcome(proxyID, tk, ok, latencyMs)
		}
	}
	L := newSandboxState(ctx)
	defer L.Close()
	L.SetContext(goCtx)

	if err := L.DoString(string(rec.Source)); err != nil {
		cause := luaErrorString(L.Get(-1))
		s.recordCrash(rec.ID, typeKey, "load: "+cause)
		return true, &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "load: " + cause}
	}
	handlers, ok := ctx.registrations[typeKey]
	if !ok {
		if srcHandlers, isSource := ctx.proxySources[typeKey]; isSource {
			handlers = srcHandlers
		} else {
			return false, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: handler}
		}
	}
	fn := handlers.RawGetString(handler)
	if fn == lua.LNil {
		return false, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: handler}
	}
	lfn, ok := fn.(*lua.LFunction)
	if !ok {
		s.recordCrash(rec.ID, typeKey, "handler "+handler+" is not a function")
		return true, &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "handler " + handler + " is not a function"}
	}

	top := L.GetTop()
	L.Push(lfn)
	if pushArgs != nil {
		pushArgs(L)
	}
	nargs := L.GetTop() - top - 1
	if callErr := L.PCall(nargs, nret, nil); callErr != nil {
		err := s.luaCallError(rec, typeKey, L, callErr)
		L.SetTop(top)
		return true, err
	}
	defer L.SetTop(top)
	if applyRet != nil {
		if err := applyRet(L); err != nil {
			return true, err
		}
	}
	return true, nil
}

func asProviderError(v lua.LValue) (*models.ProviderError, bool) {
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return nil, false
	}
	typeVal := tbl.RawGetString("type")
	msgVal := tbl.RawGetString("message")
	typeStr, ok1 := typeVal.(lua.LString)
	msgStr, ok2 := msgVal.(lua.LString)
	if !ok1 || !ok2 {
		return nil, false
	}
	var errType models.ErrorType
	var status int
	switch strings.ToLower(string(typeStr)) {
	case "rate_limit":
		errType = models.ErrorTypeRateLimit
		status = 429
	case "quota_exceeded":
		errType = models.ErrorTypeQuotaExceeded
		status = 429
	case "auth":
		errType = models.ErrorTypeAuth
		status = 401
	case "upstream":
		errType = models.ErrorTypeUpstream
		status = 502
	case "timeout":
		errType = models.ErrorTypeTimeout
		status = 504
	case "invalid_request":
		errType = models.ErrorTypeInvalidRequest
		status = 400
	default:
		return nil, false
	}
	perr := &models.ProviderError{StatusCode: status, Message: string(msgStr), Type: errType}
	if rv := tbl.RawGetString("retry_after"); rv != lua.LNil {
		if n, ok := rv.(lua.LNumber); ok {
			t := time.Unix(int64(n), 0)
			perr.RetryAfter = &t
		}
	}
	if errType == models.ErrorTypeQuotaExceeded && perr.RetryAfter == nil {
		t := time.Now().Add(time.Minute)
		perr.RetryAfter = &t
	}
	return perr, true
}

// luaCallError converts a failed PCall into ProviderError or PluginInternalError.
func (s *Service) luaCallError(rec *PluginRecord, typeKey string, L *lua.LState, callErr error) error {
	top := L.GetTop()
	if top >= 1 {
		if perr, ok := asProviderError(L.Get(top)); ok {
			return perr
		}
	}
	cause := luaErrorString(L.Get(-1))
	if callErr != nil && (cause == "" || cause == "nil") {
		cause = callErr.Error()
	}
	if goCtx := L.Context(); goCtx != nil {
		if ctxErr := goCtx.Err(); ctxErr != nil && ctxErr != context.Canceled {
			cause = "cancelled: " + ctxErr.Error()
		} else if ctxErr != nil {
			cause = "cancelled: " + ctxErr.Error()
		}
	}
	s.recordCrash(rec.ID, typeKey, cause)
	return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: cause}
}

// splitReturn reads the (result, err) pair left by a 2-value handler call.
// Stack layout: [..., result, err].
func splitReturn(L *lua.LState) (result lua.LValue, rawErr lua.LValue) {
	top := L.GetTop()
	if top < 2 {
		return lua.LNil, lua.LNil
	}
	return L.Get(top - 1), L.Get(top)
}

// contractErrOrInternal resolves the err slot of a (result, err) return:
// a valid contract table becomes ProviderError, any other non-nil value
// becomes PluginInternalError.
func (s *Service) contractErrOrInternal(rec *PluginRecord, typeKey string, rawErr lua.LValue) error {
	if rawErr == nil || rawErr == lua.LNil {
		return nil
	}
	if perr, ok := asProviderError(rawErr); ok {
		return perr
	}
	cause := "handler returned invalid error value: " + luaErrorString(rawErr)
	s.recordCrash(rec.ID, typeKey, cause)
	return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: cause}
}
