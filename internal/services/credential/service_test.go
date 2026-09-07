package credential

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// mockAdapter implements provider.GoAdapter for testing
type mockAdapter struct{}

func (m *mockAdapter) TypeKey() string { return "mock" }
func (m *mockAdapter) ValidateCredentials(data map[string]any) error {
	if s, _ := data["api_key"].(string); s == "" {
		return fmt.Errorf("api_key required")
	}
	return nil
}
func (m *mockAdapter) Complete(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest, _ map[string]any) (*models.ChatCompletionResponse, error) {
	return &models.ChatCompletionResponse{
		ID:      "mock-" + fmt.Sprintf("%d", time.Now().Unix()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   string(req.Model),
		Choices: []models.ChatCompletionChoice{
			{
				Index: 0,
				Message: models.ChatMessage{
					Role:    "assistant",
					Content: "mock response",
				},
				FinishReason: "stop",
			},
		},
	}, nil
}
func (m *mockAdapter) CompleteStream(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest, w io.Writer, _ map[string]any) error {
	chunk := models.StreamChunk{
		ID:      "mock-stream",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   string(req.Model),
	}
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	return nil
}
func (m *mockAdapter) NeedsRefresh(cred *models.Credential) bool { return false }
func (m *mockAdapter) RefreshCredential(ctx context.Context, cred *models.Credential) (map[string]any, error) {
	return nil, fmt.Errorf("no refresh needed for this credential type")
}
func (m *mockAdapter) GetModelInfos(ctx context.Context, cred *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	return []models.ModelInfo{
		{Name: "mock-model", DisplayName: "Mock Model", ContextWindow: 4096, MaxTokens: 2048},
	}, nil
}

func setupCredentialService(t *testing.T) (*Service, *provider.Service) {
	t.Helper()
	database := testutil.SetupTestDB(t)

	providerSvc := provider.NewService(database)
	providerSvc.RegisterGoAdapter(&mockAdapter{})
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credSvc := New(database, providerSvc)

	return credSvc, providerSvc
}

// ─────────────────────────────────────────────
// Add Tests
// ─────────────────────────────────────────────

func TestCredentialService_Add_Valid(t *testing.T) {
	svc, _ := setupCredentialService(t)

	cred, err := svc.Add(AddOptions{
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
	svc, _ := setupCredentialService(t)

	_, err := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Invalid",
		Data:       map[string]any{},
	})

	if err == nil {
		t.Error("expected error for invalid credentials, got nil")
	}
}

func TestCredentialService_Add_InvalidProvider(t *testing.T) {
	svc, _ := setupCredentialService(t)

	_, err := svc.Add(AddOptions{
		ProviderID: "nonexistent",
		Label:      "Test",
		Data:       map[string]any{"api_key": "test"},
	})

	if err == nil {
		t.Error("expected error for nonexistent provider, got nil")
	}
}

// ─────────────────────────────────────────────
// Next Tests (LRU Selection)
// ─────────────────────────────────────────────

func TestCredentialService_Next_NeverUsed(t *testing.T) {
	svc, _ := setupCredentialService(t)

	cred1, _ := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})
	cred2, _ := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Cred 2",
		Data:       map[string]any{"api_key": "key2"},
	})

	next, err := svc.Next("mock")
	if err != nil {
		t.Fatalf("next failed: %v", err)
	}

	if next.ID != cred1.ID && next.ID != cred2.ID {
		t.Errorf("unexpected credential returned: %s", next.ID)
	}
}

func TestCredentialService_Next_LRU(t *testing.T) {
	svc, _ := setupCredentialService(t)

	cred1, _ := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})
	cred2, _ := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Cred 2",
		Data:       map[string]any{"api_key": "key2"},
	})

	if err := svc.UpdateUsage(cred1.ID, true); err != nil {
		t.Fatalf("update usage failed: %v", err)
	}

	next, err := svc.Next("mock")
	if err != nil {
		t.Fatalf("next failed: %v", err)
	}

	if next.ID != cred2.ID {
		t.Errorf("expected cred2 (never used), got %s", next.ID)
	}
}

func TestCredentialService_Next_NoCredentials(t *testing.T) {
	svc, _ := setupCredentialService(t)

	_, err := svc.Next("mock")
	if err == nil {
		t.Error("expected error when no credentials available, got nil")
	}
}

// ─────────────────────────────────────────────
// All Tests (Priority Ordering)
// ─────────────────────────────────────────────

