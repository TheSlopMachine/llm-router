package luaplugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin/statepool"
	lua "github.com/yuin/gopher-lua"
)

// callConfiguration carries per-invocation identity and capability gates into the
// sandbox. Request paths never set credentialID or allowCredentialWrite:
// plugins select credentials themselves and persist only from jobs.
// allowedCredentials carries the router-token allow-list; nil means
// unrestricted.
type callConfiguration struct {
	providerID           string
	providerConfig       map[string]any
	credentialID         string
	model                models.ModelId
	allowedCredentials   []string
	allowCredentialWrite bool
	timeoutMs            int
	jobName              string
	// capabilitiesOverride replaces the registry-derived sandbox tables
	// for lifecycle hooks executed from a stored record snapshot (the
	// registry may already serve a newer source). Nil keeps the registry.
	capabilitiesOverride *capabilities
	// lifecycleReadOnly strips credential write access (update, disable,
	// enable, park, unpark) from the sandbox. List, get and parked stay
	// available. Lifecycle hooks observe and persist through storage;
	// they never mutate shared credential state.
	lifecycleReadOnly bool
}

// handlerCall loads plugin source in a pooled state and invokes one handler.
// Lua error tables matching the terminal (message, code?, param?, status?)
// shape become ProviderError; everything else becomes PluginInternalError.
func (s *Service) handlerCall(
	goCtx context.Context,
	record *PluginRecord,
	typeKey, handler string,
	pushArgs func(L *lua.LState),
	returnCount int,
	applyRet func(L *lua.LState) error,
	providerConfig map[string]any,
) (bool, error) {
	found, _, err := s.handlerCallEx(goCtx, record, HandlerMeta{TypeKey: typeKey, ProviderConfig: providerConfig}, handler, pushArgs, returnCount, applyRet, nil)
	return found, err
}

