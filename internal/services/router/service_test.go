package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// mockAdapter for router tests with configurable behavior.
// It is the backend: it receives the whole credential pool in one call.
type mockAdapter struct {
	completeFunc func(context.Context, []*models.Credential, *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error)
	callCount    *int
	seenPools    *[][]*models.Credential
}

func (m *mockAdapter) TypeKey() string { return "mock" }
func (m *mockAdapter) ValidateCredentials(data map[string]any) error {
	if s, _ := data["api_key"].(string); s == "" {
		return fmt.Errorf("api_key required")
	}
	return nil
}
func (m *mockAdapter) Complete(ctx context.Context, creds []*models.Credential, req *models.ChatCompletionRequest, _ map[string]any) (*models.ChatCompletionResponse, error) {
	if m.callCount != nil {
		*m.callCount++
	}
	if m.seenPools != nil {
		*m.seenPools = append(*m.seenPools, creds)
	}
	if m.completeFunc != nil {
		return m.completeFunc(ctx, creds, req)
	}
	return &models.ChatCompletionResponse{
		ID:      "test-response",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   string(req.Model),
		Choices: []models.ChatCompletionChoice{{Index: 0, Message: models.ChatMessage{Role: "assistant", Content: "test"}, FinishReason: "stop"}},
	}, nil
}
func (m *mockAdapter) CompleteStream(ctx context.Context, creds []*models.Credential, req *models.ChatCompletionRequest, w io.Writer, _ map[string]any) error {
	return nil
}
func (m *mockAdapter) NeedsRefresh(cred *models.Credential) bool { return false }
func (m *mockAdapter) RefreshCredential(ctx context.Context, cred *models.Credential) (map[string]any, error) {
	return nil, fmt.Errorf("no refresh needed for this credential type")
}
func (m *mockAdapter) GetModelInfos(ctx context.Context, cred *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	return []models.ModelInfo{{Name: "mock-model", DisplayName: "Mock", ContextWindow: 4096}}, nil
}

func setupRouterService(t *testing.T) (*Service, *credential.Service, *modelinfo.Service, *mockAdapter) {
	t.Helper()
	database := testutil.SetupTestDB(t)

	providerSvc := provider.NewService(database)
	mock := &mockAdapter{callCount: new(int), seenPools: &[][]*models.Credential{}}
	providerSvc.RegisterGoAdapter(mock)
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)

	routerSvc := New(providerSvc, credSvc, modelInfoSvc, slog.Default())

	return routerSvc, credSvc, modelInfoSvc, mock
}

// ─────────────────────────────────────────────
// Basic Routing Tests
// ─────────────────────────────────────────────

func TestRouterService_Complete_Success(t *testing.T) {
	svc, credSvc, _, _ := setupRouterService(t)

	credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Test Cred",
		Data:       map[string]any{"api_key": "test-key"},
	})

	req := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	resp, err := svc.Complete(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}

	if resp.ID != "test-response" {
		t.Errorf("response ID: got %q, want %q", resp.ID, "test-response")
	}
}

func TestRouterService_Complete_InvalidModelId(t *testing.T) {
	svc, _, _, _ := setupRouterService(t)

	req := &models.ChatCompletionRequest{
		Model:    "invalid-model-id",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := svc.Complete(context.Background(), req, nil)
	if err == nil {
		t.Error("expected error for invalid model ID, got nil")
	}
}

func TestRouterService_Complete_ProviderNotFound(t *testing.T) {
	svc, _, _, _ := setupRouterService(t)

	req := &models.ChatCompletionRequest{
		Model:    "nonexistent/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := svc.Complete(context.Background(), req, nil)
	if err == nil {
		t.Error("expected error for nonexistent provider, got nil")
	}
}

func TestRouterService_Complete_NoCredentials(t *testing.T) {
	svc, _, _, _ := setupRouterService(t)

	req := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := svc.Complete(context.Background(), req, nil)
	if err == nil {
		t.Error("expected error when no credentials available, got nil")
	}
}

// ─────────────────────────────────────────────
// Single-pass Backend Tests: the router calls the backend once with the
// whole pool; key iteration lives inside the backend.
// ─────────────────────────────────────────────

func TestRouterService_Complete_PassesFullPoolInSingleCall(t *testing.T) {
	svc, credSvc, _, mock := setupRouterService(t)

	credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})
	credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 2",
		Data:       map[string]any{"api_key": "key2"},
	})

	req := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	resp, err := svc.Complete(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if resp.ID != "test-response" {
		t.Errorf("expected test response, got %q", resp.ID)
	}

	if *mock.callCount != 1 {
		t.Errorf("expected 1 backend call, got %d", *mock.callCount)
	}
	if len(*mock.seenPools) != 1 || len((*mock.seenPools)[0]) != 2 {
		t.Errorf("expected one call with a 2-credential pool, got %v", *mock.seenPools)
	}
}