func TestCredentialService_All_SortedByPriority(t *testing.T) {
	svc, _ := setupCredentialService(t)

	neverUsed, _ := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Never Used",
		Data:       map[string]any{"api_key": "key1"},
	})

	normal, _ := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Normal",
		Data:       map[string]any{"api_key": "key2"},
	})
	svc.UpdateUsage(normal.ID, true)

	quotaExceeded, _ := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Quota Exceeded",
		Data:       map[string]any{"api_key": "key3"},
	})
	svc.UpdateUsage(quotaExceeded.ID, true)
	svc.MarkQuotaExceeded(quotaExceeded.ID, time.Now().Add(1*time.Hour))

	all, err := svc.All("mock")
	if err != nil {
		t.Fatalf("all failed: %v", err)
	}

	if len(all) != 3 {
		t.Fatalf("expected 3 credentials, got %d", len(all))
	}

	if all[0].ID != neverUsed.ID {
		t.Errorf("first should be never used, got %s", all[0].ID)
	}
	if all[1].ID != normal.ID {
		t.Errorf("second should be normal, got %s", all[1].ID)
	}
	if all[2].ID != quotaExceeded.ID {
		t.Errorf("third should be quota exceeded, got %s", all[2].ID)
	}
}

func TestCredentialService_All_ExcludesExpired(t *testing.T) {
	svc, _ := setupCredentialService(t)

	_, _ = svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Normal",
		Data:       map[string]any{"api_key": "key1"},
	})

	expired, _ := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Expired",
		Data:       map[string]any{"api_key": "key2"},
	})
	past := time.Now().Add(-1 * time.Hour)
	svc.Update(expired.ID, map[string]any{"api_key": "key2"}, &past)

	all, err := svc.All("mock")
	if err != nil {
		t.Fatalf("all failed: %v", err)
	}

	if len(all) != 1 {
		t.Errorf("expected 1 non-expired credential, got %d", len(all))
	}
}

// ─────────────────────────────────────────────
// UpdateUsage Tests
// ─────────────────────────────────────────────

func TestCredentialService_UpdateUsage_Success(t *testing.T) {
	svc, _ := setupCredentialService(t)

	cred, _ := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Test",
		Data:       map[string]any{"api_key": "key1"},
	})

	if err := svc.UpdateUsage(cred.ID, true); err != nil {
		t.Fatalf("update usage failed: %v", err)
	}

	updated, err := svc.Get(cred.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if updated.RequestCount != 1 {
		t.Errorf("request count: got %d, want 1", updated.RequestCount)
	}
	if updated.SuccessCount != 1 {
		t.Errorf("success count: got %d, want 1", updated.SuccessCount)
	}
	if updated.LastUsedAt == nil {
		t.Error("last used should be set")
	}
}

func TestCredentialService_UpdateUsage_Failure(t *testing.T) {
	svc, _ := setupCredentialService(t)

	cred, _ := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Test",
		Data:       map[string]any{"api_key": "key1"},
	})

	if err := svc.UpdateUsage(cred.ID, false); err != nil {
		t.Fatalf("update usage failed: %v", err)
	}

	updated, err := svc.Get(cred.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if updated.RequestCount != 1 {
		t.Errorf("request count: got %d, want 1", updated.RequestCount)
	}
	if updated.FailureCount != 1 {
		t.Errorf("failure count: got %d, want 1", updated.FailureCount)
	}
}

// ─────────────────────────────────────────────
// MarkQuotaExceeded Tests
// ─────────────────────────────────────────────

func TestCredentialService_MarkQuotaExceeded(t *testing.T) {
	svc, _ := setupCredentialService(t)

	cred, _ := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Test",
		Data:       map[string]any{"api_key": "key1"},
	})

	resetAt := time.Now().Add(1 * time.Hour)
	if err := svc.MarkQuotaExceeded(cred.ID, resetAt); err != nil {
		t.Fatalf("mark quota exceeded failed: %v", err)
	}

	updated, err := svc.Get(cred.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if !updated.IsQuotaExceeded() {
		t.Error("credential should be quota exceeded")
	}
	if updated.Priority() != 2 {
		t.Errorf("priority: got %d, want 2", updated.Priority())
	}
}

// ─────────────────────────────────────────────
// Delete Tests
// ─────────────────────────────────────────────

func TestCredentialService_Delete(t *testing.T) {
	svc, _ := setupCredentialService(t)

	cred, _ := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Test",
		Data:       map[string]any{"api_key": "key1"},
	})

	if err := svc.Delete(cred.ID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err := svc.Get(cred.ID)
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

// ─────────────────────────────────────────────
// Update Tests
// ─────────────────────────────────────────────

func TestCredentialService_Update(t *testing.T) {
	svc, _ := setupCredentialService(t)

	cred, _ := svc.Add(AddOptions{
		ProviderID: "mock",
		Label:      "Test",
		Data:       map[string]any{"api_key": "old-key"},
	})

	newData := map[string]any{"api_key": "new-key"}
	future := time.Now().Add(1 * time.Hour)

	if err := svc.Update(cred.ID, newData, &future); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	updated, err := svc.Get(cred.ID)
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