// handlerCallEx is handlerCall with an explicit call configuration.
func (s *Service) handlerCallEx(
	goCtx context.Context,
	record *PluginRecord,
	handlerMetadata HandlerMeta,
	handler string,
	pushArgs func(L *lua.LState),
	returnCount int,
	applyRet func(L *lua.LState) error,
	configuration *callConfiguration,
) (found bool, route string, err error) {
	typeKey := handlerMetadata.TypeKey
	if configuration == nil {
		configuration = &callConfiguration{providerID: handlerMetadata.ProviderID, providerConfig: handlerMetadata.ProviderConfig, credentialID: handlerMetadata.credentialID(), model: handlerMetadata.Model, allowedCredentials: handlerMetadata.AllowedCredentials}
	}
	s.wiringMu.RLock()
	logger := s.logger
	markDead := s.markDead
	healthTrigger := s.healthTrigger
	proxyQuery := s.proxyQuery
	proxyRequire := s.proxyRequire
	credentialList := s.credentialList
	credentialGet := s.credentialGet
	credentialUpdate := s.credentialUpdate
	credentialDisable := s.credentialDisable
	credentialEnable := s.credentialEnable
	credentialPark := s.credentialPark
	credentialUnpark := s.credentialUnpark
	credentialParked := s.credentialParked
	automationOn := s.automationOn
	s.wiringMu.RUnlock()
	ctx := &executionContext{
		pluginID:             record.ID,
		allowHosts:           record.AllowHosts,
		unsafe:               record.Unsafe,
		logger:               logger,
		logSink:              s.appendLog,
		storage:              &serviceStorage{service: s, ctx: goCtx},
		timeoutMs:            configuration.timeoutMs,
		providerID:           configuration.providerID,
		typeKey:              typeKey,
		credentialID:         configuration.credentialID,
		model:                configuration.model,
		allowedCredentials:   configuration.allowedCredentials,
		allowCredentialWrite: configuration.allowCredentialWrite,
		jobName:              configuration.jobName,
		markDead:             markDead,
		healthTrigger:        healthTrigger,
		proxyQuery:           proxyQuery,
		proxyRequire:         proxyRequire,
		delivered:            handlerMetadata.delivered,
		credentialList:       credentialList,
		credentialGet:        credentialGet,
		credentialUpdate:     credentialUpdate,
		credentialDisable:    credentialDisable,
		credentialEnable:     credentialEnable,
		credentialPark:       credentialPark,
		credentialUnpark:     credentialUnpark,
		credentialParked:     credentialParked,
		automationOn:         automationOn,
		capabilities:         s.capabilitiesFor(typeKey),
		goCtx:                goCtx,
		registrations:        map[string]*lua.LTable{},
		proxySources:         map[string]*lua.LTable{},
	}
	if configuration.capabilitiesOverride != nil {
		ctx.capabilities = *configuration.capabilitiesOverride
	}
	if configuration.lifecycleReadOnly {
		ctx.credentialUpdate = nil
		ctx.credentialDisable = nil
		ctx.credentialEnable = nil
		ctx.credentialPark = nil
		ctx.credentialUnpark = nil
		ctx.automationOn = nil
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
	// Pooled states may carry globals from a prior call. Rebinding clears
	// the stack and installs the current per-call globals; source loads
	// again below on every call. The state returns exactly once through
	// the deferred call; nothing touches it afterwards.
	bucketKey := poolBucketKey(record, typeKey)
	pooled := statepool.Acquire(bucketKey, func() *lua.LState {
		return newSandboxState(nil)
	})
	if pooled == nil {
		return true, "", &models.PluginInternalError{PluginID: record.ID, TypeKey: typeKey, Cause: "load: lua state pool exhausted"}
	}
	rebindPooledState(pooled, ctx)
	defer func() {
		if recovered := recover(); recovered != nil {
			if !pooled.IsClosed() {
				pooled.Close()
			}
			panic(recovered)
		}
		statepool.Return(bucketKey, pooled)
	}()
	L := pooled

	if err := L.DoString(string(record.Source)); err != nil {
		cause := luaErrorString(L.Get(-1))
		if cause == "" || cause == "nil" {
			cause = err.Error()
		}
		cause = fmt.Sprintf("%s (source %d bytes)", cause, len(record.Source))
		s.recordCrash(record.ID, typeKey, s.crashPrefix(ctx)+"load: "+cause)
		s.logHandlerLoadFailed(record.ID, typeKey, handler, ctx.jobName, ctx.model.String(), s.crashPrefix(ctx)+"load: "+cause)
		return true, "", &models.PluginInternalError{PluginID: record.ID, TypeKey: typeKey, Cause: "load: " + cause}
	}
	handlers, ok := ctx.registrations[typeKey]
	if !ok {
		if srcHandlers, isSource := ctx.proxySources[typeKey]; isSource {
			handlers = srcHandlers
		} else {
			s.logHandlerMissing(record.ID, typeKey, handler, ctx.jobName, ctx.model.String())
			return false, "", &notFoundError{PluginID: record.ID, TypeKey: typeKey, Handler: handler}
		}
	}
	handlerFunction := lookupCallable(handlers, handler)
	if handlerFunction == nil {
		// Job specifications carry colocated run functions under jobs.<name>.run.
		if jobFunction := lookupJobRun(handlers, handler); jobFunction != nil {
			handlerFunction = jobFunction
		} else {
			s.logHandlerMissing(record.ID, typeKey, handler, ctx.jobName, ctx.model.String())
			return false, "", &notFoundError{PluginID: record.ID, TypeKey: typeKey, Handler: handler}
		}
	}

	top := L.GetTop()
	L.Push(handlerFunction)
	if pushArgs != nil {
		pushArgs(L)
	}
	argumentCount := L.GetTop() - top - 1
	if callError := L.PCall(argumentCount, returnCount, nil); callError != nil {
		err := s.luaCallError(record, typeKey, L, callError, s.crashPrefix(ctx))
		s.logHandlerCallFailed(record.ID, typeKey, handler, ctx.jobName, ctx.model.String(), err)
		L.SetTop(top)
		return true, "", err
	}
	defer L.SetTop(top)
	if applyRet != nil {
		if err := applyRet(L); err != nil {
			s.logHandlerCallFailed(record.ID, typeKey, handler, ctx.jobName, ctx.model.String(), err)
			return true, "", err
		}
	}
	return true, "", nil
}

// poolBucketKey qualifies a lookup key with the owning plugin identifier so
// unrelated plugins sharing a declared name never share pooled states.
func poolBucketKey(record *PluginRecord, lookupKey string) string {
	if record == nil || lookupKey == "" {
		return ""
	}
	return record.ID + "\x00" + lookupKey
}

// rebindPooledState prepares a pooled state for one call: the stack clears,
// per-call globals reinstall with the current context, and cancellation
// follows the caller context.
func rebindPooledState(state *lua.LState, ctx *executionContext) {
	state.SetTop(0)
	installPrint(state, ctx)
	installRouterTable(state, ctx)
	if ctx != nil && ctx.goCtx != nil {
		state.SetContext(ctx.goCtx)
	} else {
		state.SetContext(context.Background())
	}
}

// handlerCallRouted is handlerCallEx plus a route placeholder kept for
// caller compatibility. Proxy routes no longer exist: it always returns "".
func (s *Service) handlerCallRouted(
	goCtx context.Context,
	record *PluginRecord,
	handlerMetadata HandlerMeta,
	handler string,
	pushArgs func(L *lua.LState),
	returnCount int,
	applyRet func(L *lua.LState) error,
) (found bool, route string, err error) {
	return s.handlerCallEx(goCtx, record, handlerMetadata, handler, pushArgs, returnCount, applyRet, nil)
}

func (s *Service) crashPrefix(ctx *executionContext) string {
	if ctx != nil && ctx.jobName != "" {
		return "job " + ctx.jobName + ": "
	}
	return ""
}

func lookupCallable(handlers *lua.LTable, handler string) *lua.LFunction {
	function, ok := handlers.RawGetString(handler).(*lua.LFunction)
	if !ok {
		return nil
	}
	return function
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
	function, ok := entry.RawGetString("run").(*lua.LFunction)
	if !ok {
		return nil
	}
	return function
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
// to 502; headers is an optional string-to-string table rendered through
// the HTTP layer allow-list. Anything else is not a terminal error.
func asTerminalError(v lua.LValue) (*models.ProviderError, bool) {
	table, ok := v.(*lua.LTable)
	if !ok {
		return nil, false
	}
	msgVal, ok := table.RawGetString("message").(lua.LString)
	if !ok || strings.TrimSpace(string(msgVal)) == "" {
		return nil, false
	}
	status := 502
	if sv := table.RawGetString("status"); sv != lua.LNil {
		n, ok := sv.(lua.LNumber)
		if !ok || int(n) < 400 || int(n) > 599 {
			return nil, false
		}
		status = int(n)
	}
	code := "server_error"
	if cv := table.RawGetString("code"); cv != lua.LNil {
		cs, ok := cv.(lua.LString)
		if !ok || strings.TrimSpace(string(cs)) == "" {
			return nil, false
		}
		code = string(cs)
	}
	param := ""
	if pv := table.RawGetString("param"); pv != lua.LNil {
		ps, ok := pv.(lua.LString)
		if !ok {
			return nil, false
		}
		param = string(ps)
	}
	headers := readStringTable(table.RawGetString("headers"))
	return &models.ProviderError{StatusCode: status, Message: string(msgVal), Code: code, Param: param, Headers: headers}, true
}

func readStringTable(v lua.LValue) map[string]string {
	table, ok := v.(*lua.LTable)
	if !ok {
		return nil
	}
	out := map[string]string{}
	table.ForEach(func(k, val lua.LValue) {
		ks, ok := k.(lua.LString)
		if !ok {
			return
		}
		vs, ok := val.(lua.LString)
		if !ok {
			return
		}
		out[string(ks)] = string(vs)
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

// readStringTable converts an optional Lua string-to-string table into a Go
// map. Non-table values yield nil; non-string keys or values are dropped so
// a malformed table never fails the error it rides on.

// luaCallError converts a failed PCall into ProviderError or PluginInternalError.
func (s *Service) luaCallError(record *PluginRecord, typeKey string, L *lua.LState, callError error, prefix string) error {
	top := L.GetTop()
	if top >= 1 {
		if perr, ok := asTerminalError(L.Get(top)); ok {
			return perr
		}
	}
	cause := luaErrorString(L.Get(-1))
	if callError != nil && (cause == "" || cause == "nil") {
		cause = callError.Error()
	}
	if goCtx := L.Context(); goCtx != nil {
		if ctxErr := goCtx.Err(); ctxErr != nil {
			cause = "cancelled: " + ctxErr.Error()
		}
	}
	cause = prefix + cause
	s.recordCrash(record.ID, typeKey, cause)
	return &models.PluginInternalError{PluginID: record.ID, TypeKey: typeKey, Cause: cause}
}

// splitReturn reads the (result, err) pair left by a 2-value handler call.
// Stack layout: [..., result, err].
func splitReturn(L *lua.LState) (result lua.LValue, returnError lua.LValue) {
	top := L.GetTop()
	if top < 2 {
		return lua.LNil, lua.LNil
	}
	return L.Get(top - 1), L.Get(top)
}

// contractErrOrInternal resolves the err slot of a (result, err) return:
// a valid terminal table becomes ProviderError, any other non-nil value
// becomes PluginInternalError.
func (s *Service) contractErrOrInternal(record *PluginRecord, typeKey string, returnError lua.LValue) error {
	if returnError == nil || returnError == lua.LNil {
		return nil
	}
	if perr, ok := asTerminalError(returnError); ok {
		return perr
	}
	cause := "handler returned invalid error value: " + luaErrorString(returnError)
	s.recordCrash(record.ID, typeKey, cause)
	return &models.PluginInternalError{PluginID: record.ID, TypeKey: typeKey, Cause: cause}
}

// logHandlerLoadFailed records a source load failure as a warning with
// handler context.
func (s *Service) logHandlerLoadFailed(pluginID, typeKey, handler, jobName, model, cause string) {
	logger := s.getLogger()
	if logger == nil {
		return
	}
	arguments := []any{"plugin_id", pluginID, "type", typeKey, "endpoint", handler, "error", cause}
	if jobName != "" {
		arguments = append(arguments, "job", jobName)
	}
	if model != "" {
		arguments = append(arguments, "model", model)
	}
	logger.Warn("Plugin source load failed", arguments...)
}

// logHandlerMissing records a missing handler registration at debug level:
// optional handlers miss as part of normal fallback.
func (s *Service) logHandlerMissing(pluginID, typeKey, handler, jobName, model string) {
	logger := s.getLogger()
	if logger == nil {
		return
	}
	arguments := []any{"plugin_id", pluginID, "type", typeKey, "endpoint", handler}
	if jobName != "" {
		arguments = append(arguments, "job", jobName)
	}
	if model != "" {
		arguments = append(arguments, "model", model)
	}
	logger.Debug("Plugin handler missing", arguments...)
}

// logHandlerCallFailed records a handler invocation outcome with context.
// Terminal provider errors are expected request failures at debug level;
// internal errors are plugin crashes at warning level.
func (s *Service) logHandlerCallFailed(pluginID, typeKey, handler, jobName, model string, callError error) {
	if callError == nil {
		return
	}
	logger := s.getLogger()
	if logger == nil {
		return
	}
	var providerError *models.ProviderError
	if isTerminalProviderError(callError, &providerError) {
		arguments := []any{
			"plugin_id", pluginID, "type", typeKey, "endpoint", handler,
			"code", providerError.Code, "status", providerError.StatusCode, "error", providerError.Message,
		}
		if jobName != "" {
			arguments = append(arguments, "job", jobName)
		}
		if model != "" {
			arguments = append(arguments, "model", model)
		}
		logger.Debug("Plugin handler failed", arguments...)
		return
	}
	arguments := []any{"plugin_id", pluginID, "type", typeKey, "endpoint", handler, "error", callError.Error()}
	if jobName != "" {
		arguments = append(arguments, "job", jobName)
	}
	if model != "" {
		arguments = append(arguments, "model", model)
	}
	logger.Warn("Plugin handler crashed", arguments...)
}
