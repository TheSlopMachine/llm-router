package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/TheSlopMachine/llm-router/internal/db"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// jobCall records one stub backend execution.
type jobCall struct {
	typeKey    string
	jobName    string
	reason     string
	providerID string
}

// stubJobBackend is a scripted PluginJobs: schedules per type key plus a
// call log. When release is non-nil, RunJob blocks until the test closes it
// or the context ends. No provider, luaplugin, or modelinfo source is
// involved.
type stubJobBackend struct {
	mutex    sync.Mutex
	jobs     map[string]map[string]luaplugin.JobSpec
	calls    []jobCall
	runError error
	release  chan struct{}
}

func (s *stubJobBackend) Jobs(typeKey string) map[string]luaplugin.JobSpec {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.jobs[typeKey]
}

func (s *stubJobBackend) RunJob(ctx context.Context, typeKey, jobName, reason, providerID string, _ map[string]any) error {
	s.mutex.Lock()
	s.calls = append(s.calls, jobCall{typeKey: typeKey, jobName: jobName, reason: reason, providerID: providerID})
	release := s.release
	runError := s.runError
	s.mutex.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	return runError
}

func (s *stubJobBackend) callCount() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return len(s.calls)
}

func (s *stubJobBackend) reasons() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	reasons := make([]string, 0, len(s.calls))
	for _, call := range s.calls {
		reasons = append(reasons, call.reason)
	}
	return reasons
}

// stubProviderSource is an in-memory ProviderInventory.
type stubProviderSource struct {
	mutex     sync.Mutex
	providers map[string]*models.ProviderInstance
	listError error
	getError  error
}

func (s *stubProviderSource) Get(_ context.Context, identifier string) (*models.ProviderInstance, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.getError != nil {
		return nil, s.getError
	}
	instance, ok := s.providers[identifier]
	if !ok {
		return nil, apierrors.NewNotFoundError("provider", identifier)
	}
	return instance, nil
}

