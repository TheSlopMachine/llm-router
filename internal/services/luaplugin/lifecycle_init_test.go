package luaplugin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// pluginLogs reads plugin log lines, failing the test on query errors.
func pluginLogs(t *testing.T, service *Service, pluginID string) []LogEntry {
	t.Helper()
	entries, err := service.Logs(context.Background(), pluginID)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	return entries
}

const hooksPluginSource = `--- @plugin Hooks Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.2
--- @allow_host example.com

llm_router.register("hooks-type", {
  complete = function(ctx, request)
    return {
      id = "chatcmpl-hooks",
      object = "chat.completion",
      created = 1700000000,
      model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = "hello" }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,
  initialize = function(ctx)
    llm_router.storage.set("hooks", "init", { reason = ctx.reason })
    print("init:" .. ctx.reason)
  end,
  deinitialize = function(ctx)
    local entry = { reason = ctx.reason }
    if ctx.provider_id ~= nil then entry.provider = ctx.provider_id end
    llm_router.storage.set("hooks", "deinit", entry)
    print("deinit:" .. ctx.reason)
  end,
  credential_schema = {
    { type = "secret", name = "api_key", label = "API Key" },
  },
})
`

const writeAttemptPluginSource = `--- @plugin Write Attempt Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.2
--- @allow_host example.com

llm_router.register("write-type", {
  complete = function(ctx, request)
    return nil, { message = "unreachable" }
  end,
  initialize = function(ctx)
    llm_router.credentials.disable("c1", "hook must not write")
  end,
  credential_schema = {
    { type = "secret", name = "api_key", label = "API Key" },
  },
})
`

const failingDeinitPluginSource = `--- @plugin Failing Deinit Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.2
--- @allow_host example.com

llm_router.register("fail-type", {
  complete = function(ctx, request)
    return nil, { message = "unreachable" }
  end,
  deinitialize = function(ctx)
    print("deinit-delete-ran")
    return { message = "teardown refused" }
  end,
})
`

// storageReason reads one lifecycle storage entry back as a string map.
func storageReason(t *testing.T, service *Service, pluginID, key string) map[string]any {
	t.Helper()
	value, err := service.GetStorageSQLite(context.Background(), pluginID, "hooks", key)
	if err != nil {
		t.Fatalf("storage read: %v", err)
	}
	if value == nil {
		return nil
	}
	mapped, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("storage entry is %T, want map", value)
	}
	return mapped
}

