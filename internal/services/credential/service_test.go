package credential

import (
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func setupCredentialService(t *testing.T) (*Service, *provider.Service) {
	t.Helper()
	database := testutil.SetupTestDB(t)

	providerService := provider.NewService(database)
	providerService.RegisterGoAdapter(testutil.NewMockAdapter("mock"))
	if _, err := providerService.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credentialService := New(database, providerService)

	return credentialService, providerService
}

// ─────────────────────────────────────────────
// Add Tests
// ─────────────────────────────────────────────

func TestCredentialService_Add_Valid(t *testing.T) {
	service, _ := setupCredentialService(t)

	cred, err := service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Test Credential",
		Data:       map[string]any{"api_key": "test-key"},
	})

	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	if cred.ID == "" {
		t.Error("credential ID should not be empty")
	}
	if cred.ProviderID != "mock" {
		t.Errorf("provider ID: got %q, want %q", cred.ProviderID, "mock")
	}
	if cred.Label != "Test Credential" {
		t.Errorf("label: got %q, want %q", cred.Label, "Test Credential")
	}
}

func TestCredentialService_Add_InvalidData(t *testing.T) {
	service, _ := setupCredentialService(t)

	_, err := service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Invalid",
		Data:       map[string]any{},
	})

	if err == nil {
		t.Error("expected error for invalid credentials, got nil")
	}
}

func TestCredentialService_Add_InvalidProvider(t *testing.T) {
	service, _ := setupCredentialService(t)

	_, err := service.Add(AddOptions{
		ProviderID: "nonexistent",
		Label:      "Test",
		Data:       map[string]any{"api_key": "test"},
	})

	if err == nil {
		t.Error("expected error for nonexistent provider, got nil")
	}
}

// ─────────────────────────────────────────────
// Pool Head Tests (LRU Selection via All)
// ─────────────────────────────────────────────

func TestCredentialService_Next_NeverUsed(t *testing.T) {
	service, _ := setupCredentialService(t)

	cred1, _ := service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})
	cred2, _ := service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Cred 2",
		Data:       map[string]any{"api_key": "key2"},
	})

	next, err := service.All("mock")
	if err != nil {
		t.Fatalf("all failed: %v", err)
	}

	if len(next) != 2 {
		t.Fatalf("expected 2 credentials, got %d", len(next))
	}
	if next[0].ID != cred1.ID && next[0].ID != cred2.ID {
		t.Errorf("unexpected credential at pool head: %s", next[0].ID)
	}
}

func TestCredentialService_ListUsable_EmptyPoolYieldsEmpty(t *testing.T) {
	service, _ := setupCredentialService(t)

	got, err := service.ListUsable("mock")
	if err != nil {
		t.Fatalf("empty pool must not error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty pool must yield empty slice, got %v", got)
	}

	if _, err := service.All("mock"); err == nil {
		t.Fatal("All must deny empty pools for the router gate")
	}
}

func TestCredentialService_All_ReturnsBoth(t *testing.T) {
	service, _ := setupCredentialService(t)

	cred1, _ := service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})
	cred2, _ := service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Cred 2",
		Data:       map[string]any{"api_key": "key2"},
	})

	all, err := service.All("mock")
	if err != nil {
		t.Fatalf("all failed: %v", err)
	}

	if len(all) != 2 {
		t.Fatalf("expected 2 credentials, got %d", len(all))
	}
	seen := map[string]bool{cred1.ID: false, cred2.ID: false}
	for _, c := range all {
		seen[c.ID] = true
	}
	for id, ok := range seen {
		if !ok {
			t.Errorf("missing credential %s", id)
		}
	}
}

func TestCredentialService_Next_NoCredentials(t *testing.T) {
	service, _ := setupCredentialService(t)

	_, err := service.All("mock")
	if err == nil {
		t.Error("expected error when no credentials available, got nil")
	}
}

// ─────────────────────────────────────────────
// All Tests (Stored Set)
// ─────────────────────────────────────────────

