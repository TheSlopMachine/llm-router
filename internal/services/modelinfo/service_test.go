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
	modelList  []models.ModelInfo
	panicOnNil bool
	failError  error
}

func (adapter *modelInfoTestAdapter) TypeKey() string { return adapter.typeKey }
func (adapter *modelInfoTestAdapter) ValidateCredentials(data map[string]any) error {
	if s, _ := data["api_key"].(string); s == "" {
		return fmt.Errorf("api_key required")
	}
	return nil
}
func (adapter *modelInfoTestAdapter) Complete(ctx context.Context, credentials []*models.Credential, request *models.ChatCompletionRequest, _ map[string]any) (*models.ChatCompletionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (adapter *modelInfoTestAdapter) CompleteStream(ctx context.Context, credentials []*models.Credential, request *models.ChatCompletionRequest, writer io.Writer, _ map[string]any) error {
	return fmt.Errorf("not implemented")
}
func (adapter *modelInfoTestAdapter) NeedsRefresh(credential *models.Credential) bool { return false }
func (adapter *modelInfoTestAdapter) RefreshCredential(ctx context.Context, credential *models.Credential) (map[string]any, error) {
	return nil, fmt.Errorf("no refresh needed for this credential type")
}
func (adapter *modelInfoTestAdapter) GetModelInfos(ctx context.Context, credential *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	adapter.callCount++
	if adapter.failError != nil {
		return nil, adapter.failError
	}
	if adapter.panicOnNil && credential == nil {
		panic("nil credential")
	}
	return append([]models.ModelInfo(nil), adapter.modelList...), nil
}

func setupModelInfoService(t *testing.T) (*Service, *credential.Service, *provider.Service, *db.DB) {
	t.Helper()

	database := testutil.SetupTestDB(t)
	providerService := provider.NewService(database)
	for _, adapter := range []*modelInfoTestAdapter{
		{typeKey: "modelinfo-test", modelList: []models.ModelInfo{
			{Name: "live-model", DisplayName: "Live Model", ContextWindow: 4096},
		}},
		{typeKey: "modelinfo-nocred", modelList: []models.ModelInfo{
			{Name: "nocred-model", DisplayName: "No Credential Model", ContextWindow: 2048},
		}},
		{typeKey: "modelinfo-panic-nocred", panicOnNil: true},
	} {
		providerService.RegisterGoAdapter(adapter)
		if _, err := providerService.Create(provider.CreateOptions{Name: adapter.typeKey, TypeKey: adapter.typeKey}); err != nil {
			t.Fatalf("create provider %s: %v", adapter.typeKey, err)
		}
		if adapter.callCount != 0 {
			t.Fatalf("adapter %s should start with zero calls", adapter.typeKey)
		}
	}
	credentialService := credential.New(database, providerService)
	service := New(database, providerService, credentialService, time.Hour)
	return service, credentialService, providerService, database
}

func addModelInfoCredential(t *testing.T, credentialService *credential.Service, providerID string) {
	t.Helper()
	if _, err := credentialService.Add(credential.AddOptions{
		ProviderID: providerID,
		Label:      "test",
		Data:       map[string]any{"api_key": "test-key"},
	}); err != nil {
		t.Fatalf("add credential failed: %v", err)
	}
}

func adapterFor(t *testing.T, providerService *provider.Service, typeKey string) *modelInfoTestAdapter {
	t.Helper()
	adapter, ok := providerService.GoAdapterFor(typeKey)
	if !ok {
		t.Fatalf("adapter %s not registered", typeKey)
	}
	modelAdapter, ok := adapter.(*modelInfoTestAdapter)
	if !ok {
		t.Fatalf("adapter %s has wrong type", typeKey)
	}
	return modelAdapter
}

func TestModelInfoService_CacheHitUsesMemoryOnly(t *testing.T) {
	service, credentialService, providerService, _ := setupModelInfoService(t)
	addModelInfoCredential(t, credentialService, "modelinfo-test")

	first, err := service.GetModelInfos(context.Background(), "modelinfo-test")
	if err != nil {
		t.Fatalf("first fetch failed: %v", err)
	}
	second, err := service.GetModelInfos(context.Background(), "modelinfo-test")
	if err != nil {
		t.Fatalf("second fetch failed: %v", err)
	}

	if got := adapterFor(t, providerService, "modelinfo-test").callCount; got != 1 {
		t.Fatalf("expected adapter to be called once, got %d", got)
	}
	if len(first) != 1 || first[0].Name != "live-model" {
		t.Fatalf("unexpected first result: %+v", first)
	}
	if len(second) != 1 || second[0].Name != "live-model" {
		t.Fatalf("unexpected second result: %+v", second)
	}
}

func TestModelInfoService_RefreshKeepsStaleOnFailure(t *testing.T) {
	service, credentialService, providerService, _ := setupModelInfoService(t)
	addModelInfoCredential(t, credentialService, "modelinfo-test")

	if _, err := service.GetModelInfos(context.Background(), "modelinfo-test"); err != nil {
		t.Fatalf("warm cache: %v", err)
	}
	adapterFor(t, providerService, "modelinfo-test").failError = fmt.Errorf("upstream down")

	if _, err := service.Refresh(context.Background(), "modelinfo-test"); err == nil {
		t.Fatal("expected refresh error, got nil")
	}
	got := service.PeekModelInfos("modelinfo-test")
	if len(got) != 1 || got[0].Name != "live-model" {
		t.Fatalf("stale list must survive failed refresh, got %+v", got)
	}
}

func TestModelInfoService_StoreMergesNewRefreshesExisting(t *testing.T) {
	service, credentialService, providerService, _ := setupModelInfoService(t)
	addModelInfoCredential(t, credentialService, "modelinfo-test")
	adapter := adapterFor(t, providerService, "modelinfo-test")

	adapter.modelList = []models.ModelInfo{{Name: "old-model", DisplayName: "Old", ContextWindow: 100}}
	if _, err := service.Refresh(context.Background(), "modelinfo-test"); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	adapter.modelList = []models.ModelInfo{
		{Name: "old-model", DisplayName: "Changed", ContextWindow: 999},
		{Name: "new-model", DisplayName: "New", ContextWindow: 50},
	}
	if _, err := service.Refresh(context.Background(), "modelinfo-test"); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	got := service.PeekModelInfos("modelinfo-test")
	if len(got) != 2 {
		t.Fatalf("merged list: got %+v", got)
	}
	if got[0].Name != "old-model" || got[0].ContextWindow != 999 {
		t.Errorf("existing record must take the fresh row, got %+v", got[0])
	}
	if got[1].Name != "new-model" || got[1].ContextWindow != 50 {
		t.Errorf("new model must be appended, got %+v", got[1])
	}
}

func TestModelInfoService_ModalityCapsStripped(t *testing.T) {
	service, credentialService, providerService, _ := setupModelInfoService(t)
	addModelInfoCredential(t, credentialService, "modelinfo-test")
	adapterFor(t, providerService, "modelinfo-test").modelList = []models.ModelInfo{
		{Name: "m", Capabilities: []string{"tools", "vision", "audio"}},
	}
	if _, err := service.Refresh(context.Background(), "modelinfo-test"); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	for _, got := range service.PeekModelInfos("modelinfo-test") {
		for _, c := range got.Capabilities {
			if c == "vision" || c == "audio" {
				t.Fatalf("modality chip must not render: %+v", got.Capabilities)
			}
		}
	}
	if got, err := service.GetModelInfos(context.Background(), "modelinfo-test"); err != nil {
		t.Fatalf("get: %v", err)
	} else {
		for _, c := range got[0].Capabilities {
			if c == "vision" || c == "audio" {
				t.Fatalf("modality chip must not render: %+v", got[0].Capabilities)
			}
		}
		if len(got[0].Capabilities) != 1 || got[0].Capabilities[0] != "tools" {
			t.Fatalf("tools must survive: %+v", got[0].Capabilities)
		}
	}
}

func TestModelInfoService_InvalidateProviderClearsMemoryCache(t *testing.T) {
	service, credentialService, providerService, _ := setupModelInfoService(t)
	addModelInfoCredential(t, credentialService, "modelinfo-test")

	if _, err := service.GetModelInfos(context.Background(), "modelinfo-test"); err != nil {
		t.Fatalf("initial fetch failed: %v", err)
	}
	if err := service.InvalidateProvider("modelinfo-test"); err != nil {
		t.Fatalf("invalidate provider failed: %v", err)
	}
	if _, err := service.GetModelInfos(context.Background(), "modelinfo-test"); err != nil {
		t.Fatalf("refetch failed: %v", err)
	}

	if got := adapterFor(t, providerService, "modelinfo-test").callCount; got != 2 {
		t.Fatalf("expected adapter to be called twice after provider invalidation, got %d", got)
	}
}

func TestModelInfoService_InvalidateAllClearsMemoryCache(t *testing.T) {
	service, credentialService, providerService, _ := setupModelInfoService(t)
	addModelInfoCredential(t, credentialService, "modelinfo-test")

	if _, err := service.GetModelInfos(context.Background(), "modelinfo-test"); err != nil {
		t.Fatalf("initial fetch failed: %v", err)
	}
	if err := service.InvalidateAll(); err != nil {
		t.Fatalf("invalidate all failed: %v", err)
	}
	if _, err := service.GetModelInfos(context.Background(), "modelinfo-test"); err != nil {
		t.Fatalf("refetch failed: %v", err)
	}

	if got := adapterFor(t, providerService, "modelinfo-test").callCount; got != 2 {
		t.Fatalf("expected adapter to be called twice after cache reset, got %d", got)
	}
}

func TestModelInfoService_IgnoresLegacyDatabaseCache(t *testing.T) {
	service, credentialService, providerService, database := setupModelInfoService(t)
	addModelInfoCredential(t, credentialService, "modelinfo-test")

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
		b, err := tx.CreateBucketIfNotExists([]byte("model_info"))
		if err != nil {
			return err
		}
		return b.Put([]byte("modelinfo-test"), data)
	}); err != nil {
		t.Fatalf("seed legacy model_info bucket: %v", err)
	}

	got, err := service.GetModelInfos(context.Background(), "modelinfo-test")
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}

	if count := adapterFor(t, providerService, "modelinfo-test").callCount; count != 1 {
		t.Fatalf("expected live adapter fetch, got call count %d", count)
	}
	if len(got) != 1 || got[0].Name != "live-model" {
		t.Fatalf("expected live model info, got %+v", got)
	}
}

