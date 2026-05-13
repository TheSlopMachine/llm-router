package provider_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	bolt "go.etcd.io/bbolt"

	sdk "github.com/TheSlopMachine/llm-router-sdk"
	_ "github.com/TheSlopMachine/llm-router/providers/agents"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

type runtimeTestAdapter struct{}

func (a *runtimeTestAdapter) TypeKey() string { return "runtime-test" }
func (a *runtimeTestAdapter) AuthType() models.AuthType { return models.AuthTypeAPIKey }
func (a *runtimeTestAdapter) ValidateCredentials(data map[string]string) error { return nil }
func (a *runtimeTestAdapter) Complete(ctx context.Context, cred *sdk.Credential, req *sdk.ChatCompletionRequest) (*sdk.ChatCompletionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (a *runtimeTestAdapter) CompleteStream(ctx context.Context, cred *sdk.Credential, req *sdk.ChatCompletionRequest, w io.Writer) error {
	return fmt.Errorf("not implemented")
}
func (a *runtimeTestAdapter) NeedsRefresh(cred *sdk.Credential) bool { return false }
func (a *runtimeTestAdapter) RefreshCredential(ctx context.Context, cred *sdk.Credential) (*sdk.Credential, error) {
	return nil, provider.ErrNoRefreshNeeded
}
func (a *runtimeTestAdapter) GetModelInfos(ctx context.Context, cred *sdk.Credential, providerQualifier string) ([]sdk.ModelInfo, error) {
	return nil, nil
}
func (a *runtimeTestAdapter) GetAuthFlow() provider.AuthFlowHandler { return nil }
func (a *runtimeTestAdapter) GetDefaultProviders() []provider.ProviderInfo {
	return []provider.ProviderInfo{
		{Name: "Runtime Test Default", Qualifier: "", BaseURL: "https://default.example.com", IconURL: "default"},
		{Name: "Runtime Test Alt", Qualifier: "alt", BaseURL: "https://alt.example.com", IconURL: "alt"},
	}
}

func ensureRuntimeTestAdapter(t *testing.T) {
	t.Helper()
	if _, err := provider.Lookup("runtime-test"); err == nil {
		return
	}
	provider.Register(&runtimeTestAdapter{})
}

func containsProvider(providers []*models.Provider, id string) bool {
	for _, p := range providers {
		if p.ID == id {
			return true
		}
	}
	return false
}

func TestProviderService_ListReturnsRuntimeProviders(t *testing.T) {
	ensureRuntimeTestAdapter(t)

	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)

	providers, err := svc.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	if !containsProvider(providers, "runtime-test") {
		t.Fatalf("runtime provider list is missing runtime-test")
	}
	if !containsProvider(providers, "runtime-test:alt") {
		t.Fatalf("runtime provider list is missing runtime-test:alt")
	}
	if !containsProvider(providers, "agents") {
		t.Fatalf("runtime provider list is missing agents")
	}
}

func TestProviderService_GetResolvesQualifiedProvider(t *testing.T) {
	ensureRuntimeTestAdapter(t)

	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)

	provider, err := svc.Get("runtime-test:alt")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if provider.Type != "runtime-test" {
		t.Fatalf("type: got %q, want %q", provider.Type, "runtime-test")
	}
	if provider.Qualifier != "alt" {
		t.Fatalf("qualifier: got %q, want %q", provider.Qualifier, "alt")
	}
	if provider.Name != "Runtime Test Alt" {
		t.Fatalf("name: got %q, want %q", provider.Name, "Runtime Test Alt")
	}
}

func TestProviderService_IgnoresLegacyProviderBucketData(t *testing.T) {
	ensureRuntimeTestAdapter(t)

	database := testutil.SetupTestDB(t)
	stale := &models.Provider{
		ID:       "stale-provider",
		Name:     "Stale Provider",
		Type:     "stale",
		AuthType: models.AuthTypeAPIKey,
	}
	data, err := json.Marshal(stale)
	if err != nil {
		t.Fatalf("marshal stale provider: %v", err)
	}

	if err := database.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(db.BucketProviders)
		if err != nil {
			return err
		}
		return b.Put([]byte(stale.ID), data)
	}); err != nil {
		t.Fatalf("seed legacy providers bucket: %v", err)
	}

	svc := provider.NewService(database)
	providers, err := svc.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if containsProvider(providers, stale.ID) {
		t.Fatalf("legacy provider bucket entry %q should not appear in runtime provider list", stale.ID)
	}

	if _, err := svc.Get(stale.ID); err == nil {
		t.Fatalf("expected stale provider lookup to fail")
	}
}
