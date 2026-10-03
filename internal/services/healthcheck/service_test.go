package healthcheck

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// fakeLua is a scripted PluginHealth: verdict per credential, call counter,
// optional gate to hold checks open for singleflight assertions.
type fakeLua struct {
	handlers  map[string]bool
	cooldown  time.Duration
	verdict   map[string]luaplugin.HealthStatus
	calls     atomic.Int32
	releaseCh chan struct{}
}

func (f *fakeLua) HasHandler(typeKey, handler string) bool {
	return f.handlers[typeKey+"/"+handler]
}

func (f *fakeLua) HealthCooldown(typeKey string) time.Duration {
	if f.cooldown > 0 {
		return f.cooldown
	}
	return luaplugin.DefaultHealthCooldown
}

func (f *fakeLua) CheckHealth(ctx context.Context, typeKey string, cred *models.Credential) (luaplugin.HealthStatus, string, error) {
	f.calls.Add(1)
	if f.releaseCh != nil {
		select {
		case <-f.releaseCh:
		case <-ctx.Done():
		}
	}
	if v, ok := f.verdict[cred.ID]; ok {
		return v, "fake " + string(v), nil
	}
	return luaplugin.HealthHealthy, "", nil
}

// fakeCreds is an in-memory CredentialStore.
type fakeCreds struct {
	creds    map[string]*models.Credential
	disabled map[string]string
}

func (f *fakeCreds) Get(id string) (*models.Credential, error) {
	return f.creds[id], nil
}

func (f *fakeCreds) DisableUnhealthy(id, reason string) error {
	f.disabled[id] = reason
	f.creds[id].Disabled = true
	f.creds[id].DisabledBy = "healthcheck"
	return nil
}

// fakeProvs is an in-memory ProviderConfigs.
type fakeProvs struct {
	configs map[string]map[string]any
}

func (f *fakeProvs) Get(id string) (*models.ProviderInstance, error) {
	cfg, ok := f.configs[id]
	if !ok {
		return nil, errors.New("provider not found")
	}
	return &models.ProviderInstance{ID: id, Config: cfg}, nil
}

func healthStack(t *testing.T, lua *fakeLua) (*Service, *fakeCreds) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	creds := &fakeCreds{
		creds: map[string]*models.Credential{
			"c1": {ID: "c1", ProviderID: "p"},
		},
		disabled: map[string]string{},
	}
	provs := &fakeProvs{configs: map[string]map[string]any{
		"p": {"disable_failed_credentials": true},
	}}
	svc := New(database, lua, creds, provs)
	return svc, creds
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSuspectFailed_UnhealthyDisables(t *testing.T) {
	lua := &fakeLua{
		handlers: map[string]bool{"t/check_health": true},
		cooldown: time.Minute,
		verdict:  map[string]luaplugin.HealthStatus{"c1": luaplugin.HealthUnhealthy},
	}
	svc, creds := healthStack(t, lua)
	svc.SuspectFailed("plug", "t", "c1")
	waitFor(t, "disable", func() bool { return creds.creds["c1"].Disabled })
	if got := creds.creds["c1"].DisabledBy; got != "healthcheck" {
		t.Fatalf("disabled_by = %q, want healthcheck", got)
	}
	if lua.calls.Load() != 1 {
		t.Fatalf("checks = %d, want 1", lua.calls.Load())
	}
}

func TestSuspectFailed_AutomationOffKeeps(t *testing.T) {
	lua := &fakeLua{
		handlers: map[string]bool{"t/check_health": true},
		cooldown: time.Minute,
		verdict:  map[string]luaplugin.HealthStatus{"c1": luaplugin.HealthUnhealthy},
	}
	database := testutil.SetupTestDB(t)
	creds := &fakeCreds{
		creds:    map[string]*models.Credential{"c1": {ID: "c1", ProviderID: "p"}},
		disabled: map[string]string{},
	}
	// Switch absent (dashboard default off): verdict records, no disable.
	svc := New(database, lua, creds, &fakeProvs{configs: map[string]map[string]any{}})
	svc.SuspectFailed("plug", "t", "c1")
	waitFor(t, "check", func() bool { return lua.calls.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	if creds.creds["c1"].Disabled {
		t.Fatal("automation off must keep the credential enabled")
	}
	if len(creds.disabled) != 0 {
		t.Fatalf("no disable must record, got %v", creds.disabled)
	}
}

func TestSuspectFailed_HealthyAndUnknownKeep(t *testing.T) {
	for id, verdict := range map[string]luaplugin.HealthStatus{
		"c1": luaplugin.HealthHealthy,
		"c2": luaplugin.HealthUnknown,
	} {
		lua := &fakeLua{
			handlers: map[string]bool{"t/check_health": true},
			cooldown: time.Minute,
			verdict:  map[string]luaplugin.HealthStatus{id: verdict},
		}
		database := testutil.SetupTestDB(t)
		creds := &fakeCreds{
			creds:    map[string]*models.Credential{id: {ID: id, ProviderID: "p"}},
			disabled: map[string]string{},
		}
		svc := New(database, lua, creds, nil)
		svc.SuspectFailed("plug", "t", id)
		waitFor(t, "check", func() bool { return lua.calls.Load() == 1 })
		if creds.creds[id].Disabled {
			t.Fatalf("%s must stay enabled", verdict)
		}
	}
}

func TestSuspectFailed_CooldownBounds(t *testing.T) {
	lua := &fakeLua{
		handlers: map[string]bool{"t/check_health": true},
		cooldown: time.Hour,
	}
	svc, _ := healthStack(t, lua)
	svc.SuspectFailed("plug", "t", "c1")
	svc.SuspectFailed("plug", "t", "c1")
	waitFor(t, "first check", func() bool { return lua.calls.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	if got := lua.calls.Load(); got != 1 {
		t.Fatalf("cooldown must bound checks, got %d", got)
	}
}

func TestSuspectFailed_MissingHandlerNeverChecks(t *testing.T) {
	lua := &fakeLua{handlers: map[string]bool{}, cooldown: time.Minute}
	svc, _ := healthStack(t, lua)
	svc.SuspectFailed("plug", "t", "c1")
	time.Sleep(50 * time.Millisecond)
	if got := lua.calls.Load(); got != 0 {
		t.Fatalf("types without check_health must never check, got %d", got)
	}
}

func TestSuspectFailed_Singleflight(t *testing.T) {
	lua := &fakeLua{
		handlers:  map[string]bool{"t/check_health": true},
		cooldown:  time.Minute,
		releaseCh: make(chan struct{}),
	}
	svc, _ := healthStack(t, lua)
	svc.SuspectFailed("plug", "t", "c1")
	waitFor(t, "check start", func() bool { return lua.calls.Load() == 1 })
	svc.SuspectFailed("plug", "t", "c1")
	svc.SuspectFailed("plug", "t", "c1")
	close(lua.releaseCh)
	waitFor(t, "check end", func() bool {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		return !svc.inflight["c1"]
	})
	if got := lua.calls.Load(); got != 1 {
		t.Fatalf("concurrent suspects must coalesce, got %d checks", got)
	}
}
