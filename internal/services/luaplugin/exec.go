package luaplugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
	lua "github.com/yuin/gopher-lua"
)

// dumpSnippetCap bounds the upstream body kept in memory and info logs.
// Larger bodies spill to dumpDir files in debug mode.
const dumpSnippetCap = 4096

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

// spillUpstreamBody moves oversized upstream bodies out of memory: bodies
// over dumpSnippetCap truncate to the snippet, and in debug mode the full
// body spills to a dumpDir file with the path on the log line.
func spillUpstreamBody(ctx *execContext, perr *models.ProviderError) {
	if perr == nil || len(perr.UpstreamBody) <= dumpSnippetCap {
		return
	}
	full := perr.UpstreamBody
	perr.UpstreamBody = full[:dumpSnippetCap] + "…[truncated]"
	if ctx == nil || ctx.logger == nil || !ctx.logger.Enabled(ctx.goCtx, slog.LevelDebug) {
		return
	}
	if ctx.dumpDir == "" {
		return
	}
	name := fmt.Sprintf("%s_%s_%d.body", ctx.pluginID, ctx.typeKey, time.Now().UnixNano())
	path := filepath.Join(ctx.dumpDir, name)
	if err := os.WriteFile(path, []byte(full), 0600); err != nil {
		ctx.logger.Warn("upstream dump failed", "error", err)
		return
	}
	ctx.logger.Debug("upstream body spilled to file", "path", path, "size", len(full))
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
		geoban:              s.geoban,
		disableCredential:   s.credDisabler,
		disableProvider:     s.provDisabler,
		dumpDir:             s.dumpDir,
		goCtx:               goCtx,
	}
	// Outcome effects: the single site owning every error side effect.
	// rate_limit/quota_exceeded record the scoped joint key with the
	// plugin-supplied TTL; model_unavailable records the model key for a
	// fixed 2 minutes; geo records the indefinite (plugin, provider, proxy)
	// flag; auth/payment_required disable the attempt credential;
	// structural_fault disables the provider instance. Everything else
	// carries no state. Disables are first-wins inside the owning service.
	defer func() {
		if err == nil {
			return
		}
		var perr *models.ProviderError
		if !errors.As(err, &perr) {
			return
		}
		// Debug spill: bodies over the snippet cap go to disk with the path
		// on the log line; the in-memory body is truncated to the snippet
		// unless debug logging is on.
		spillUpstreamBody(ctx, perr)
		switch perr.Type {
		case models.ErrorTypeRateLimit, models.ErrorTypeQuotaExceeded:
			if ctx.exhausted == nil {
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
			} else if ctx.logger != nil {
				ctx.logger.Debug("exhausted: marked limit key", "plugin_id", ctx.pluginID, "type", ctx.typeKey, "key", key)
			}
		case models.ErrorTypeModelUnavailable:
			if ctx.exhausted == nil || ctx.model == "" {
				return
			}
			key, kerr := exhausted.KeyFromScope(ctx.pluginID, ctx.typeKey, "", ctx.model.String(), "", []string{models.ExhaustedScopeModel})
			if kerr != nil {
				return
			}
			if merr := ctx.exhausted.Mark(key, time.Now().Add(2*time.Minute), perr.Message); merr != nil && ctx.logger != nil {
				ctx.logger.Warn("exhausted: model_unavailable mark failed", "key", key, "error", merr)
			} else if ctx.logger != nil {
				ctx.logger.Debug("exhausted: marked model key", "plugin_id", ctx.pluginID, "type", ctx.typeKey, "key", key)
			}
		case models.ErrorTypeGeo:
			if ctx.geoban == nil || ctx.lastProxyID == "" {
				return
			}
			if merr := ctx.geoban.Mark(ctx.pluginID, ctx.typeKey, ctx.lastProxyID, perr.Message); merr != nil && ctx.logger != nil {
				ctx.logger.Warn("geoban: mark failed", "proxy", ctx.lastProxyID, "error", merr)
			} else if ctx.logger != nil {
				ctx.logger.Debug("geoban: marked proxy", "plugin_id", ctx.pluginID, "type", ctx.typeKey, "proxy", ctx.lastProxyID)
			}
		case models.ErrorTypeAuth, models.ErrorTypePaymentRequired:
			if ctx.disableCredential == nil || ctx.credentialID == "" {
				return
			}
			ctx.disableCredential(ctx.credentialID, perr.Message)
			if ctx.logger != nil {
				ctx.logger.Info("credential disabled by system", "credential_id", ctx.credentialID, "plugin_id", ctx.pluginID, "type", ctx.typeKey, "reason", contractTypeName(perr.Type))
			}
		case models.ErrorTypeStructuralFault:
			if ctx.disableProvider == nil || ctx.providerID == "" {
				return
			}
			ctx.disableProvider(ctx.providerID, perr.Message)
			if ctx.logger != nil {
				ctx.logger.Info("provider disabled by system", "provider_id", ctx.providerID, "plugin_id", ctx.pluginID, "type", ctx.typeKey, "reason", contractTypeName(perr.Type))
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
	if !ok1 || !ok2 || string(msgStr) == "" {
		return nil, false
	}
	var errType models.ErrorType
	var status int
	var scopeAllowed []string
	retryRequired := false
	switch strings.ToLower(string(typeStr)) {
	case "rate_limit":
		errType = models.ErrorTypeRateLimit
		status = 429
		scopeAllowed = []string{models.ExhaustedScopeAccount, models.ExhaustedScopeModel, models.ExhaustedScopeProxy}
		retryRequired = true
	case "quota_exceeded":
		errType = models.ErrorTypeQuotaExceeded
		status = 429
		scopeAllowed = []string{models.ExhaustedScopeAccount, models.ExhaustedScopeModel}
		retryRequired = true
	case "auth":
		errType = models.ErrorTypeAuth
		status = 401
	case "upstream":
		errType = models.ErrorTypeUpstream
		status = 502
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
	case "content_policy":
		errType = models.ErrorTypeContentPolicy
		status = 400
	case "model_unavailable":
		errType = models.ErrorTypeModelUnavailable
		status = 503
	case "structural_fault":
		errType = models.ErrorTypeStructuralFault
		status = 502
	default:
		return nil, false
	}
	perr := &models.ProviderError{StatusCode: status, Message: string(msgStr), Type: errType}
	if rv := tbl.RawGetString("retry_after"); rv != lua.LNil {
		n, ok := rv.(lua.LNumber)
		if !ok {
			return nil, false
		}
		t := time.Unix(int64(n), 0)
		perr.RetryAfter = &t
	}
	if retryRequired {
		if perr.RetryAfter == nil || !perr.RetryAfter.After(time.Now()) {
			return nil, false
		}
	} else if perr.RetryAfter != nil {
		return nil, false
	}
	if sv := tbl.RawGetString("scope"); sv != lua.LNil {
		if scopeAllowed == nil {
			return nil, false
		}
		scope, ok := parseScope(sv)
		if !ok {
			return nil, false
		}
		for _, w := range scope {
			allowed := false
			for _, a := range scopeAllowed {
				if w == a {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil, false
			}
		}
		perr.Scope = scope
	}
	if uv := tbl.RawGetString("upstream_status"); uv != lua.LNil {
		if n, ok := uv.(lua.LNumber); ok && int(n) > 0 {
			perr.UpstreamStatus = int(n)
		}
	}
	if bv := tbl.RawGetString("upstream_body"); bv != lua.LNil {
		if s, ok := bv.(lua.LString); ok {
			perr.UpstreamBody = string(s)
		}
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
