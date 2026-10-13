package luaplugin

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// Lifecycle hook reasons. initialize runs once per plugin event; ctx carries
// reason only. deinitialize with reason "disable" for a provider transition
// additionally carries the disabled provider identity (provider_id plus
// provider_config); the plugin-disable path reuses "disable" with reason
// only and no provider identity.
const (
	InitializeReasonInstall = "install"
	InitializeReasonUpdate  = "update"
	InitializeReasonStartup = "startup"
	InitializeReasonEnable  = "enable"
)

const (
	DeinitializeReasonDelete   = "delete"
	DeinitializeReasonUpdate   = "update"
	DeinitializeReasonShutdown = "shutdown"
	DeinitializeReasonDisable  = "disable"
)

// lifecycleHookTimeout bounds one hook call. Every hook fails open: timeout,
// crash or terminal error logs and the triggering operation proceeds.
const lifecycleHookTimeout = 30 * time.Second

// lifecycleMaxWorkers bounds concurrent hook calls. Sweeps fan out across
// plugin types instead of running sequentially; one slow upstream never
// holds the whole sweep.
const lifecycleMaxWorkers = 4

// validLifecycleReason reports whether reason selects a known hook event.
// Unknown reasons fail closed: no hook runs.
func validLifecycleReason(handler, reason string) bool {
	switch handler {
	case string(HandlerInitialize):
		switch reason {
		case InitializeReasonInstall, InitializeReasonUpdate, InitializeReasonStartup, InitializeReasonEnable:
			return true
		}
	case string(HandlerDeinitialize):
		switch reason {
		case DeinitializeReasonDelete, DeinitializeReasonUpdate, DeinitializeReasonShutdown, DeinitializeReasonDisable:
			return true
		}
	}
	return false
}

// recordDeclares reports whether a stored record snapshot declares a handler
// for a type key. Snapshots may postdate the registry (update deinitializes
// the replaced source), so lifecycle paths check the record they run.
func recordDeclares(record *PluginRecord, typeKey, handler string) bool {
	if record == nil {
		return false
	}
	for _, name := range record.Handlers[typeKey] {
		if name == handler {
			return true
		}
	}
	return false
}

// runLifecycleHook invokes one optional lifecycle slot of one type key.
// Missing slots skip silently; every other outcome logs through the shared
// crash path and returns the error to the caller for fan-out accounting.
func (s *Service) runLifecycleHook(
	goCtx context.Context,
	record *PluginRecord,
	typeKey, handler, reason, providerID string,
	providerConfig map[string]any,
) error {
	if !validLifecycleReason(handler, reason) {
		return &models.PluginInternalError{PluginID: record.ID, TypeKey: typeKey, Cause: handler + " called with unknown reason " + reason}
	}
	hookCtx, cancel := context.WithTimeout(goCtx, lifecycleHookTimeout)
	defer cancel()
	caps := capabilitiesForRecord(record, typeKey)
	hookErr := error(nil)
	metadata := HandlerMeta{ProviderID: providerID, TypeKey: typeKey, ProviderConfig: providerConfig}
	found, _, err := s.handlerCallEx(hookCtx, record, metadata, handler, func(L *lua.LState) {
		table := ctxTable(L, "", providerConfig)
		table.RawSetString("reason", lua.LString(reason))
		if providerID != "" {
			table.RawSetString("provider_id", lua.LString(providerID))
		}
		L.Push(table)
	}, 1, func(L *lua.LState) error {
		value := L.Get(-1)
		if value == lua.LNil {
			return nil
		}
		if boolean, ok := value.(lua.LBool); ok && bool(boolean) {
			return nil
		}
		if terminal, ok := asTerminalError(value); ok {
			hookErr = terminal
			return nil
		}
		cause := handler + " must return nil, true or an error table"
		s.recordCrash(record.ID, typeKey, cause)
		hookErr = &models.PluginInternalError{PluginID: record.ID, TypeKey: typeKey, Cause: cause}
		return hookErr
	}, &callConfiguration{
		providerID:           providerID,
		providerConfig:       providerConfig,
		timeoutMs:            defaultHTTPTimeoutMs,
		capabilitiesOverride: &caps,
		lifecycleReadOnly:    true,
	})
	if err != nil {
		return err
	}
	if !found {
		return &notFoundError{PluginID: record.ID, TypeKey: typeKey, Handler: handler}
	}
	return hookErr
}

// RunInitialize runs the initialize slot of every declaring type in a plugin
// record with one reason. Calls fan out concurrently; the call waits for all
// of them. Detach (go) at call sites that must not wait: install, update and
// rollback handlers. Unknown reasons fail closed before any hook runs.
func (s *Service) RunInitialize(goCtx context.Context, record *PluginRecord, reason string) error {
	return s.runRecordHooks(goCtx, []*PluginRecord{record}, string(HandlerInitialize), reason, "", nil)
}

// RunDeinitialize runs the deinitialize slot of every declaring type in a
// plugin record with one reason. providerID and providerConfig travel only
// for the disable reason; every other reason passes empty values. Calls fan
// out concurrently; the call waits for all of them. Detach (go) at call
// sites that must not wait: delete, disable and shutdown paths.
func (s *Service) RunDeinitialize(goCtx context.Context, record *PluginRecord, reason, providerID string, providerConfig map[string]any) error {
	return s.runRecordHooks(goCtx, []*PluginRecord{record}, string(HandlerDeinitialize), reason, providerID, providerConfig)
}

