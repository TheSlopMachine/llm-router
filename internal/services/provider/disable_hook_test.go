package provider

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

const disableHookPluginSource = `--- @plugin Disable Hook Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.2
--- @allow_host example.com

llm_router.register("disable-hooks-type", {
  complete = function(ctx, request)
    return nil, { message = "unreachable" }
  end,
  deinitialize = function(ctx)
    local entry = { reason = ctx.reason }
    if ctx.provider_id ~= nil then entry.provider = ctx.provider_id end
    llm_router.storage.set("hooks", "deinit", entry)
    print("deinit:" .. ctx.reason)
  end,
})
`

// hookDeinitLogLength waits for the disable hook's log line, then returns the
// log length. The hook writes storage before its print line lands, so
// snapshotting right after the storage poll races a late first line and
// misreads it as a refire on the repeat disable below.
func hookDeinitLogLength(t *testing.T, luaService *luaplugin.Service, pluginID string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := luaService.Logs(context.Background(), pluginID)
		if err != nil {
			t.Fatalf("logs: %v", err)
		}
		for _, entry := range entries {
			if strings.Contains(entry.Message, "deinit:disable") {
				return len(entries)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("disable hook log line never landed")
	return 0
}

// hookDeinitEntry polls plugin storage for the disable hook outcome.
func hookDeinitEntry(t *testing.T, luaService *luaplugin.Service, pluginID, providerID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		value, err := luaService.GetStorageSQLite(context.Background(), pluginID, "hooks", "deinit")
		if err != nil {
			t.Fatalf("storage read: %v", err)
		}
		if mapped, ok := value.(map[string]any); ok && mapped["reason"] == "disable" && mapped["provider"] == providerID {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("deinitialize on provider disable never ran")
}

func TestDisableTransitionFiresDeinitializeOnce(t *testing.T) {
	ctx := context.Background()
	database := testutil.SetupTestSQLite(t)
	luaService, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("new plugin service: %v", err)
	}
	service := NewGorm(database)
	service.SetLuaService(luaService)
	record, err := luaService.InstallSQLite(ctx, []byte(disableHookPluginSource), luaplugin.PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	created, err := service.Create(ctx, CreateOptions{Name: "Hooked", TypeKey: "disable-hooks-type"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	disabled := true
	if _, err := service.Update(ctx, created.ID, UpdateOptions{Name: "Hooked", Disabled: &disabled}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	hookDeinitEntry(t, luaService, record.ID, created.ID)
	before := hookDeinitLogLength(t, luaService, record.ID)
	if _, err := service.Update(ctx, created.ID, UpdateOptions{Name: "Hooked", Disabled: &disabled}); err != nil {
		t.Fatalf("repeat disable: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	afterEntries, err := luaService.Logs(ctx, record.ID)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	for _, entry := range afterEntries[before:] {
		if strings.Contains(entry.Message, "deinit:disable") {
			afterReread, err := luaService.Logs(ctx, record.ID)
			if err != nil {
				t.Fatalf("logs: %v", err)
			}
			t.Fatalf("repeat disable must not refire the hook: %+v", afterReread)
		}
	}
}
