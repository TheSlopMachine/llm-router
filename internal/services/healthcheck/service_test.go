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
	handlers       map[string]bool
	cooldown       time.Duration
	verdict        map[string]luaplugin.HealthStatus
	calls          atomic.Int32
	releaseChannel chan struct{}
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

func (f *fakeLua) CheckHealth(ctx context.Context, typeKey string, credential *models.Credential) (luaplugin.HealthStatus, string, error) {
	f.calls.Add(1)
	if f.releaseChannel != nil {
		select {
		case <-f.releaseChannel:
		case <-ctx.Done():
		}
	}
	if v, ok := f.verdict[credential.ID]; ok {
		return v, "fake " + string(v), nil
	}
	return luaplugin.HealthHealthy, "", nil
}

// fakeCredentials is an in-memory CredentialStore.
type fakeCredentials struct {
	credentials map[string]*models.Credential
	disabled    map[string]string
}

func (f *fakeCredentials) Get(id string) (*models.Credential, error) {
	return f.credentials[id], nil
}

func (f *fakeCredentials) DisableUnhealthy(id, reason string) error {
	f.disabled[id] = reason
	f.credentials[id].Disabled = true
	f.credentials[id].DisabledBy = "healthcheck"
	return nil
}

// fakeProviders is an in-memory ProviderConfigs.
type fakeProviders struct {
	configs map[string]map[string]any
}

func (f *fakeProviders) Get(id string) (*models.ProviderInstance, error) {
	providerConfig, ok := f.configs[id]
	if !ok {
		return nil, errors.New("provider not found")
	}
	return &models.ProviderInstance{ID: id, Config: providerConfig}, nil
}

func healthStack(t *testing.T, lua *fakeLua) (*Service, *fakeCredentials) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	credentials := &fakeCredentials{
		credentials: map[string]*models.Credential{
			"c1": {ID: "c1", ProviderID: "p"},
		},
		disabled: map[string]string{},
	}
	providers := &fakeProviders{configs: map[string]map[string]any{
		"p": {"disable_failed_credentials": true},
	}}
	service := New(database, lua, credentials, providers)
	return service, credentials
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
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
	service, credentials := healthStack(t, lua)
	service.SuspectFailed("plug", "t", "c1")
	waitFor(t, "disable", func() bool { return credentials.credentials["c1"].Disabled })
	if got := credentials.credentials["c1"].DisabledBy; got != "healthcheck" {
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
	credentials := &fakeCredentials{
		credentials: map[string]*models.Credential{"c1": {ID: "c1", ProviderID: "p"}},
		disabled:    map[string]string{},
	}
	// Switch absent (dashboard default off): verdict records, no disable.
	service := New(database, lua, credentials, &fakeProviders{configs: map[string]map[string]any{}})
	service.SuspectFailed("plug", "t", "c1")
	waitFor(t, "check", func() bool { return lua.calls.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	if credentials.credentials["c1"].Disabled {
		t.Fatal("automation off must keep the credential enabled")
	}
	if len(credentials.disabled) != 0 {
		t.Fatalf("no disable must record, got %v", credentials.disabled)
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
		credentials := &fakeCredentials{
			credentials: map[string]*models.Credential{id: {ID: id, ProviderID: "p"}},
			disabled:    map[string]string{},
		}
		service := New(database, lua, credentials, nil)
		service.SuspectFailed("plug", "t", id)
		waitFor(t, "check", func() bool { return lua.calls.Load() == 1 })
		if credentials.credentials[id].Disabled {
			t.Fatalf("%s must stay enabled", verdict)
		}
	}
}

func TestSuspectFailed_CooldownBounds(t *testing.T) {
	lua := &fakeLua{
		handlers: map[string]bool{"t/check_health": true},
		cooldown: time.Hour,
	}
	service, _ := healthStack(t, lua)
	service.SuspectFailed("plug", "t", "c1")
	service.SuspectFailed("plug", "t", "c1")
	waitFor(t, "first check", func() bool { return lua.calls.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	if got := lua.calls.Load(); got != 1 {
		t.Fatalf("cooldown must bound checks, got %d", got)
	}
}

func TestSuspectFailed_MissingHandlerNeverChecks(t *testing.T) {
	lua := &fakeLua{handlers: map[string]bool{}, cooldown: time.Minute}
	service, _ := healthStack(t, lua)
	service.SuspectFailed("plug", "t", "c1")
	time.Sleep(50 * time.Millisecond)
	if got := lua.calls.Load(); got != 0 {
		t.Fatalf("types without check_health must never check, got %d", got)
	}
}

func TestSuspectFailed_Singleflight(t *testing.T) {
	lua := &fakeLua{
		handlers:       map[string]bool{"t/check_health": true},
		cooldown:       time.Minute,
		releaseChannel: make(chan struct{}),
	}
	service, _ := healthStack(t, lua)
	service.SuspectFailed("plug", "t", "c1")
	waitFor(t, "check start", func() bool { return lua.calls.Load() == 1 })
	service.SuspectFailed("plug", "t", "c1")
	service.SuspectFailed("plug", "t", "c1")
	close(lua.releaseChannel)
	waitFor(t, "check end", func() bool {
		service.mu.Lock()
		defer service.mu.Unlock()
		return !service.inflight["c1"]
	})
	if got := lua.calls.Load(); got != 1 {
		t.Fatalf("concurrent suspects must coalesce, got %d checks", got)
	}
}