// RunStartupInitializers runs initialize with reason startup for every
// enabled plugin type that declares it. Disabled plugins stay skipped: they
// left the registry on disable and re-enter on enable. The caller bounds the
// whole sweep with its gate context (the server startup gate); slow hooks
// cancel with it.
func (s *Service) RunStartupInitializers(goCtx context.Context) error {
	records, err := s.ListSQLite(goCtx)
	if err != nil {
		return err
	}
	return s.runRecordHooks(goCtx, enabledRecords(records), string(HandlerInitialize), InitializeReasonStartup, "", nil)
}

// RunShutdownDeinitializers runs deinitialize with reason shutdown for every
// enabled plugin type that declares it. Disabled plugins stay skipped: they
// already ran deinitialize on disable. The caller bounds the whole sweep
// with its gate context; shutdown proceeds when the gate expires.
func (s *Service) RunShutdownDeinitializers(goCtx context.Context) error {
	records, err := s.ListSQLite(goCtx)
	if err != nil {
		return err
	}
	return s.runRecordHooks(goCtx, enabledRecords(records), string(HandlerDeinitialize), DeinitializeReasonShutdown, "", nil)
}

// enabledRecords filters installed plugin records to enabled rows only.
// Disabled rows stay inventoried but never run hooks, jobs, or handlers.
func enabledRecords(records []*PluginRecord) []*PluginRecord {
	filtered := make([]*PluginRecord, 0, len(records))
	for _, record := range records {
		if record == nil || record.Disabled {
			continue
		}
		filtered = append(filtered, record)
	}
	return filtered
}

// launchLifecycleHook runs one record sweep detached. Lua execution stays
// outside transactions; failures log and never block the caller. Install,
// update, rollback and delete paths use it so dashboard calls return while
// hooks finish in the background.
func (s *Service) launchLifecycleHook(record *PluginRecord, handler, reason string) {
	if record == nil {
		return
	}
	go func() {
		hookCtx, cancel := context.WithTimeout(context.Background(), lifecycleHookTimeout)
		defer cancel()
		var err error
		if handler == string(HandlerInitialize) {
			err = s.RunInitialize(hookCtx, record, reason)
		} else {
			err = s.RunDeinitialize(hookCtx, record, reason, "", nil)
		}
		if err != nil && s.logger != nil {
			s.logger.Warn("Plugin lifecycle hook failed",
				"plugin_id", record.ID, "endpoint", handler, "reason", reason, "error", err.Error())
		}
	}()
}

// NotifyProviderDisabled fires deinitialize with reason disable for the
// types served by one provider snapshot. It is the single entry point for
// provider-disable lifecycle events: the provider service calls it exactly
// when a provider transitions from enabled to disabled or is deleted while
// enabled. Lookup misses (built-in Go backends, uninstalled plugins) and
// types without the slot skip silently. Execution runs detached and never
// blocks the caller.
func (s *Service) NotifyProviderDisabled(instance *models.ProviderInstance) {
	if instance == nil {
		return
	}
	record, err := s.Lookup(instance.TypeKey)
	if err != nil {
		s.logLookupFailed(instance.TypeKey, string(HandlerDeinitialize), err)
		return
	}
	providerID := instance.ID
	providerConfig := instance.Config
	go func() {
		hookCtx, cancel := context.WithTimeout(context.Background(), lifecycleHookTimeout)
		defer cancel()
		if hookErr := s.RunDeinitialize(hookCtx, record, DeinitializeReasonDisable, providerID, providerConfig); hookErr != nil && s.logger != nil {
			s.logger.Warn("Plugin deinitialize on provider disable failed",
				"plugin_id", record.ID, "provider_id", providerID, "error", hookErr.Error())
		}
	}()
}

// lifecycleTask is one hook invocation of one type served by one record.
type lifecycleTask struct {
	record         *PluginRecord
	typeKey        string
	providerID     string
	providerConfig map[string]any
}

// runRecordHooks fans out one handler across the declaring types of the
// given records with a bounded worker pool. Declaring nothing succeeds
// without a call. One hook failure never skips the others; the first error
// returns after every worker finishes.
func (s *Service) runRecordHooks(
	goCtx context.Context,
	records []*PluginRecord,
	handler, reason, providerID string,
	providerConfig map[string]any,
) error {
	if !validLifecycleReason(handler, reason) {
		return fmt.Errorf("luaplugin: %s called with unknown reason %q", handler, reason)
	}
	var tasks []lifecycleTask
	seen := map[string]bool{}
	for _, record := range records {
		if record == nil {
			continue
		}
		keys := append([]string(nil), record.TypeKeys...)
		sort.Strings(keys)
		for _, key := range keys {
			if seen[record.ID+"\x00"+key] || !recordDeclares(record, key, handler) {
				continue
			}
			seen[record.ID+"\x00"+key] = true
			tasks = append(tasks, lifecycleTask{record: record, typeKey: key, providerID: providerID, providerConfig: providerConfig})
		}
	}
	if len(tasks) == 0 {
		return nil
	}
	var group sync.WaitGroup
	semaphore := make(chan struct{}, lifecycleMaxWorkers)
	var firstErr atomicError
loop:
	for _, task := range tasks {
		select {
		case <-goCtx.Done():
			break loop
		case semaphore <- struct{}{}:
		}
		group.Add(1)
		go func(task lifecycleTask) {
			defer group.Done()
			defer func() { <-semaphore }()
			if err := s.runLifecycleHook(goCtx, task.record, task.typeKey, handler, reason, task.providerID, task.providerConfig); err != nil {
				firstErr.store(err)
			}
		}(task)
	}
	group.Wait()
	return firstErr.load()
}

// atomicError keeps the first hook error across fan-out workers.
type atomicError struct {
	mu  sync.Mutex
	err error
}

func (a *atomicError) store(err error) {
	if err == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err == nil {
		a.err = err
	}
}

func (a *atomicError) load() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}
