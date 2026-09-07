package modelinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

type modelInfoTestAdapter struct {
	typeKey    string
	callCount  int
	infos      []models.ModelInfo
	panicOnNil bool
}

func (a *modelInfoTestAdapter) TypeKey() string { return a.typeKey }
func (a *modelInfoTestAdapter) ValidateCredentials(data map[string]any) error {
	if s, _ := data["api_key"].(string); s == "" {
		return fmt.Errorf("api_key required")
	}
	return nil
}
func (a *modelInfoTestAdapter) Complete(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest, _ map[string]any) (*models.ChatCompletionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (a *modelInfoTestAdapter) CompleteStream(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest, w io.Writer, _ map[string]any) error {
	return fmt.Errorf("not implemented")
}
func (a *modelInfoTestAdapter) NeedsRefresh(cred *models.Credential) bool { return false }
func (a *modelInfoTestAdapter) RefreshCredential(ctx context.Context, cred *models.Credential) (map[string]any, error) {
	return nil, fmt.Errorf("no refresh needed for this credential type")
}
func (a *modelInfoTestAdapter) GetModelInfos(ctx context.Context, cred *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	a.callCount++
	if a.panicOnNil && cred == nil {
		panic("nil credential")
	}
	return append([]models.ModelInfo(nil), a.infos...), nil
}

func setupModelInfoService(t *testing.T) (*Service, *credential.Service, *provider.Service, *db.DB) {
	t.Helper()

	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	for _, a := range []*modelInfoTestAdapter{
		{typeKey: "modelinfo-test", infos: []models.ModelInfo{
			{Name: "live-model", DisplayName: "Live Model", ContextWindow: 4096},
		}},
		{typeKey: "modelinfo-nocred", infos: []models.ModelInfo{
			{Name: "nocred-model", DisplayName: "No Credential Model", ContextWindow: 2048},
		}},
		{typeKey: "modelinfo-panic-nocred", panicOnNil: true},
	} {
		providerSvc.RegisterGoAdapter(a)
		if _, err := providerSvc.Create(provider.CreateOptions{Name: a.typeKey, TypeKey: a.typeKey}); err != nil {
			t.Fatalf("create provider %s: %v", a.typeKey, err)
		}
		if args := a; args.callCount != 0 {
			t.Fatalf("adapter %s should start with zero calls", a.typeKey)
		}
	}
	credSvc := credential.New(database, providerSvc)
	svc := New(database, providerSvc, credSvc, time.Hour)
	return svc, credSvc, providerSvc, database
}

func addModelInfoCredential(t *testing.T, credSvc *credential.Service, providerID string) {
	t.Helper()
	if _, err := credSvc.Add(credential.AddOptions{
		ProviderID: providerID,
		Label:      "test",
		Data:       map[string]any{"api_key": "test-key"},
	}); err != nil {
		t.Fatalf("add credential failed: %v", err)
	}
}

func adapterFor(t *testing.T, providerSvc *provider.Service, typeKey string) *modelInfoTestAdapter {
	t.Helper()
	a, ok := providerSvc.GoAdapterFor(typeKey)
	if !ok {
		t.Fatalf("adapter %s not registered", typeKey)
	}
	ma, ok := a.(*modelInfoTestAdapter)
	if !ok {
		t.Fatalf("adapter %s has wrong type", typeKey)
	}
	return ma
}

func TestModelInfoService_CacheHitUsesMemoryOnly(t *testing.T) {
	svc, credSvc, providerSvc, _ := setupModelInfoService(t)
	addModelInfoCredential(t, credSvc, "modelinfo-test")

	first, err := svc.GetModelInfos(context.Background(), "modelinfo-test")
	if err != nil {
		t.Fatalf("first fetch failed: %v", err)
	}
	second, err := svc.GetModelInfos(context.Background(), "modelinfo-test")
	if err != nil {
		t.Fatalf("second fetch failed: %v", err)
	}

	if got := adapterFor(t, providerSvc, "modelinfo-test").callCount; got != 1 {
		t.Fatalf("expected adapter to be called once, got %d", got)
	}
	if len(first) != 1 || first[0].Name != "live-model" {
		t.Fatalf("unexpected first result: %+v", first)
	}
	if len(second) != 1 || second[0].Name != "live-model" {
		t.Fatalf("unexpected second result: %+v", second)
	}
}

func TestModelInfoService_InvalidateProviderClearsMemoryCache(t *testing.T) {
	svc, credSvc, providerSvc, _ := setupModelInfoService(t)
	addModelInfoCredential(t, credSvc, "modelinfo-test")

	if _, err := svc.GetModelInfos(context.Background(), "modelinfo-test"); err != nil {
		t.Fatalf("initial fetch failed: %v", err)
	}
	if err := svc.InvalidateProvider("modelinfo-test"); err != nil {
		t.Fatalf("invalidate provider failed: %v", err)
	}
	if _, err := svc.GetModelInfos(context.Background(), "modelinfo-test"); err != nil {
		t.Fatalf("refetch failed: %v", err)
	}

	if got := adapterFor(t, providerSvc, "modelinfo-test").callCount; got != 2 {
		t.Fatalf("expected adapter to be called twice after provider invalidation, got %d", got)
	}
}

func TestModelInfoService_InvalidateAllClearsMemoryCache(t *testing.T) {
	svc, credSvc, providerSvc, _ := setupModelInfoService(t)
	addModelInfoCredential(t, credSvc, "modelinfo-test")

	if _, err := svc.GetModelInfos(context.Background(), "modelinfo-test"); err != nil {
		t.Fatalf("initial fetch failed: %v", err)
	}
	if err := svc.InvalidateAll(); err != nil {
		t.Fatalf("invalidate all failed: %v", err)
	}
	if _, err := svc.GetModelInfos(context.Background(), "modelinfo-test"); err != nil {
		t.Fatalf("refetch failed: %v", err)
	}

	if got := adapterFor(t, providerSvc, "modelinfo-test").callCount; got != 2 {
		t.Fatalf("expected adapter to be called twice after cache reset, got %d", got)
	}
}

func TestModelInfoService_IgnoresLegacyDatabaseCache(t *testing.T) {
	svc, credSvc, providerSvc, database := setupModelInfoService(t)
	addModelInfoCredential(t, credSvc, "modelinfo-test")

	stale := struct {
		Models    []models.ModelInfo `json:"models"`
		CachedAt  time.Time          `json:"cached_at"`
		ExpiresAt time.Time          `json:"expires_at"`
	}{
		Models:    []models.ModelInfo{{Name: "stale-model", DisplayName: "Stale Model"}},
		CachedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	data, err := json.Marshal(stale)
	if err != nil {
		t.Fatalf("marshal stale model info: %v", err)
	}
	if err := database.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(db.BucketModelInfo)
		if err != nil {
			return err
		}
		return b.Put([]byte("modelinfo-test"), data)
	}); err != nil {
		t.Fatalf("seed legacy model_info bucket: %v", err)
	}

	got, err := svc.GetModelInfos(context.Background(), "modelinfo-test")
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}

	if count := adapterFor(t, providerSvc, "modelinfo-test").callCount; count != 1 {
		t.Fatalf("expected live adapter fetch, got call count %d", count)
	}
	if len(got) != 1 || got[0].Name != "live-model" {
		t.Fatalf("expected live model info, got %+v", got)
	}
}

