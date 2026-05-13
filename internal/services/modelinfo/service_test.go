package modelinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	sdk "github.com/TheSlopMachine/llm-router-sdk"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

type modelInfoTestAdapter struct {
	typeKey   string
	callCount int
	infos     []models.ModelInfo
}

func (a *modelInfoTestAdapter) TypeKey() string { return a.typeKey }
func (a *modelInfoTestAdapter) AuthType() models.AuthType { return models.AuthTypeAPIKey }
func (a *modelInfoTestAdapter) ValidateCredentials(data map[string]string) error {
	if data["api_key"] == "" {
		return fmt.Errorf("api_key required")
	}
	return nil
}
func (a *modelInfoTestAdapter) Complete(ctx context.Context, cred *sdk.Credential, req *sdk.ChatCompletionRequest) (*sdk.ChatCompletionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (a *modelInfoTestAdapter) CompleteStream(ctx context.Context, cred *sdk.Credential, req *sdk.ChatCompletionRequest, w io.Writer) error {
	return fmt.Errorf("not implemented")
}
func (a *modelInfoTestAdapter) NeedsRefresh(cred *sdk.Credential) bool { return false }
func (a *modelInfoTestAdapter) RefreshCredential(ctx context.Context, cred *sdk.Credential) (*sdk.Credential, error) {
	return nil, provider.ErrNoRefreshNeeded
}
func (a *modelInfoTestAdapter) GetModelInfos(ctx context.Context, cred *sdk.Credential, providerQualifier string) ([]sdk.ModelInfo, error) {
	a.callCount++
	return append([]models.ModelInfo(nil), a.infos...), nil
}
func (a *modelInfoTestAdapter) GetAuthFlow() provider.AuthFlowHandler { return nil }
func (a *modelInfoTestAdapter) GetDefaultProviders() []provider.ProviderInfo {
	return []provider.ProviderInfo{{Name: a.typeKey, Qualifier: "", BaseURL: "", IconURL: ""}}
}

var (
	cachedAdapter = &modelInfoTestAdapter{
		typeKey: "modelinfo-test",
		infos: []models.ModelInfo{
			{Name: "live-model", DisplayName: "Live Model", ContextWindow: 4096},
		},
	}
	noCredAdapter = &modelInfoTestAdapter{
		typeKey: "modelinfo-nocred",
		infos: []models.ModelInfo{
			{Name: "nocred-model", DisplayName: "No Credential Model", ContextWindow: 2048},
		},
	}
)

func ensureModelInfoAdapters(t *testing.T) {
	t.Helper()
	if _, err := provider.Lookup(cachedAdapter.typeKey); err != nil {
		provider.Register(cachedAdapter)
	}
	if _, err := provider.Lookup(noCredAdapter.typeKey); err != nil {
		provider.Register(noCredAdapter)
	}
	cachedAdapter.callCount = 0
	noCredAdapter.callCount = 0
}

func setupModelInfoService(t *testing.T) (*Service, *credential.Service, *db.DB) {
	t.Helper()
	ensureModelInfoAdapters(t)

	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	credSvc := credential.New(database, providerSvc)
	svc := New(database, providerSvc, credSvc, time.Hour)
	return svc, credSvc, database
}

func addModelInfoCredential(t *testing.T, credSvc *credential.Service, providerID string) {
	t.Helper()
	if _, err := credSvc.Add(credential.AddOptions{
		ProviderID: providerID,
		Label:      "test",
		Data:       map[string]string{"api_key": "test-key"},
	}); err != nil {
		t.Fatalf("add credential failed: %v", err)
	}
}

func TestModelInfoService_CacheHitUsesMemoryOnly(t *testing.T) {
	svc, credSvc, _ := setupModelInfoService(t)
	addModelInfoCredential(t, credSvc, cachedAdapter.typeKey)

	first, err := svc.GetModelInfos(context.Background(), cachedAdapter.typeKey)
	if err != nil {
		t.Fatalf("first fetch failed: %v", err)
	}
	second, err := svc.GetModelInfos(context.Background(), cachedAdapter.typeKey)
	if err != nil {
		t.Fatalf("second fetch failed: %v", err)
	}

	if cachedAdapter.callCount != 1 {
		t.Fatalf("expected adapter to be called once, got %d", cachedAdapter.callCount)
	}
	if len(first) != 1 || first[0].Name != "live-model" {
		t.Fatalf("unexpected first result: %+v", first)
	}
	if len(second) != 1 || second[0].Name != "live-model" {
		t.Fatalf("unexpected second result: %+v", second)
	}
}

func TestModelInfoService_InvalidateProviderClearsMemoryCache(t *testing.T) {
	svc, credSvc, _ := setupModelInfoService(t)
	addModelInfoCredential(t, credSvc, cachedAdapter.typeKey)

	if _, err := svc.GetModelInfos(context.Background(), cachedAdapter.typeKey); err != nil {
		t.Fatalf("initial fetch failed: %v", err)
	}
	if err := svc.InvalidateProvider(cachedAdapter.typeKey); err != nil {
		t.Fatalf("invalidate provider failed: %v", err)
	}
	if _, err := svc.GetModelInfos(context.Background(), cachedAdapter.typeKey); err != nil {
		t.Fatalf("refetch failed: %v", err)
	}

	if cachedAdapter.callCount != 2 {
		t.Fatalf("expected adapter to be called twice after provider invalidation, got %d", cachedAdapter.callCount)
	}
}

func TestModelInfoService_InvalidateAllClearsMemoryCache(t *testing.T) {
	svc, credSvc, _ := setupModelInfoService(t)
	addModelInfoCredential(t, credSvc, cachedAdapter.typeKey)

	if _, err := svc.GetModelInfos(context.Background(), cachedAdapter.typeKey); err != nil {
		t.Fatalf("initial fetch failed: %v", err)
	}
	if err := svc.InvalidateAll(); err != nil {
		t.Fatalf("invalidate all failed: %v", err)
	}
	if _, err := svc.GetModelInfos(context.Background(), cachedAdapter.typeKey); err != nil {
		t.Fatalf("refetch failed: %v", err)
	}

	if cachedAdapter.callCount != 2 {
		t.Fatalf("expected adapter to be called twice after cache reset, got %d", cachedAdapter.callCount)
	}
}

func TestModelInfoService_IgnoresLegacyDatabaseCache(t *testing.T) {
	svc, credSvc, database := setupModelInfoService(t)
	addModelInfoCredential(t, credSvc, cachedAdapter.typeKey)

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
		return b.Put([]byte(cachedAdapter.typeKey), data)
	}); err != nil {
		t.Fatalf("seed legacy model_info bucket: %v", err)
	}

	got, err := svc.GetModelInfos(context.Background(), cachedAdapter.typeKey)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}

	if cachedAdapter.callCount != 1 {
		t.Fatalf("expected live adapter fetch, got call count %d", cachedAdapter.callCount)
	}
	if len(got) != 1 || got[0].Name != "live-model" {
		t.Fatalf("expected live model info, got %+v", got)
	}
}

func TestModelInfoService_SupportsCredentialFreeDiscovery(t *testing.T) {
	svc, _, _ := setupModelInfoService(t)

	got, err := svc.GetModelInfos(context.Background(), noCredAdapter.typeKey)
	if err != nil {
		t.Fatalf("credential-free fetch failed: %v", err)
	}

	if noCredAdapter.callCount != 1 {
		t.Fatalf("expected credential-free adapter to be called once, got %d", noCredAdapter.callCount)
	}
	if len(got) != 1 || got[0].Name != "nocred-model" {
		t.Fatalf("unexpected credential-free models: %+v", got)
	}
}