func TestCredentialService_All_ContainsBoth(t *testing.T) {
	service, _ := setupCredentialService(t)

	neverUsed, _ := service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Never Used",
		Data:       map[string]any{"api_key": "key1"},
	})

	normal, _ := service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Normal",
		Data:       map[string]any{"api_key": "key2"},
	})

	all, err := service.All("mock")
	if err != nil {
		t.Fatalf("all failed: %v", err)
	}

	if len(all) != 2 {
		t.Fatalf("expected 2 credentials, got %d", len(all))
	}

	seen := map[string]bool{}
	for _, c := range all {
		seen[c.ID] = true
	}
	if !seen[neverUsed.ID] || !seen[normal.ID] {
		t.Errorf("stored set incomplete: %v", seen)
	}
}

func TestCredentialService_All_ExcludesExpired(t *testing.T) {
	service, _ := setupCredentialService(t)

	_, _ = service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Normal",
		Data:       map[string]any{"api_key": "key1"},
	})

	expired, _ := service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Expired",
		Data:       map[string]any{"api_key": "key2"},
	})
	past := time.Now().Add(-1 * time.Hour)
	service.Update(expired.ID, map[string]any{"api_key": "key2"}, &past)

	all, err := service.All("mock")
	if err != nil {
		t.Fatalf("all failed: %v", err)
	}

	if len(all) != 1 {
		t.Errorf("expected 1 non-expired credential, got %d", len(all))
	}
}

// ─────────────────────────────────────────────
// Job Refresh Persist Tests
// ─────────────────────────────────────────────

func TestCredentialService_Update_PersistsJobRefresh(t *testing.T) {
	service, _ := setupCredentialService(t)

	cred, _ := service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Test",
		Data:       map[string]any{"api_key": "key1"},
	})

	refreshed := map[string]any{"api_key": "key1", "access_token": "tok"}
	if err := service.Update(cred.ID, refreshed, nil); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	updated, err := service.Get(cred.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if updated.Data["access_token"] != "tok" {
		t.Errorf("refreshed data not persisted: %v", updated.Data)
	}
}

// ─────────────────────────────────────────────
// Delete Tests
// ─────────────────────────────────────────────

func TestCredentialService_Delete(t *testing.T) {
	service, _ := setupCredentialService(t)

	cred, _ := service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Test",
		Data:       map[string]any{"api_key": "key1"},
	})

	if err := service.Delete(cred.ID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err := service.Get(cred.ID)
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

// ─────────────────────────────────────────────
// Update Tests
// ─────────────────────────────────────────────

func TestCredentialService_Update(t *testing.T) {
	service, _ := setupCredentialService(t)

	cred, _ := service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Test",
		Data:       map[string]any{"api_key": "old-key"},
	})

	newData := map[string]any{"api_key": "new-key"}
	future := time.Now().Add(1 * time.Hour)

	if err := service.Update(cred.ID, newData, &future); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	updated, err := service.Get(cred.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if updated.Data["api_key"] != "new-key" {
		t.Errorf("api_key: got %q, want %q", updated.Data["api_key"], "new-key")
	}
	if updated.ExpiresAt == nil {
		t.Error("expires at should be set")
	}
}

// ─────────────────────────────────────────────
// DeleteByProvider Tests
// ─────────────────────────────────────────────

func TestCredentialService_DeleteByProvider(t *testing.T) {
	service, _ := setupCredentialService(t)

	service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "One",
		Data:       map[string]any{"api_key": "key1"},
	})
	service.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Two",
		Data:       map[string]any{"api_key": "key2"},
	})

	n, err := service.DeleteByProvider("mock")
	if err != nil {
		t.Fatalf("delete by provider failed: %v", err)
	}
	if n != 2 {
		t.Errorf("removed count: got %d, want 2", n)
	}

	remaining, err := service.ListByProvider("mock")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected no remaining credentials, got %d", len(remaining))
	}

	n, err = service.DeleteByProvider("mock")
	if err != nil {
		t.Fatalf("second delete failed: %v", err)
	}
	if n != 0 {
		t.Errorf("second delete count: got %d, want 0", n)
	}
}