func (s *stubProviderSource) List(_ context.Context) ([]*models.ProviderInstance, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.listError != nil {
		return nil, s.listError
	}
	items := make([]*models.ProviderInstance, 0, len(s.providers))
	for _, instance := range s.providers {
		items = append(items, instance)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// stubModelInfo is a scripted modelInfoRefresher: canned views per provider
// plus a call log. No modelinfo source is involved.
type stubModelInfo struct {
	mutex    sync.Mutex
	views    map[string][]modelinfo.ModelView
	calls    []string
	runError error
}

func (s *stubModelInfo) MergedView(_ context.Context, providerID string) ([]modelinfo.ModelView, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.calls = append(s.calls, providerID)
	if s.runError != nil {
		return nil, s.runError
	}
	return s.views[providerID], nil
}

func (s *stubModelInfo) calledWith() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append([]string(nil), s.calls...)
}

// gormMaintenanceStack builds a GormService with stub jobs and providers on
// a fresh bbolt database. The logger stays nil to pin the nil-safe logging.
func gormMaintenanceStack(t *testing.T, backend *stubJobBackend, providers *stubProviderSource) *GormService {
	t.Helper()
	boltDatabase := testutil.SetupTestDB(t)
	return NewGormService(boltDatabase, backend, providers, nil)
}

// waitForGormCondition polls until the condition holds or the test times out.
func waitForGormCondition(t *testing.T, what string, condition func() bool) {
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

// seedAuthEntry writes one auth flow row with the given creation time. The
// cleanup path reads only CreatedAt, so the minimal shape suffices.
func seedAuthEntry(t *testing.T, boltDatabase *db.DB, key string, createdAt time.Time) {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"created_at": createdAt.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatalf("marshal auth entry: %v", err)
	}
	if err := boltDatabase.Update(func(transaction *bolt.Tx) error {
		return transaction.Bucket(db.BucketAuth).Put([]byte(key), payload)
	}); err != nil {
		t.Fatalf("seed auth entry %q: %v", key, err)
	}
}

// authEntryPresent reports whether one auth flow row exists.
func authEntryPresent(t *testing.T, boltDatabase *db.DB, key string) bool {
	t.Helper()
	var present bool
	if err := boltDatabase.View(func(transaction *bolt.Tx) error {
		present = transaction.Bucket(db.BucketAuth).Get([]byte(key)) != nil
		return nil
	}); err != nil {
		t.Fatalf("read auth entry %q: %v", key, err)
	}
	return present
}

func TestGormRunDueJobsRunsEnabledSkipsDisabled(t *testing.T) {
	backend := &stubJobBackend{jobs: map[string]map[string]luaplugin.JobSpec{
		"test-type": {"nightly": {IntervalSeconds: 0}},
	}}
	providers := &stubProviderSource{providers: map[string]*models.ProviderInstance{
		"enabled":  {ID: "enabled", TypeKey: "test-type"},
		"disabled": {ID: "disabled", TypeKey: "test-type", Disabled: true},
	}}
	service := gormMaintenanceStack(t, backend, providers)
	service.runDueJobs(context.Background(), false)
	if got := backend.callCount(); got != 1 {
		t.Fatalf("job calls = %d, want 1 for the enabled provider only", got)
	}
}

func TestGormRunDueJobsIntervalGatesTickStartupBypasses(t *testing.T) {
	backend := &stubJobBackend{jobs: map[string]map[string]luaplugin.JobSpec{
		"test-type": {"hourly": {IntervalSeconds: 3600, RunOnStartup: true}},
	}}
	providers := &stubProviderSource{providers: map[string]*models.ProviderInstance{
		"provider": {ID: "provider", TypeKey: "test-type"},
	}}
	service := gormMaintenanceStack(t, backend, providers)
	ctx := context.Background()
	service.runDueJobs(ctx, false)
	service.runDueJobs(ctx, false)
	if got := backend.callCount(); got != 1 {
		t.Fatalf("tick calls = %d, want 1 while the interval holds", got)
	}
	service.runDueJobs(ctx, true)
	if got := backend.callCount(); got != 2 {
		t.Fatalf("calls after startup = %d, want 2 with the startup bypass", got)
	}
}

func TestGormRunDueJobsStartupSkipsNonStartup(t *testing.T) {
	backend := &stubJobBackend{jobs: map[string]map[string]luaplugin.JobSpec{
		"test-type": {"periodic": {IntervalSeconds: 0}},
	}}
	providers := &stubProviderSource{providers: map[string]*models.ProviderInstance{
		"provider": {ID: "provider", TypeKey: "test-type"},
	}}
	service := gormMaintenanceStack(t, backend, providers)
	ctx := context.Background()
	service.runDueJobs(ctx, true)
	if got := backend.callCount(); got != 0 {
		t.Fatalf("startup calls = %d, want 0 without the startup flag", got)
	}
	service.runDueJobs(ctx, false)
	if got := backend.callCount(); got != 1 {
		t.Fatalf("tick calls = %d, want 1", got)
	}
}

func TestGormRunDueJobsNilDependenciesSkip(t *testing.T) {
	boltDatabase := testutil.SetupTestDB(t)
	ctx := context.Background()
	NewGormService(boltDatabase, nil, &stubProviderSource{}, nil).runDueJobs(ctx, false)
	NewGormService(boltDatabase, &stubJobBackend{}, nil, nil).runDueJobs(ctx, false)
}

func TestGormRunDueJobsListErrorStops(t *testing.T) {
	backend := &stubJobBackend{jobs: map[string]map[string]luaplugin.JobSpec{
		"test-type": {"nightly": {}},
	}}
	providers := &stubProviderSource{listError: errors.New("list failed")}
	service := gormMaintenanceStack(t, backend, providers)
	service.runDueJobs(context.Background(), false)
	if got := backend.callCount(); got != 0 {
		t.Fatalf("job calls = %d, want 0 when the provider list fails", got)
	}
}

func TestGormRunDueJobsOverlapSkips(t *testing.T) {
	backend := &stubJobBackend{
		jobs:    map[string]map[string]luaplugin.JobSpec{"test-type": {"nightly": {}}},
		release: make(chan struct{}),
	}
	providers := &stubProviderSource{providers: map[string]*models.ProviderInstance{
		"provider": {ID: "provider", TypeKey: "test-type"},
	}}
	service := gormMaintenanceStack(t, backend, providers)
	ctx := context.Background()
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		service.runDueJobs(ctx, false)
	}()
	waitForGormCondition(t, "first job start", func() bool { return backend.callCount() == 1 })
	service.runDueJobs(ctx, false)
	if got := backend.callCount(); got != 1 {
		t.Fatalf("overlapping calls = %d, want 1 while the first job holds its slot", got)
	}
	close(backend.release)
	waitForGormCondition(t, "first job end", func() bool {
		select {
		case <-firstDone:
			return true
		default:
			return false
		}
	})
}

func TestGormRunJobUnknownFails(t *testing.T) {
	backend := &stubJobBackend{jobs: map[string]map[string]luaplugin.JobSpec{
		"test-type": {"nightly": {}},
	}}
	providers := &stubProviderSource{providers: map[string]*models.ProviderInstance{
		"provider": {ID: "provider", TypeKey: "test-type"},
	}}
	service := gormMaintenanceStack(t, backend, providers)
	err := service.RunJob(context.Background(), "provider", "missing-job")
	if err == nil {
		t.Fatal("unknown job succeeded, want not-found error")
	}
	if !strings.Contains(err.Error(), `"missing-job" not found for provider "provider"`) {
		t.Fatalf("error = %q, want the unknown-job message", err.Error())
	}
}

