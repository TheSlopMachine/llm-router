package luaplugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// callConfig carries per-invocation identity and capability gates into the
// sandbox. Request paths never set credentialID or allowCredentialWrite:
// plugins select credentials themselves and persist only from jobs.
// allowedCredentials carries the router-token allow-list; nil means
// unrestricted.
type callConfig struct {
	providerID           string
	providerConfig       map[string]any
	credentialID         string
	model                models.ModelId
	allowedCredentials   []string
	allowCredentialWrite bool
	timeoutMs            int
	jobName              string
}

// handlerCall loads plugin source in a fresh state and invokes one handler.
// Lua error tables matching the terminal (message, code?, param?, status?)
// shape become ProviderError; everything else becomes PluginInternalError.
func (s *Service) handlerCall(
	goCtx context.Context,
	rec *PluginRecord,
	typeKey, handler string,
	pushArgs func(L *lua.LState),
	nret int,
	applyRet func(L *lua.LState) error,
	providerConfig map[string]any,
) (bool, error) {
	found, _, err := s.handlerCallEx(goCtx, rec, HandlerMeta{TypeKey: typeKey, ProviderConfig: providerConfig}, handler, pushArgs, nret, applyRet, nil)
	return found, err
}

// handlerCallEx is handlerCall with an explicit call config.
func (s *Service) handlerCallEx(
	goCtx context.Context,
	rec *PluginRecord,
	meta HandlerMeta,
	handler string,
	pushArgs func(L *lua.LState),
	nret int,
	applyRet func(L *lua.LState) error,
	cfg *callConfig,
) (found bool, route string, err error) {
	typeKey := meta.TypeKey
	if cfg == nil {
		cfg = &callConfig{providerID: meta.ProviderID, providerConfig: meta.ProviderConfig, credentialID: meta.credentialID(), model: meta.Model, allowedCredentials: meta.AllowedCredentials}
	}
	ctx := &execContext{
		pluginID:             rec.ID,
		allowHosts:           rec.AllowHosts,
		unsafe:               rec.Unsafe,
		logger:               s.logger,
		logSink:              s.appendLog,
		storage:              s.storage,
		timeoutMs:            cfg.timeoutMs,
		providerID:           cfg.providerID,
		typeKey:              typeKey,
		credentialID:         cfg.credentialID,
		model:                cfg.model,
		allowedCredentials:   cfg.allowedCredentials,
		allowCredentialWrite: cfg.allowCredentialWrite,
		jobName:              cfg.jobName,
		markDead:             s.markDead,
		healthTrigger:        s.healthTrigger,
		proxyQuery:           s.proxyQuery,
		credList:             s.credList,
		credGet:              s.credGet,
		credUpdate:           s.credUpdate,
		credDisable:          s.credDisable,
		credEnable:           s.credEnable,
		credPark:             s.credPark,
		credUnpark:           s.credUnpark,
		credParked:           s.credParked,
		automationOn:         s.automationOn,
		capabilities:         s.capabilitiesFor(typeKey),
		goCtx:                goCtx,
		registrations:        map[string]*lua.LTable{},
		proxySources:         map[string]*lua.LTable{},
	}
	// Failure-triggered health verification runs detached: attempts that
	// carry a credential identity mark it suspect, the healthcheck service
	// bounds check frequency by cooldown and disables only on an explicit
	// unhealthy verdict. Traffic paths carry no credential identity, so
	// they never trigger.
	defer func() {
		if err == nil {
			return
		}
		var perr *models.ProviderError
		if !isTerminalProviderError(err, &perr) {
			return
		}
		if ctx.healthTrigger != nil && ctx.credentialID != "" {
			ctx.healthTrigger.SuspectFailed(ctx.pluginID, ctx.typeKey, ctx.credentialID)
		}
	}()
	L := newSandboxState(ctx)
	defer L.Close()
	L.SetContext(goCtx)

	if err := L.DoString(string(rec.Source)); err != nil {
		cause := luaErrorString(L.Get(-1))
		if cause == "" || cause == "nil" {
			cause = err.Error()
		}
		cause = fmt.Sprintf("%s (source %d bytes)", cause, len(rec.Source))
		s.recordCrash(rec.ID, typeKey, s.crashPrefix(ctx)+"load: "+cause)
		return true, "", &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "load: " + cause}
	}
	handlers, ok := ctx.registrations[typeKey]
	if !ok {
		if srcHandlers, isSource := ctx.proxySources[typeKey]; isSource {
			handlers = srcHandlers
		} else {
			return false, "", &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: handler}
		}
	}
	fn := lookupCallable(handlers, handler)
	if fn == nil {
		// Job specs carry colocated run functions under jobs.<name>.run.
		if jobFn := lookupJobRun(handlers, handler); jobFn != nil {
			fn = jobFn
		} else {
			return false, "", &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: handler}
		}
	}

	top := L.GetTop()
	L.Push(fn)
	if pushArgs != nil {
		pushArgs(L)
	}
	nargs := L.GetTop() - top - 1
	if callErr := L.PCall(nargs, nret, nil); callErr != nil {
		err := s.luaCallError(rec, typeKey, L, callErr, s.crashPrefix(ctx))
		L.SetTop(top)
		return true, "", err
	}
	defer L.SetTop(top)
	if applyRet != nil {
		if err := applyRet(L); err != nil {
			return true, "", err
		}
	}
	return true, "", nil
}

