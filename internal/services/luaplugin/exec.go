package luaplugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// scopedProxyRec attributes proxy pair outcomes to the requesting type key.
// A plugin serving several keys shares one record; limits apply per key.
func scopedProxyRec(rec *PluginRecord, typeKey string) *PluginRecord {
	cp := *rec
	cp.TypeKeys = []string{typeKey}
	return &cp
}

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
) (bool, error) {
	found, _, err := s.handlerCallRouted(goCtx, rec, typeKey, handler, pushArgs, nret, applyRet, providerConfig)
	return found, err
}

// handlerCallRouted is handlerCall plus the proxy route used by the call's
// last HTTP request ("" = direct).
func (s *Service) handlerCallRouted(
	goCtx context.Context,
	rec *PluginRecord,
	typeKey, handler string,
	pushArgs func(L *lua.LState),
	nret int,
	applyRet func(L *lua.LState) error,
	providerConfig map[string]any,
) (found bool, route string, err error) {
	ctx := &execContext{
		pluginID:            rec.ID,
		allowHosts:          rec.AllowHosts,
		unsafe:              rec.Unsafe,
		logger:              s.logger,
		logSink:             s.appendLog,
		storage:             s.storage,
		registrations:       map[string]*lua.LTable{},
		proxySources:        map[string]*lua.LTable{},
		proxyResolver:       s.proxyResolver,
		proxyRec:            scopedProxyRec(rec, typeKey),
		proxyProviderConfig: providerConfig,
	}
	if s.proxyEventReporter != nil {
		tk := typeKey
		ctx.onProxyEvent = func(ev ProxyEvent) {
			if ev.ProxyID != "" {
				ev.Provider = tk
				s.proxyEventReporter(ev)
			}
		}
	}
	// Pair outcomes: a rate-limit response limits the proxy for this
	// provider until retry_after; a geo-blocked response blocks it.
	defer func() {
		if err != nil && ctx.onProxyEvent != nil && ctx.lastProxyID != "" {
			var perr *models.ProviderError
			if errors.As(err, &perr) {
				switch perr.Type {
				case models.ErrorTypeRateLimit, models.ErrorTypeQuotaExceeded:
					resetsAt := time.Now().Add(time.Minute)
					if perr.RetryAfter != nil {
						resetsAt = *perr.RetryAfter
					}
					ctx.onProxyEvent(ProxyEvent{ProxyID: ctx.lastProxyID, RateLimited: true, ResetsAt: resetsAt})
				case models.ErrorTypeGeo:
					ctx.onProxyEvent(ProxyEvent{ProxyID: ctx.lastProxyID, Blocked: true, BlockReason: perr.Message})
				}
			}
		}
	}()
	L := newSandboxState(ctx)
	defer L.Close()
	L.SetContext(goCtx)

	if err := L.DoString(string(rec.Source)); err != nil {
		// A parse failure pushes nothing: fall back to the Go error so
		// the crash carries the parser message instead of "nil".
		cause := luaErrorString(L.Get(-1))
		if cause == "" || cause == "nil" {
			cause = err.Error()
		}
		cause = fmt.Sprintf("%s (source %d bytes)", cause, len(rec.Source))
		s.recordCrash(rec.ID, typeKey, "load: "+cause)
		return true, ctx.proxyID, &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "load: " + cause}
	}
	handlers, ok := ctx.registrations[typeKey]
	if !ok {
		if srcHandlers, isSource := ctx.proxySources[typeKey]; isSource {
			handlers = srcHandlers
		} else {
			return false, ctx.proxyID, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: handler}
		}
	}
	fn := handlers.RawGetString(handler)
	if fn == lua.LNil {
		return false, ctx.proxyID, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: handler}
	}
	lfn, ok := fn.(*lua.LFunction)
	if !ok {
		s.recordCrash(rec.ID, typeKey, "handler "+handler+" is not a function")
		return true, ctx.proxyID, &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "handler " + handler + " is not a function"}
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
		return true, ctx.proxyID, err
	}
	defer L.SetTop(top)
	if applyRet != nil {
		if err := applyRet(L); err != nil {
			return true, ctx.proxyID, err
		}
	}
	return true, ctx.proxyID, nil
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
	case "geo":
		errType = models.ErrorTypeGeo
		status = 400
	case "not_found":
		errType = models.ErrorTypeNotFound
		status = 404
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