func TestGormRunJobBusyRefuses(t *testing.T) {
	backend := &stubJobBackend{
		jobs:    map[string]map[string]luaplugin.JobSpec{"test-type": {"nightly": {}}},
		release: make(chan struct{}),
	}
	providers := &stubProviderSource{providers: map[string]*models.ProviderInstance{
		"provider": {ID: "provider", TypeKey: "test-type"},
	}}
	service := gormMaintenanceStack(t, backend, providers)
	ctx := context.Background()
	firstResult := make(chan error, 1)
	go func() { firstResult <- service.RunJob(ctx, "provider", "nightly") }()
	waitForGormCondition(t, "first job start", func() bool { return backend.callCount() == 1 })
	err := service.RunJob(ctx, "provider", "nightly")
	var busy *jobBusyError
	if !errors.As(err, &busy) {
		t.Fatalf("second trigger error = %v, want the busy sentinel", err)
	}
	close(backend.release)
	if err := <-firstResult; err != nil {
		t.Fatalf("first trigger error = %v, want nil", err)
	}
}

func TestGormRunJobProviderMissFails(t *testing.T) {
	backend := &stubJobBackend{}
	providers := &stubProviderSource{providers: map[string]*models.ProviderInstance{}}
	service := gormMaintenanceStack(t, backend, providers)
	err := service.RunJob(context.Background(), "missing", "nightly")
	if !errors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("error = %v, want the not-found sentinel", err)
	}
}

func TestGormRunJobBackendErrorStaysSilent(t *testing.T) {
	backend := &stubJobBackend{
		jobs:     map[string]map[string]luaplugin.JobSpec{"test-type": {"nightly": {}}},
		runError: errors.New("upstream failed"),
	}
	providers := &stubProviderSource{providers: map[string]*models.ProviderInstance{
		"provider": {ID: "provider", TypeKey: "test-type"},
	}}
	service := gormMaintenanceStack(t, backend, providers)
	if err := service.RunJob(context.Background(), "provider", "nightly"); err != nil {
		t.Fatalf("trigger error = %v, want nil: job failures log, never fail the caller", err)
	}
	if got := backend.callCount(); got != 1 {
		t.Fatalf("job calls = %d, want 1", got)
	}
	if reasons := backend.reasons(); len(reasons) != 1 || reasons[0] != "manual" {
		t.Fatalf("reasons = %v, want one manual trigger", reasons)
	}
}

func TestGormSyncProviderModelsAutoSyncGate(t *testing.T) {
	backend := &stubJobBackend{}
	providers := &stubProviderSource{providers: map[string]*models.ProviderInstance{
		"auto":          {ID: "auto", TypeKey: "test-type", Config: map[string]any{"models_auto_sync": true}},
		"manual":        {ID: "manual", TypeKey: "test-type"},
		"disabled-auto": {ID: "disabled-auto", TypeKey: "test-type", Disabled: true, Config: map[string]any{"models_auto_sync": true}},
		"flag-off":      {ID: "flag-off", TypeKey: "test-type", Config: map[string]any{"models_auto_sync": false}},
		"non-boolean":   {ID: "non-boolean", TypeKey: "test-type", Config: map[string]any{"models_auto_sync": "yes"}},
	}}
	refresher := &stubModelInfo{views: map[string][]modelinfo.ModelView{}}
	service := gormMaintenanceStack(t, backend, providers)
	service.SetModelInfoService(refresher)
	service.syncProviderModels(context.Background())
	calledWith := refresher.calledWith()
	if len(calledWith) != 1 || calledWith[0] != "auto" {
		t.Fatalf("synced providers = %v, want only the enabled opted-in provider", calledWith)
	}
}

func TestGormSyncProviderModelsFailureStaysSilent(t *testing.T) {
	backend := &stubJobBackend{}
	providers := &stubProviderSource{providers: map[string]*models.ProviderInstance{
		"auto": {ID: "auto", TypeKey: "test-type", Config: map[string]any{"models_auto_sync": true}},
	}}
	refresher := &stubModelInfo{runError: errors.New("upstream failed")}
	service := gormMaintenanceStack(t, backend, providers)
	service.SetModelInfoService(refresher)
	service.syncProviderModels(context.Background())
	if calledWith := refresher.calledWith(); len(calledWith) != 1 {
		t.Fatalf("synced providers = %v, want one attempted sync", calledWith)
	}
}

func TestGormSyncProviderModelsNilRefresherSkips(t *testing.T) {
	backend := &stubJobBackend{}
	providers := &stubProviderSource{providers: map[string]*models.ProviderInstance{
		"auto": {ID: "auto", TypeKey: "test-type", Config: map[string]any{"models_auto_sync": true}},
	}}
	service := gormMaintenanceStack(t, backend, providers)
	service.syncProviderModels(context.Background())
}

