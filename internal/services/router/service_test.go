package router

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// mockAdapter for router tests with configurable behavior
type mockAdapter struct {
	completeFunc func(context.Context, *models.Credential, *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error)
	callCount    *int
}

func (m *mockAdapter) TypeKey() string { return "mock" }
func (m *mockAdapter) ValidateCredentials(data map[string]any) error {
	if s, _ := data["api_key"].(string); s == "" {
		return fmt.Errorf("api_key required")
	}
	return nil
}
func (m *mockAdapter) Complete(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest, _ map[string]any) (*models.ChatCompletionResponse, error) {
	if m.callCount != nil {
		*m.callCount++
	}
	if m.completeFunc != nil {
		return m.completeFunc(ctx, cred, req)
	}
	return &models.ChatCompletionResponse{
		ID:      "test-response",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   string(req.Model),
		Choices: []models.ChatCompletionChoice{{Index: 0, Message: models.ChatMessage{Role: "assistant", Content: "test"}, FinishReason: "stop"}},
	}, nil
}
func (m *mockAdapter) CompleteStream(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest, w io.Writer, _ map[string]any) error {
	return nil
}
func (m *mockAdapter) NeedsRefresh(cred *models.Credential) bool { return false }
func (m *mockAdapter) RefreshCredential(ctx context.Context, cred *models.Credential) (map[string]any, error) {
	return nil, fmt.Errorf("no refresh needed for this credential type")
}
func (m *mockAdapter) GetModelInfos(ctx context.Context, cred *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	return []models.ModelInfo{{Name: "mock-model", DisplayName: "Mock", ContextWindow: 4096}}, nil
}

func setupRouterService(t *testing.T, maxRetries int) (*Service, *credential.Service, *mockAdapter) {
	t.Helper()
	database := testutil.SetupTestDB(t)

	providerSvc := provider.NewService(database)
	mock := &mockAdapter{callCount: new(int)}
	providerSvc.RegisterGoAdapter(mock)
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)

	routerSvc := New(providerSvc, credSvc, modelInfoSvc, maxRetries, slog.Default())

	return routerSvc, credSvc, mock
}

// ─────────────────────────────────────────────
// Basic Routing Tests
// ─────────────────────────────────────────────

func TestRouterService_Complete_Success(t *testing.T) {
	svc, credSvc, _ := setupRouterService(t, 3)

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
	svc, _, _ := setupRouterService(t, 3)

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
	svc, _, _ := setupRouterService(t, 3)

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
	svc, _, _ := setupRouterService(t, 3)

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
// Credential Rotation Tests
// ─────────────────────────────────────────────

func TestRouterService_Complete_RateLimitRotatesToSecond(t *testing.T) {
	svc, credSvc, mock := setupRouterService(t, 3)

	cred1, _ := credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})
	cred2, _ := credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 2",
		Data:       map[string]any{"api_key": "key2"},
	})

	firstCall := true
	mock.completeFunc = func(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		if firstCall {
			firstCall = false
			resetAt := time.Now().Add(60 * time.Second)
			return nil, &models.ProviderError{
				StatusCode: 429,
				Message:    "rate limit exceeded",
				Type:       models.ErrorTypeRateLimit,
				RetryAfter: &resetAt,
			}
		}
		return &models.ChatCompletionResponse{
			ID:      "success",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   string(req.Model),
			Choices: []models.ChatCompletionChoice{{Index: 0, Message: models.ChatMessage{Role: "assistant", Content: "test"}, FinishReason: "stop"}},
		}, nil
	}

	req := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	resp, err := svc.Complete(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}

	if resp.ID != "success" {
		t.Errorf("expected success response")
	}

	_, err1 := credSvc.Get(cred1.ID)
	_, err2 := credSvc.Get(cred2.ID)
	if err1 != nil || err2 != nil {
		t.Errorf("both credentials should exist")
	}
}

func TestRouterService_Complete_QuotaExceededRotates(t *testing.T) {
	svc, credSvc, mock := setupRouterService(t, 3)

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

	firstCall := true
	mock.completeFunc = func(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		if firstCall {
			firstCall = false
			resetAt := time.Now().Add(24 * time.Hour)
			return nil, &models.ProviderError{
				StatusCode: 429,
				Message:    "quota exceeded",
				Type:       models.ErrorTypeQuotaExceeded,
				RetryAfter: &resetAt,
			}
		}
		return &models.ChatCompletionResponse{
			ID:      "success",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   string(req.Model),
			Choices: []models.ChatCompletionChoice{{Index: 0, Message: models.ChatMessage{Role: "assistant", Content: "test"}, FinishReason: "stop"}},
		}, nil
	}

	req := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	resp, err := svc.Complete(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}

	if resp.ID != "success" {
		t.Errorf("expected success response")
	}
}

// ─────────────────────────────────────────────
// Error Handling Tests
// ─────────────────────────────────────────────

func TestRouterService_Complete_AuthErrorNoRetry(t *testing.T) {
	svc, credSvc, mock := setupRouterService(t, 3)

	credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})

	mock.completeFunc = func(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
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
		t.Errorf("expected 1 call (no retry on auth error), got %d", *mock.callCount)
	}
}

func TestRouterService_Complete_UpstreamErrorNoRetry(t *testing.T) {
	svc, credSvc, mock := setupRouterService(t, 3)

	credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})

	mock.completeFunc = func(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		return nil, &models.ProviderError{
			StatusCode: 500,
			Message:    "upstream error",
			Type:       models.ErrorTypeUpstream,
		}
	}

	req := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := svc.Complete(context.Background(), req, nil)
	if err == nil {
		t.Error("expected error for upstream failure, got nil")
	}

	if *mock.callCount != 1 {
		t.Errorf("expected 1 call (no retry on upstream error), got %d", *mock.callCount)
	}
}

// ─────────────────────────────────────────────
// Usage Tracking Tests
// ─────────────────────────────────────────────

func TestRouterService_Complete_UpdatesUsageOnSuccess(t *testing.T) {
	svc, credSvc, mock := setupRouterService(t, 3)

	mock.completeFunc = nil

	cred, _ := credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Test Cred",
		Data:       map[string]any{"api_key": "test-key"},
	})

	req := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := svc.Complete(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}

	updated, _ := credSvc.Get(cred.ID)
	if updated.RequestCount != 1 {
		t.Errorf("request count: got %d, want 1", updated.RequestCount)
	}
	if updated.SuccessCount != 1 {
		t.Errorf("success count: got %d, want 1", updated.SuccessCount)
	}
}

func TestRouterService_Complete_UpdatesUsageOnFailure(t *testing.T) {
	svc, credSvc, mock := setupRouterService(t, 3)

	cred, _ := credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Test Cred",
		Data:       map[string]any{"api_key": "test-key"},
	})

	mock.completeFunc = func(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		return nil, &models.ProviderError{
			StatusCode: 400,
			Message:    "bad request",
			Type:       models.ErrorTypeUpstream,
		}
	}

	req := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := svc.Complete(context.Background(), req, nil)
	if err == nil {
		t.Error("expected error, got nil")
	}

	updated, _ := credSvc.Get(cred.ID)
	if updated.RequestCount != 1 {
		t.Errorf("request count: got %d, want 1", updated.RequestCount)
	}
	if updated.FailureCount != 1 {
		t.Errorf("failure count: got %d, want 1", updated.FailureCount)
	}
}