// handlerCallRouted is handlerCallEx plus a route placeholder kept for
// caller compatibility. Proxy routes no longer exist: it always returns "".
func (s *Service) handlerCallRouted(
	goCtx context.Context,
	rec *PluginRecord,
	meta HandlerMeta,
	handler string,
	pushArgs func(L *lua.LState),
	nret int,
	applyRet func(L *lua.LState) error,
) (found bool, route string, err error) {
	return s.handlerCallEx(goCtx, rec, meta, handler, pushArgs, nret, applyRet, nil)
}

func (s *Service) crashPrefix(ctx *execContext) string {
	if ctx != nil && ctx.jobName != "" {
		return "job " + ctx.jobName + ": "
	}
	return ""
}

func lookupCallable(handlers *lua.LTable, handler string) *lua.LFunction {
	fn, ok := handlers.RawGetString(handler).(*lua.LFunction)
	if !ok {
		return nil
	}
	return fn
}

// lookupJobRun resolves "job:<name>" to the colocated jobs.<name>.run
// function stored in the registration table.
func lookupJobRun(handlers *lua.LTable, handler string) *lua.LFunction {
	name, ok := strings.CutPrefix(handler, "job:")
	if !ok || name == "" {
		return nil
	}
	jobs, ok := handlers.RawGetString("jobs").(*lua.LTable)
	if !ok {
		return nil
	}
	entry, ok := jobs.RawGetString(name).(*lua.LTable)
	if !ok {
		return nil
	}
	fn, ok := entry.RawGetString("run").(*lua.LFunction)
	if !ok {
		return nil
	}
	return fn
}

func isTerminalProviderError(err error, out **models.ProviderError) bool {
	perr, ok := err.(*models.ProviderError)
	if !ok || perr == nil {
		return false
	}
	*out = perr
	return true
}

// asTerminalError parses the terminal OpenAI-shaped error table:
// message is required and non-empty; code defaults to server_error;
// param is optional; status is an optional 400..599 override defaulting
// to 502. Anything else is not a terminal error.
func asTerminalError(v lua.LValue) (*models.ProviderError, bool) {
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return nil, false
	}
	msgVal, ok := tbl.RawGetString("message").(lua.LString)
	if !ok || strings.TrimSpace(string(msgVal)) == "" {
		return nil, false
	}
	status := 502
	if sv := tbl.RawGetString("status"); sv != lua.LNil {
		n, ok := sv.(lua.LNumber)
		if !ok || int(n) < 400 || int(n) > 599 {
			return nil, false
		}
		status = int(n)
	}
	code := "server_error"
	if cv := tbl.RawGetString("code"); cv != lua.LNil {
		cs, ok := cv.(lua.LString)
		if !ok || strings.TrimSpace(string(cs)) == "" {
			return nil, false
		}
		code = string(cs)
	}
	param := ""
	if pv := tbl.RawGetString("param"); pv != lua.LNil {
		ps, ok := pv.(lua.LString)
		if !ok {
			return nil, false
		}
		param = string(ps)
	}
	return &models.ProviderError{StatusCode: status, Message: string(msgVal), Code: code, Param: param}, true
}

// luaCallError converts a failed PCall into ProviderError or PluginInternalError.
func (s *Service) luaCallError(rec *PluginRecord, typeKey string, L *lua.LState, callErr error, prefix string) error {
	top := L.GetTop()
	if top >= 1 {
		if perr, ok := asTerminalError(L.Get(top)); ok {
			return perr
		}
	}
	cause := luaErrorString(L.Get(-1))
	if callErr != nil && (cause == "" || cause == "nil") {
		cause = callErr.Error()
	}
	if goCtx := L.Context(); goCtx != nil {
		if ctxErr := goCtx.Err(); ctxErr != nil {
			cause = "cancelled: " + ctxErr.Error()
		}
	}
	cause = prefix + cause
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
// a valid terminal table becomes ProviderError, any other non-nil value
// becomes PluginInternalError.
func (s *Service) contractErrOrInternal(rec *PluginRecord, typeKey string, rawErr lua.LValue) error {
	if rawErr == nil || rawErr == lua.LNil {
		return nil
	}
	if perr, ok := asTerminalError(rawErr); ok {
		return perr
	}
	cause := "handler returned invalid error value: " + luaErrorString(rawErr)
	s.recordCrash(rec.ID, typeKey, cause)
	return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: cause}
}