func TestModelInfoService_SupportsCredentialFreeDiscovery(t *testing.T) {
	svc, _, providerSvc, _ := setupModelInfoService(t)

	got, err := svc.GetModelInfos(context.Background(), "modelinfo-nocred")
	if err != nil {
		t.Fatalf("credential-free fetch failed: %v", err)
	}

	if count := adapterFor(t, providerSvc, "modelinfo-nocred").callCount; count != 1 {
		t.Fatalf("expected credential-free adapter to be called once, got %d", count)
	}
	if len(got) != 1 || got[0].Name != "nocred-model" {
		t.Fatalf("unexpected credential-free models: %+v", got)
	}
}

func TestModelInfoService_NoCredentialsDoesNotCrashOnAdapterPanic(t *testing.T) {
	svc, _, providerSvc, _ := setupModelInfoService(t)

	got, err := svc.GetModelInfos(context.Background(), "modelinfo-panic-nocred")
	if err == nil {
		t.Fatalf("expected missing-credentials error")
	}

	if len(got) != 0 {
		t.Fatalf("expected no models, got %+v", got)
	}
	if count := adapterFor(t, providerSvc, "modelinfo-panic-nocred").callCount; count != 1 {
		t.Fatalf("expected panic adapter to be called once, got %d", count)
	}
	if !strings.Contains(err.Error(), "no credentials available for provider modelinfo-panic-nocred") {
		t.Fatalf("unexpected error: %v", err)
	}
}
