package healthcheck

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	stdErrors "errors"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
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

// fakeCredentials is an in-memory CredentialStore with mutex-guarded state:
// DisableUnhealthy runs on the service background goroutine while test
// polling closures read the same entries.
type fakeCredentials struct {
	mu          sync.Mutex
	credentials map[string]*models.Credential
	disabled    map[string]string
}

func (f *fakeCredentials) Get(_ context.Context, id string) (*models.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	credential := f.credentials[id]
	if credential == nil {
		return nil, nil
	}
	snapshot := *credential
	return &snapshot, nil
}

func (f *fakeCredentials) DisableUnhealthy(_ context.Context, id, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disabled[id] = reason
	f.credentials[id].Disabled = true
	f.credentials[id].DisabledBy = "healthcheck"
	return nil
}

func (f *fakeCredentials) isDisabled(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.credentials[id].Disabled
}

func (f *fakeCredentials) disabledBy(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.credentials[id].DisabledBy
}

func (f *fakeCredentials) disableCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.disabled)
}

// fakeProviders is an in-memory ProviderConfigs.
type fakeProviders struct {
	configs map[string]map[string]any
}

func (f *fakeProviders) Get(_ context.Context, id string) (*models.ProviderInstance, error) {
	providerConfig, ok := f.configs[id]
	if !ok {
		return nil, stdErrors.New("provider not found")
	}
	return &models.ProviderInstance{ID: id, Config: providerConfig}, nil
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

// seedHealthParents inserts the provider and credential rows the
// credential_health foreign key requires. Health rows cascade from them.
func seedHealthParents(t *testing.T, ctx context.Context, service *GormService, credentialIDs ...string) {
	t.Helper()
	now := time.Now().UTC()
	providers := repository.NewGormRepository[record.ProviderInstance](service.database.Handle, "id")
	if err := providers.Upsert(ctx, record.ProviderInstance{ID: "p", Name: "p", TypeKey: "custom", Settings: map[string]any{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	credentials := repository.NewGormRepository[record.Credential](service.database.Handle, "id")
	for _, id := range credentialIDs {
		if err := credentials.Upsert(ctx, record.Credential{ID: id, ProviderID: "p", Data: map[string]any{}, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("seed credential %s: %v", id, err)
		}
	}
}

// healthGormStack builds a GormService with stub lua, credentials and
// providers on a fresh SQLite database. The narrow interfaces enter as
// stubs; no provider, credential or luaplugin service source is involved.
func healthGormStack(t *testing.T, lua *fakeLua, credentialIDs ...string) (*GormService, *fakeCredentials) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	credentials := &fakeCredentials{
		credentials: map[string]*models.Credential{},
		disabled:    map[string]string{},
	}
	for _, id := range credentialIDs {
		credentials.credentials[id] = &models.Credential{ID: id, ProviderID: "p"}
	}
	providers := &fakeProviders{configs: map[string]map[string]any{
		"p": {"disable_failed_credentials": true},
	}}
	service := NewGormService(database, lua, credentials, providers)
	seedHealthParents(t, context.Background(), service, credentialIDs...)
	return service, credentials
}

func TestGormSuspectFailed_UnhealthyDisables(t *testing.T) {
	lua := &fakeLua{
		handlers: map[string]bool{"t/check_health": true},
		cooldown: time.Minute,
		verdict:  map[string]luaplugin.HealthStatus{"c1": luaplugin.HealthUnhealthy},
	}
	service, credentials := healthGormStack(t, lua, "c1")
	service.SuspectFailed("plug", "t", "c1")
	waitFor(t, "disable", func() bool { return credentials.isDisabled("c1") })
	if got := credentials.disabledBy("c1"); got != "healthcheck" {
		t.Fatalf("disabled_by = %q, want healthcheck", got)
	}
	if lua.calls.Load() != 1 {
		t.Fatalf("checks = %d, want 1", lua.calls.Load())
	}
	entry, err := service.Get(context.Background(), "c1")
	if err != nil {
		t.Fatalf("get stamped verdict: %v", err)
	}
	if entry.LastStatus != string(luaplugin.HealthUnhealthy) {
		t.Fatalf("status = %q, want unhealthy", entry.LastStatus)
	}
	if entry.LastCheckAt.IsZero() {
		t.Fatal("last check time is zero, want stamped time")
	}
}

func TestGormSuspectFailed_AutomationOffKeeps(t *testing.T) {
	lua := &fakeLua{
		handlers: map[string]bool{"t/check_health": true},
		cooldown: time.Minute,
		verdict:  map[string]luaplugin.HealthStatus{"c1": luaplugin.HealthUnhealthy},
	}
	database := testutil.SetupTestSQLite(t)
	credentials := &fakeCredentials{
		credentials: map[string]*models.Credential{"c1": {ID: "c1", ProviderID: "p"}},
		disabled:    map[string]string{},
	}
	// Switch absent (dashboard default off): verdict records, no disable.
	service := NewGormService(database, lua, credentials, &fakeProviders{configs: map[string]map[string]any{}})
	seedHealthParents(t, context.Background(), service, "c1")
	service.SuspectFailed("plug", "t", "c1")
	waitFor(t, "check", func() bool { return lua.calls.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	if credentials.isDisabled("c1") {
		t.Fatal("automation off must keep the credential enabled")
	}
	if got := credentials.disableCount(); got != 0 {
		t.Fatalf("no disable must record, got %d entries", got)
	}
	entry, err := service.Get(context.Background(), "c1")
	if err != nil {
		t.Fatalf("get stamped verdict: %v", err)
	}
	if entry.LastStatus != string(luaplugin.HealthUnhealthy) {
		t.Fatalf("status = %q, want unhealthy", entry.LastStatus)
	}
}

func TestGormSuspectFailed_HealthyAndUnknownKeep(t *testing.T) {
	for id, verdict := range map[string]luaplugin.HealthStatus{
		"c1": luaplugin.HealthHealthy,
		"c2": luaplugin.HealthUnknown,
	} {
		lua := &fakeLua{
			handlers: map[string]bool{"t/check_health": true},
			cooldown: time.Minute,
			verdict:  map[string]luaplugin.HealthStatus{id: verdict},
		}
		database := testutil.SetupTestSQLite(t)
		credentials := &fakeCredentials{
			credentials: map[string]*models.Credential{id: {ID: id, ProviderID: "p"}},
			disabled:    map[string]string{},
		}
		service := NewGormService(database, lua, credentials, nil)
		seedHealthParents(t, context.Background(), service, id)
		service.SuspectFailed("plug", "t", id)
		waitFor(t, "check", func() bool { return lua.calls.Load() == 1 })
		if credentials.isDisabled(id) {
			t.Fatalf("%s must stay enabled", verdict)
		}
	}
}

func TestGormSuspectFailed_CooldownBounds(t *testing.T) {
	lua := &fakeLua{
		handlers: map[string]bool{"t/check_health": true},
		cooldown: time.Hour,
	}
	service, _ := healthGormStack(t, lua, "c1")
	service.SuspectFailed("plug", "t", "c1")
	service.SuspectFailed("plug", "t", "c1")
	waitFor(t, "first check", func() bool { return lua.calls.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	if got := lua.calls.Load(); got != 1 {
		t.Fatalf("cooldown must bound checks, got %d", got)
	}
}

func TestGormSuspectFailed_MissingHandlerNeverChecks(t *testing.T) {
	lua := &fakeLua{handlers: map[string]bool{}, cooldown: time.Minute}
	service, _ := healthGormStack(t, lua, "c1")
	service.SuspectFailed("plug", "t", "c1")
	time.Sleep(50 * time.Millisecond)
	if got := lua.calls.Load(); got != 0 {
		t.Fatalf("types without check_health must never check, got %d", got)
	}
}

func TestGormSuspectFailed_Singleflight(t *testing.T) {
	lua := &fakeLua{
		handlers:       map[string]bool{"t/check_health": true},
		cooldown:       time.Minute,
		releaseChannel: make(chan struct{}),
	}
	service, _ := healthGormStack(t, lua, "c1")
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

func TestGormGet_MissReturnsNotFound(t *testing.T) {
	lua := &fakeLua{handlers: map[string]bool{}, cooldown: time.Minute}
	service, _ := healthGormStack(t, lua, "c1")
	if _, err := service.Get(context.Background(), "c-missing"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("error is %v, want not found sentinel", err)
	}
}

func TestGormStamp_RoundTrip(t *testing.T) {
	lua := &fakeLua{handlers: map[string]bool{}, cooldown: time.Minute}
	service, _ := healthGormStack(t, lua, "c1")
	before := time.Now().UTC()
	if err := service.Stamp(context.Background(), "c1", string(luaplugin.HealthHealthy)); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	got, err := service.Get(context.Background(), "c1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CredentialID != "c1" || got.LastStatus != string(luaplugin.HealthHealthy) {
		t.Fatalf("record = %+v, want c1/healthy", got)
	}
	if got.LastCheckAt.Before(before) || time.Since(got.LastCheckAt) > time.Minute {
		t.Fatalf("last check = %v, want time near %v", got.LastCheckAt, before)
	}
}

func TestGormStamp_Overwrites(t *testing.T) {
	lua := &fakeLua{handlers: map[string]bool{}, cooldown: time.Minute}
	service, _ := healthGormStack(t, lua, "c1")
	if err := service.Stamp(context.Background(), "c1", string(luaplugin.HealthHealthy)); err != nil {
		t.Fatalf("stamp healthy: %v", err)
	}
	if err := service.Stamp(context.Background(), "c1", string(luaplugin.HealthUnhealthy)); err != nil {
		t.Fatalf("stamp unhealthy: %v", err)
	}
	got, err := service.Get(context.Background(), "c1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastStatus != string(luaplugin.HealthUnhealthy) {
		t.Fatalf("status = %q, want unhealthy", got.LastStatus)
	}
}

func TestGormStamp_MissingCredentialPersists(t *testing.T) {
	lua := &fakeLua{handlers: map[string]bool{}, cooldown: time.Minute}
	service, _ := healthGormStack(t, lua, "c1")
	// Health rows reference credentials softly across files: a stamp for a
	// missing credential persists instead of failing, and readers skip
	// verdicts without a credential row.
	if err := service.Stamp(context.Background(), "c-missing", string(luaplugin.HealthHealthy)); err != nil {
		t.Fatalf("stamp missing credential: %v, want success with soft reference", err)
	}
	got, err := service.Get(context.Background(), "c-missing")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastStatus != string(luaplugin.HealthHealthy) {
		t.Fatalf("status = %q, want healthy", got.LastStatus)
	}
	if err := service.Stamp(context.Background(), "", string(luaplugin.HealthHealthy)); err == nil {
		t.Fatal("empty credential id succeeded, want id required error")
	}
}

func TestGormConverter_RoundTrip(t *testing.T) {
	stampedAt := time.Now().UTC()
	entry := &Record{CredentialID: "c1", LastCheckAt: stampedAt, LastStatus: string(luaplugin.HealthUnknown)}
	restored := fromHealthRow(toHealthRow(entry))
	if restored.CredentialID != entry.CredentialID || restored.LastStatus != entry.LastStatus {
		t.Fatalf("converter = %+v, want %+v", restored, entry)
	}
	if !restored.LastCheckAt.Equal(entry.LastCheckAt) {
		t.Fatalf("converter time = %v, want %v", restored.LastCheckAt, entry.LastCheckAt)
	}
}