// pollStorage waits for one lifecycle storage entry to carry a reason.
func pollStorage(t *testing.T, service *Service, pluginID, key, reason string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if mapped := storageReason(t, service, pluginID, key); mapped != nil && mapped["reason"] == reason {
			return mapped
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("storage %q never carried reason %q", key, reason)
	return nil
}

func TestLifecycleSlotsValidate(t *testing.T) {
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(hooksPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	for _, slot := range []string{string(HandlerInitialize), string(HandlerDeinitialize)} {
		if !service.HasHandler("hooks-type", slot) {
			t.Fatalf("HasHandler(%q) is false", slot)
		}
	}
	if !recordDeclares(record, "hooks-type", string(HandlerInitialize)) {
		t.Fatal("record snapshot must declare initialize")
	}
	// Drain the detached install hook so no database write outlives the test.
	pollStorage(t, service, record.ID, "init", InitializeReasonInstall)
}

func TestLifecycleRejectsNonFunctionSlot(t *testing.T) {
	service := setupService(t)
	bad := strings.Replace(hooksPluginSource, "  initialize = function(ctx)\n    llm_router.storage.set(\"hooks\", \"init\", { reason = ctx.reason })\n    print(\"init:\" .. ctx.reason)\n  end,", `  initialize = 42,`, 1)
	if _, err := service.InstallSQLite(context.Background(), []byte(bad), PluginOrigin{Manual: true}); err == nil {
		t.Fatal("non-function initialize must fail install")
	} else if !strings.Contains(err.Error(), "initialize") {
		t.Fatalf("error must name the slot, got %v", err)
	}
}

func TestLifecycleRunInitializeStartup(t *testing.T) {
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(hooksPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	// Drain the detached install hook before the synchronous startup run so
	// the two writes never race on the same storage entry.
	pollStorage(t, service, record.ID, "init", InitializeReasonInstall)
	if err := service.RunInitialize(context.Background(), record, InitializeReasonStartup); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if mapped := storageReason(t, service, record.ID, "init"); mapped["reason"] != InitializeReasonStartup {
		t.Fatalf("init reason: %+v", mapped)
	}
	found := false
	for _, entry := range pluginLogs(t, service, record.ID) {
		if strings.Contains(entry.Message, "init:startup") {
			found = true
		}
	}
	if !found {
		t.Fatalf("logs miss init:startup: %+v", pluginLogs(t, service, record.ID))
	}
}

func TestLifecycleRunDeinitializeDisableCarriesProvider(t *testing.T) {
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(hooksPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	pollStorage(t, service, record.ID, "init", InitializeReasonInstall)
	store := newLifecycleStore()
	service.SetCredentialAccess(
		store.list,
		func(ctx context.Context, id string) (*models.Credential, error) { return store.creds[id], nil },
		func(ctx context.Context, id string, data map[string]any) error { return nil },
	)
	if err := service.RunDeinitialize(context.Background(), record, DeinitializeReasonDisable, "prov-1", map[string]any{}); err != nil {
		t.Fatalf("deinitialize: %v", err)
	}
	mapped := storageReason(t, service, record.ID, "deinit")
	if mapped["reason"] != DeinitializeReasonDisable || mapped["provider"] != "prov-1" {
		t.Fatalf("deinit entry: %+v", mapped)
	}
}

func TestLifecycleUnknownReasonFailsClosed(t *testing.T) {
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(hooksPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	// Drain the detached install hook, then clear its entry so the failed
	// call below must leave storage untouched.
	pollStorage(t, service, record.ID, "init", InitializeReasonInstall)
	if err := service.DeleteStorageSQLite(context.Background(), record.ID, "hooks", "init"); err != nil {
		t.Fatalf("clear storage: %v", err)
	}
	if err := service.RunInitialize(context.Background(), record, "someday"); err == nil {
		t.Fatal("unknown reason must fail")
	}
	if mapped := storageReason(t, service, record.ID, "init"); mapped != nil {
		t.Fatalf("failed hook must not run: %+v", mapped)
	}
}

func TestLifecycleReadOnlyStripsCredentialWrites(t *testing.T) {
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(writeAttemptPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	store := newLifecycleStore()
	service.SetCredentialAccess(
		store.list,
		func(ctx context.Context, id string) (*models.Credential, error) { return store.creds[id], nil },
		func(ctx context.Context, id string, data map[string]any) error { return nil },
	)
	service.SetCredentialLifecycle(store.disable, store.enable, func(providerID string) bool { return true })
	// Drain the detached install hook (it fails the same way) before the
	// synchronous run so no Lua state outlives the test.
	drainDeadline := time.Now().Add(10 * time.Second)
	for len(service.Crashes(record.ID)) == 0 && time.Now().Before(drainDeadline) {
		time.Sleep(20 * time.Millisecond)
	}
	err = service.RunInitialize(context.Background(), record, InitializeReasonInstall)
	var internal *models.PluginInternalError
	if !errors.As(err, &internal) {
		t.Fatalf("credential write in hook must fail loudly, got %T (%v)", err, err)
	}
	if store.creds["c1"].Disabled {
		t.Fatal("stripped write must not mutate")
	}
}

func TestLifecycleInstallDispatchesAsync(t *testing.T) {
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(hooksPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	pollStorage(t, service, record.ID, "init", InitializeReasonInstall)
}

func TestLifecycleDeleteStillDeletesOnHookFailure(t *testing.T) {
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(failingDeinitPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := service.DeleteSQLite(context.Background(), record.ID); err != nil {
		t.Fatalf("delete must proceed despite hook failure: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		ran := false
		for _, entry := range pluginLogs(t, service, record.ID) {
			if strings.Contains(entry.Message, "deinit-delete-ran") {
				ran = true
			}
		}
		if ran {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("deinitialize never ran: %+v", pluginLogs(t, service, record.ID))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := service.GetSQLite(context.Background(), record.ID); err == nil {
		t.Fatal("record must be gone")
	}
}

func TestNotifyProviderDisabledSkipsUnknownType(t *testing.T) {
	service := setupService(t)
	service.NotifyProviderDisabled(&models.ProviderInstance{ID: "ghost", TypeKey: "missing-type"})
	if crashes := service.Crashes("ghost"); len(crashes) != 0 {
		t.Fatalf("unknown type must skip silently: %+v", crashes)
	}
}