func TestRouterService_Complete_BackendErrorSurfacesWithoutRepeat(t *testing.T) {
	svc, credSvc, _, mock := setupRouterService(t)

	credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})
	credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 2",
		Data:       map[string]any{"api_key": "key2"},
	})

	mock.completeFunc = func(ctx context.Context, creds []*models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		resetAt := time.Now().Add(60 * time.Second)
		return nil, &models.ProviderError{
			StatusCode: 429,
			Message:    "rate limit exceeded",
			Type:       models.ErrorTypeRateLimit,
			RetryAfter: &resetAt,
		}
	}

	req := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := svc.Complete(context.Background(), req, nil)
	if err == nil {
		t.Error("expected backend error, got nil")
	}

	if *mock.callCount != 1 {
		t.Errorf("expected 1 backend call (no repeat passes), got %d", *mock.callCount)
	}
}

func TestRouterService_TestVision_Success(t *testing.T) {
	svc, credSvc, _, _ := setupRouterService(t)

	credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})

	res := svc.TestVision(context.Background(), "mock/test-model")
	if !res.OK {
		t.Fatalf("vision probe failed: %v", res.Error)
	}
}

func TestRouterService_TestModel_BypassesManualDisable(t *testing.T) {
	svc, credSvc, modelInfoSvc, _ := setupRouterService(t)

	credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})
	if err := modelInfoSvc.SetOverride(models.ModelOverride{
		ProviderID: "mock",
		Name:       "test-model",
		Disabled:   true,
	}); err != nil {
		t.Fatalf("disable model: %v", err)
	}

	req := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}
	if _, err := svc.Complete(context.Background(), req, nil); !errors.Is(err, apierrors.ErrModelDisabled) {
		t.Fatalf("expected ErrModelDisabled from routing, got %v", err)
	}

	res := svc.TestModel(context.Background(), "mock/test-model")
	if !res.OK {
		t.Fatalf("probe of disabled model must go through, got %v", res.Error)
	}
}

func TestRouterService_Complete_AuthErrorSingleAttempt(t *testing.T) {
	svc, credSvc, _, mock := setupRouterService(t)

	credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})

	mock.completeFunc = func(ctx context.Context, creds []*models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		return nil, &models.ProviderError{
			StatusCode: 401,
			Message:    "authentication failed",
			Type:       models.ErrorTypeAuth,
		}
	}

	req := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := svc.Complete(context.Background(), req, nil)
	if err == nil {
		t.Error("expected error for auth failure, got nil")
	}

	if *mock.callCount != 1 {
		t.Errorf("expected 1 call, got %d", *mock.callCount)
	}
}

func TestRouterService_TestModel_MarksQuotaExceeded(t *testing.T) {
	svc, credSvc, _, mock := setupRouterService(t)

	credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})
	mock.completeFunc = func(ctx context.Context, creds []*models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		return nil, &models.ProviderError{
			StatusCode: 429,
			Message:    "quota exhausted",
			Type:       models.ErrorTypeQuotaExceeded,
		}
	}

	res := svc.TestModel(context.Background(), "mock/test-model")
	if res.OK {
		t.Fatal("expected failed probe, got ok")
	}
	if !res.QuotaExceeded {
		t.Error("quota failure must set quota_exceeded so callers never disable over it")
	}
}
