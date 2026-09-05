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

func (a *runtimeTestAdapter) TypeKey() string                                  { return "runtime-test" }
func (a *runtimeTestAdapter) AuthType() models.AuthType                        { return models.AuthTypeAPIKey }
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

func TestProviderService_CustomProviderCRUD(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)

	// Create
	cp, err := svc.CreateCustom("My LLM", "https://api.example.com/v1/", "https://example.com/icon.svg")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if cp.ID != "my-llm" {
		t.Fatalf("id: got %q, want %q", cp.ID, "my-llm")
	}
	if cp.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("base_url should be normalized (no trailing slash): got %q", cp.BaseURL)
	}

	// Get via provider ID
	p, err := svc.Get("custom:my-llm")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if p.Type != "custom" {
		t.Fatalf("type: got %q, want %q", p.Type, "custom")
	}
	if p.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("base_url: got %q", p.BaseURL)
	}

	// List includes custom provider
	providers, err := svc.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if !containsProvider(providers, "custom:my-llm") {
		t.Fatalf("custom provider missing from list")
	}

	// Duplicate names get unique slugs
	cp2, err := svc.CreateCustom("My LLM", "https://other.example.com/v1", "")
	if err != nil {
		t.Fatalf("second create failed: %v", err)
	}
	if cp2.ID != "my-llm-2" {
		t.Fatalf("duplicate id: got %q, want %q", cp2.ID, "my-llm-2")
	}

	// Update
	updated, err := svc.UpdateCustom("my-llm", "My LLM Updated", "https://new.example.com/v1", "")
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.Name != "My LLM Updated" {
		t.Fatalf("name: got %q", updated.Name)
	}

	// Delete
	if err := svc.DeleteCustom("my-llm-2"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := svc.Get("custom:my-llm-2"); err == nil {
		t.Fatalf("expected deleted provider lookup to fail")
	}
}

func TestProviderService_CreateCustom_Validation(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := provider.NewService(database)

	if _, err := svc.CreateCustom("", "https://api.example.com/v1", ""); err == nil {
		t.Fatalf("expected error for empty name")
	}
	if _, err := svc.CreateCustom("Test", "", ""); err == nil {
		t.Fatalf("expected error for empty base_url")
	}
	if _, err := svc.CreateCustom("Test", "not-a-url", ""); err == nil {
		t.Fatalf("expected error for invalid base_url")
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