func TestGormCleanupAuthFlowsRemovesExpired(t *testing.T) {
	boltDatabase := testutil.SetupTestDB(t)
	backend := &stubJobBackend{}
	providers := &stubProviderSource{}
	service := NewGormService(boltDatabase, backend, providers, nil)
	now := time.Now().UTC()
	seedAuthEntry(t, boltDatabase, "expired", now.Add(-20*time.Minute))
	seedAuthEntry(t, boltDatabase, "fresh", now)
	if err := boltDatabase.Update(func(transaction *bolt.Tx) error {
		return transaction.Bucket(db.BucketAuth).Put([]byte("malformed"), []byte("not json"))
	}); err != nil {
		t.Fatalf("seed malformed entry: %v", err)
	}
	service.cleanupAuthFlows()
	if authEntryPresent(t, boltDatabase, "expired") {
		t.Fatal("expired auth flow still present, want it removed")
	}
	if !authEntryPresent(t, boltDatabase, "fresh") {
		t.Fatal("fresh auth flow missing, want it kept")
	}
	if !authEntryPresent(t, boltDatabase, "malformed") {
		t.Fatal("malformed auth flow missing, want it skipped, never deleted")
	}
}

func TestGormCleanupAuthFlowsNilDatabaseSkips(t *testing.T) {
	service := NewGormService(nil, &stubJobBackend{}, &stubProviderSource{}, nil)
	service.cleanupAuthFlows()
}

func TestGormStartupRefreshRunsStartupJobsAndCleansAuth(t *testing.T) {
	boltDatabase := testutil.SetupTestDB(t)
	backend := &stubJobBackend{jobs: map[string]map[string]luaplugin.JobSpec{
		"test-type": {
			"boot":     {IntervalSeconds: 3600, RunOnStartup: true},
			"periodic": {IntervalSeconds: 0},
		},
	}}
	providers := &stubProviderSource{providers: map[string]*models.ProviderInstance{
		"provider": {ID: "provider", TypeKey: "test-type"},
	}}
	service := NewGormService(boltDatabase, backend, providers, nil)
	seedAuthEntry(t, boltDatabase, "expired", time.Now().UTC().Add(-20*time.Minute))
	service.RunStartupRefresh(context.Background())
	if got := backend.callCount(); got != 1 {
		t.Fatalf("startup calls = %d, want only the startup-flagged job", got)
	}
	if reasons := backend.reasons(); len(reasons) != 1 || reasons[0] != "startup" {
		t.Fatalf("reasons = %v, want one startup reason", reasons)
	}
	if authEntryPresent(t, boltDatabase, "expired") {
		t.Fatal("expired auth flow still present after startup refresh, want it removed")
	}
}

func TestGormStartTicksJobs(t *testing.T) {
	boltDatabase := testutil.SetupTestDB(t)
	backend := &stubJobBackend{jobs: map[string]map[string]luaplugin.JobSpec{
		"test-type": {"nightly": {}},
	}}
	providers := &stubProviderSource{providers: map[string]*models.ProviderInstance{
		"provider": {ID: "provider", TypeKey: "test-type"},
	}}
	service := NewGormService(boltDatabase, backend, providers, nil).WithInterval(5 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx)
	waitForGormCondition(t, "first tick", func() bool { return backend.callCount() >= 1 })
	cancel()
}

// TestGormAgainstRealProviderGormService pins the GORM compatibility the
// narrow interface promises: a real provider GORM service on SQLite plugs
// into the scheduler with no adapter. The job backend stays stubbed; no
// luaplugin source is involved.
func TestGormAgainstRealProviderGormService(t *testing.T) {
	sqliteDatabase := testutil.SetupTestSQLite(t)
	providerService := provider.NewGorm(sqliteDatabase)
	ctx := context.Background()
	created, err := providerService.Create(ctx, provider.CreateOptions{Name: "Test", TypeKey: "test-type"})
	if err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	backend := &stubJobBackend{jobs: map[string]map[string]luaplugin.JobSpec{
		"test-type": {"nightly": {}},
	}}
	boltDatabase := testutil.SetupTestDB(t)
	service := NewGormService(boltDatabase, backend, providerService, nil)
	service.runDueJobs(ctx, false)
	if got := backend.callCount(); got != 1 {
		t.Fatalf("job calls = %d, want 1 through the real provider rows", got)
	}
	backend.mutex.Lock()
	providerID := backend.calls[0].providerID
	backend.mutex.Unlock()
	if providerID != created.ID {
		t.Fatalf("job provider = %q, want %q", providerID, created.ID)
	}
}
