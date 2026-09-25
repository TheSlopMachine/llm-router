package luaplugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
	lua "github.com/yuin/gopher-lua"
)

// scopedProxyRec attributes proxy pair outcomes to the requesting type key.
// A plugin serving several keys shares one record; limits apply per key.
func scopedProxyRec(rec *PluginRecord, typeKey string) *PluginRecord {
	cp := *rec
	cp.TypeKeys = []string{typeKey}
	return &cp
}

// exhaustedJointKey builds the stored joint key for one rate/quota outcome.
// Empty scope marks the full combination of known dimensions (at minimum
// plugin and provider). A scope word naming an empty dimension skips
// marking: a degraded key would bench wider than the error warrants (e.g. a
// credential-less call marking the whole provider via scope account).
func exhaustedJointKey(ctx *execContext, scope []string) (string, error) {
	if len(scope) == 0 {
		return exhausted.FullKey(ctx.pluginID, ctx.typeKey, ctx.credentialID, ctx.model.String(), ctx.lastProxyID), nil
	}
	for _, w := range scope {
		switch w {
		case models.ExhaustedScopeAccount:
			if ctx.credentialID == "" {
				return "", fmt.Errorf("exhausted: scope account with no credential")
			}
		case models.ExhaustedScopeModel:
			if ctx.model == "" {
				return "", fmt.Errorf("exhausted: scope model with no model")
			}
		case models.ExhaustedScopeProxy:
			if ctx.lastProxyID == "" {
				return "", fmt.Errorf("exhausted: scope proxy with no proxy")
			}
		default:
			return "", fmt.Errorf("exhausted: unknown scope word %q", w)
		}
	}
	return exhausted.KeyFromScope(ctx.pluginID, ctx.typeKey, ctx.credentialID, ctx.model.String(), ctx.lastProxyID, scope)
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
	found, _, err := s.handlerCallRouted(goCtx, rec, HandlerMeta{TypeKey: typeKey, ProviderConfig: providerConfig}, handler, pushArgs, nret, applyRet)
	return found, err
}

// handlerCallRouted is handlerCall plus the proxy route used by the call's
// last HTTP request as redacted host:port ("" = direct).
func (s *Service) handlerCallRouted(
	goCtx context.Context,
	rec *PluginRecord,
	meta HandlerMeta,
	handler string,
	pushArgs func(L *lua.LState),
	nret int,
	applyRet func(L *lua.LState) error,
) (found bool, route string, err error) {
	typeKey := meta.TypeKey
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
		proxyProviderConfig: meta.ProviderConfig,
		providerID:          meta.ProviderID,
		typeKey:             typeKey,
		credentialID:        meta.credentialID(),
		model:               meta.Model,
		exhausted:           s.exhausted,
		goCtx:               goCtx,
	}
	// Joint limit marking: a rate/quota outcome records the scoped joint
	// key (or the full combination without scope) in the exhausted store.
	// The single site owns every identity dimension: plugin, type,
	// credential, model and the last proxy of the call.
	defer func() {
		if err == nil || ctx.exhausted == nil {
			return
		}
		var perr *models.ProviderError
		if !errors.As(err, &perr) {
			return
		}
		if perr.Type != models.ErrorTypeRateLimit && perr.Type != models.ErrorTypeQuotaExceeded {
			return
		}
		resetsAt := time.Now().Add(time.Minute)
		if perr.RetryAfter != nil {
			resetsAt = *perr.RetryAfter
		}
		key, kerr := exhaustedJointKey(ctx, perr.Scope)
		if kerr != nil {
			if ctx.logger != nil {
				ctx.logger.Warn("exhausted: skip marking on empty dimension",
					"plugin_id", ctx.pluginID, "type", ctx.typeKey, "error", kerr)
			}
			return
		}
		if merr := ctx.exhausted.Mark(key, resetsAt, perr.Message); merr != nil && ctx.logger != nil {
			ctx.logger.Warn("exhausted: mark failed", "key", key, "error", merr)
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
		return true, ctx.proxyDisplay(), &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "load: " + cause}
	}
	handlers, ok := ctx.registrations[typeKey]
	if !ok {
		if srcHandlers, isSource := ctx.proxySources[typeKey]; isSource {
			handlers = srcHandlers
		} else {
			return false, ctx.proxyDisplay(), &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: handler}
		}
	}
	fn := handlers.RawGetString(handler)
	if fn == lua.LNil {
		return false, ctx.proxyDisplay(), &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: handler}
	}
	lfn, ok := fn.(*lua.LFunction)
	if !ok {
		s.recordCrash(rec.ID, typeKey, "handler "+handler+" is not a function")
		return true, ctx.proxyDisplay(), &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "handler " + handler + " is not a function"}
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
		return true, ctx.proxyDisplay(), err
	}
	defer L.SetTop(top)
	if applyRet != nil {
		if err := applyRet(L); err != nil {
			return true, ctx.proxyDisplay(), err
		}
	}
	return true, ctx.proxyDisplay(), nil
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
	case "payment_required":
		errType = models.ErrorTypePaymentRequired
		status = 402
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
	if sv := tbl.RawGetString("scope"); sv != lua.LNil {
		scope, ok := parseScope(sv)
		if !ok {
			return nil, false
		}
		perr.Scope = scope
	}
	return perr, true
}

// parseScope reads the optional scope array of an error contract table.
// Every entry must name a known exhausted dimension; anything else rejects
// the whole table so scope typos fail closed instead of silently widening
// or dropping the marking.
func parseScope(v lua.LValue) ([]string, bool) {
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return nil, false
	}
	var scope []string
	valid := true
	tbl.ForEach(func(_, item lua.LValue) {
		s, ok := item.(lua.LString)
		if !ok {
			valid = false
			return
		}
		switch string(s) {
		case models.ExhaustedScopeAccount, models.ExhaustedScopeModel, models.ExhaustedScopeProxy:
			scope = append(scope, string(s))
		default:
			valid = false
		}
	})
	if !valid {
		return nil, false
	}
	return scope, true
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