func TestModelInfoService_SupportsCredentialFreeDiscovery(t *testing.T) {
	service, _, providerService, _ := setupModelInfoService(t)

	got, err := service.GetModelInfos(context.Background(), "modelinfo-nocred")
	if err != nil {
		t.Fatalf("credential-free fetch failed: %v", err)
	}

	if count := adapterFor(t, providerService, "modelinfo-nocred").callCount; count != 1 {
		t.Fatalf("expected credential-free adapter to be called once, got %d", count)
	}
	if len(got) != 1 || got[0].Name != "nocred-model" {
		t.Fatalf("unexpected credential-free models: %+v", got)
	}
}

func TestModelInfoService_NoCredentialsDoesNotCrashOnAdapterPanic(t *testing.T) {
	service, _, providerService, _ := setupModelInfoService(t)

	got, err := service.GetModelInfos(context.Background(), "modelinfo-panic-nocred")
	if err == nil {
		t.Fatalf("expected missing-credentials error")
	}

	if len(got) != 0 {
		t.Fatalf("expected no models, got %+v", got)
	}
	if count := adapterFor(t, providerService, "modelinfo-panic-nocred").callCount; count != 1 {
		t.Fatalf("expected panic adapter to be called once, got %d", count)
	}
	if !strings.Contains(err.Error(), "no credentials available for provider modelinfo-panic-nocred") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestModelInfoService_MergeRetainedFreshWins(t *testing.T) {
	service, credentialService, providerService, _ := setupModelInfoService(t)
	addModelInfoCredential(t, credentialService, "modelinfo-test")
	ctx := context.Background()

	if _, err := service.GetModelInfos(ctx, "modelinfo-test"); err != nil {
		t.Fatalf("initial fetch failed: %v", err)
	}
	adapterFor(t, providerService, "modelinfo-test").modelList = []models.ModelInfo{
		{Name: "live-model", DisplayName: "Live Model", ContextWindow: 8192,
			InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}},
		{Name: "new-model", DisplayName: "New Model", ContextWindow: 1024},
	}
	refreshed, err := service.Refresh(ctx, "modelinfo-test")
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	byName := make(map[string]models.ModelInfo, len(refreshed))
	for _, model := range refreshed {
		byName[model.Name] = model
	}
	live, ok := byName["live-model"]
	if !ok {
		t.Fatalf("live-model missing after refresh: %+v", refreshed)
	}
	if live.ContextWindow != 8192 {
		t.Errorf("live-model kept stale context window %d, want 8192", live.ContextWindow)
	}
	if len(live.InputModalities) != 2 || live.InputModalities[1] != "image" {
		t.Errorf("live-model kept stale modalities %v, want [text image]", live.InputModalities)
	}
	if _, ok := byName["new-model"]; !ok {
		t.Errorf("new-model not appended: %+v", refreshed)
	}

	// Flap: a fetch omitting a known name retains it; confirmed removals
	// still go through RemoveModel.
	adapterFor(t, providerService, "modelinfo-test").modelList = []models.ModelInfo{
		{Name: "new-model", DisplayName: "New Model", ContextWindow: 1024},
	}
	flapped, err := service.Refresh(ctx, "modelinfo-test")
	if err != nil {
		t.Fatalf("flap refresh failed: %v", err)
	}
	if len(flapped) != 2 {
		t.Fatalf("flap dropped the retained model: %+v", flapped)
	}
	removed, err := service.RemoveModel("modelinfo-test", "live-model")
	if err != nil || !removed {
		t.Fatalf("RemoveModel(live-model) = %v, %v", removed, err)
	}
	kept := service.PeekModelInfos("modelinfo-test")
	for _, model := range kept {
		if model.Name == "live-model" {
			t.Fatalf("live-model survived RemoveModel: %+v", kept)
		}
	}
}
