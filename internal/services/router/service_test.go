package router

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func setupRouterService(t *testing.T) (*Service, *credential.Service, *modelinfo.Service, *testutil.MockAdapter) {
	t.Helper()
	database := testutil.SetupTestDB(t)

	providerSvc := provider.NewService(database)
	mock := testutil.NewMockAdapter("mock").WithCompleteFunc(func(ctx context.Context, creds []*models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		return &models.ChatCompletionResponse{
			ID:      "test-response",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   string(req.Model),
			Choices: []models.ChatCompletionChoice{{Index: 0, Message: models.ChatMessage{Role: "assistant", Content: "test"}, FinishReason: "stop"}},
		}, nil
	})
	providerSvc.RegisterGoAdapter(mock)
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)

	routerSvc := New(providerSvc, credSvc, modelInfoSvc, exhausted.New(database), slog.Default())

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

	if mock.Calls != 1 {
		t.Errorf("expected 1 backend call, got %d", mock.Calls)
	}
	if len(mock.SeenPools) != 1 || len(mock.SeenPools[0]) != 2 {
		t.Errorf("expected one call with a 2-credential pool, got %v", mock.SeenPools)
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

	mock.WithCompleteFunc(func(ctx context.Context, creds []*models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		resetAt := time.Now().Add(60 * time.Second)
		return nil, &models.ProviderError{
			StatusCode: 429,
			Message:    "rate limit exceeded",
			Type:       models.ErrorTypeRateLimit,
			RetryAfter: &resetAt,
		}
	})

	req := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := svc.Complete(context.Background(), req, nil)
	if err == nil {
		t.Error("expected backend error, got nil")
	}

	if mock.Calls != 1 {
		t.Errorf("expected 1 backend call (no repeat passes), got %d", mock.Calls)
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

	mock.WithCompleteFunc(func(ctx context.Context, creds []*models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		return nil, &models.ProviderError{
			StatusCode: 401,
			Message:    "authentication failed",
			Type:       models.ErrorTypeAuth,
		}
	})

	req := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := svc.Complete(context.Background(), req, nil)
	if err == nil {
		t.Error("expected error for auth failure, got nil")
	}

	if mock.Calls != 1 {
		t.Errorf("expected 1 call, got %d", mock.Calls)
	}
}

func TestRouterService_TestModel_MarksQuotaExceeded(t *testing.T) {
	svc, credSvc, _, mock := setupRouterService(t)

	credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})
	mock.WithCompleteFunc(func(ctx context.Context, creds []*models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		return nil, &models.ProviderError{
			StatusCode: 429,
			Message:    "quota exhausted",
			Type:       models.ErrorTypeQuotaExceeded,
		}
	})

	res := svc.TestModel(context.Background(), "mock/test-model")
	if res.OK {
		t.Fatal("expected failed probe, got ok")
	}
	if !res.QuotaExceeded {
		t.Error("quota failure must set quota_exceeded so callers never disable over it")
	}
	if res.Code != "quota_exceeded" {
		t.Errorf("probe must carry machine-readable code, got %q", res.Code)
	}
}
