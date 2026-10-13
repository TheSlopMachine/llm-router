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
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func setupRouterService(t *testing.T) (*Service, *credential.GormService, *modelinfo.GormService, *testutil.MockAdapter) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()

	providerService := provider.NewGorm(database)
	mock := testutil.NewMockAdapter("mock").WithCompleteFunc(func(ctx context.Context, creds []*models.Credential, request *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		return &models.ChatCompletionResponse{
			ID:      "test-response",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   string(request.Model),
			Choices: []models.ChatCompletionChoice{{Index: 0, Message: models.ChatMessage{Role: "assistant", Content: "test"}, FinishReason: "stop"}},
		}, nil
	})
	providerService.RegisterGoAdapter(mock)
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credentialService := credential.NewGorm(database, providerService)
	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)

	routerService := New(providerService, credentialService, modelInfoService, videojobs.New(database), nil, nil, slog.Default())

	return routerService, credentialService, modelInfoService, mock
}

// ─────────────────────────────────────────────
// Basic Routing Tests
// ─────────────────────────────────────────────

func TestRouterService_Complete_Success(t *testing.T) {
	service, credentialService, _, _ := setupRouterService(t)

	credentialService.Add(context.Background(), credential.AddOptions{
		ProviderID: "mock",
		Label:      "Test Cred",
		Data:       map[string]any{"api_key": "test-key"},
	})

	request := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	response, err := service.Complete(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}

	if response.ID != "test-response" {
		t.Errorf("response ID: got %q, want %q", response.ID, "test-response")
	}
}

func TestRouterService_Complete_InvalidModelId(t *testing.T) {
	service, _, _, _ := setupRouterService(t)

	request := &models.ChatCompletionRequest{
		Model:    "invalid-model-id",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := service.Complete(context.Background(), request, nil)
	if err == nil {
		t.Error("expected error for invalid model ID, got nil")
	}
}

func TestRouterService_Complete_ProviderNotFound(t *testing.T) {
	service, _, _, _ := setupRouterService(t)

	request := &models.ChatCompletionRequest{
		Model:    "nonexistent/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := service.Complete(context.Background(), request, nil)
	if err == nil {
		t.Error("expected error for nonexistent provider, got nil")
	}
}

func TestRouterService_Complete_NoCredentials(t *testing.T) {
	service, _, _, _ := setupRouterService(t)

	request := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := service.Complete(context.Background(), request, nil)
	if err == nil {
		t.Error("expected error when no credentials available, got nil")
	}
}

// ─────────────────────────────────────────────
// Single-pass Backend Tests: the router calls the backend once with the
// whole pool; key iteration lives inside the backend.
// ─────────────────────────────────────────────

func TestRouterService_Complete_PassesFullPoolInSingleCall(t *testing.T) {
	service, credentialService, _, mock := setupRouterService(t)

	credentialService.Add(context.Background(), credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})
	credentialService.Add(context.Background(), credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 2",
		Data:       map[string]any{"api_key": "key2"},
	})

	request := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	response, err := service.Complete(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if response.ID != "test-response" {
		t.Errorf("expected test response, got %q", response.ID)
	}

	if mock.Calls != 1 {
		t.Errorf("expected 1 backend call, got %d", mock.Calls)
	}
	if len(mock.SeenPools) != 1 || len(mock.SeenPools[0]) != 2 {
		t.Errorf("expected one call with a 2-credential pool, got %v", mock.SeenPools)
	}
}

func TestRouterService_Complete_BackendErrorSurfacesWithoutRepeat(t *testing.T) {
	service, credentialService, _, mock := setupRouterService(t)

	credentialService.Add(context.Background(), credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})
	credentialService.Add(context.Background(), credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 2",
		Data:       map[string]any{"api_key": "key2"},
	})

	mock.WithCompleteFunc(func(ctx context.Context, creds []*models.Credential, request *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		return nil, &models.ProviderError{
			StatusCode: 429,
			Message:    "rate limit exceeded",
			Code:       "rate_limit",
		}
	})

	request := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := service.Complete(context.Background(), request, nil)
	if err == nil {
		t.Error("expected backend error, got nil")
	}

	if mock.Calls != 1 {
		t.Errorf("expected 1 backend call (no repeat passes), got %d", mock.Calls)
	}
}

func TestRouterService_TestModel_UnsupportedOnGoBackend(t *testing.T) {
	service, credentialService, _, _ := setupRouterService(t)

	credentialService.Add(context.Background(), credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})

	result := service.TestModel(context.Background(), "mock/test-model", "chat")
	if result.OK || result.Code != "unsupported" {
		t.Fatalf("Go backends must report unsupported: %+v", result)
	}
}

func TestRouterService_TestModel_BypassesManualDisable(t *testing.T) {
	service, credentialService, modelInfoService, _ := setupRouterService(t)

	credentialService.Add(context.Background(), credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})
	if err := modelInfoService.SetOverride(context.Background(), models.ModelOverride{
		ProviderID: "mock",
		Name:       "test-model",
		Disabled:   true,
	}); err != nil {
		t.Fatalf("disable model: %v", err)
	}

	request := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}
	if _, err := service.Complete(context.Background(), request, nil); !errors.Is(err, apierrors.ErrModelDisabled) {
		t.Fatalf("expected ErrModelDisabled from routing, got %v", err)
	}

	result := service.TestModel(context.Background(), "mock/test-model", "chat")
	if result.Code == "model_disabled" {
		t.Fatalf("probe must bypass the manual disable gate, got %+v", result)
	}
	if result.Code != "unsupported" {
		t.Fatalf("Go backends must report unsupported: %+v", result)
	}
}

func TestRouterService_Complete_AuthErrorSingleAttempt(t *testing.T) {
	service, credentialService, _, mock := setupRouterService(t)

	credentialService.Add(context.Background(), credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})

	mock.WithCompleteFunc(func(ctx context.Context, creds []*models.Credential, request *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		return nil, &models.ProviderError{
			StatusCode: 401,
			Message:    "authentication failed",
			Code:       "authentication_error",
		}
	})

	request := &models.ChatCompletionRequest{
		Model:    "mock/test-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}

	_, err := service.Complete(context.Background(), request, nil)
	if err == nil {
		t.Error("expected error for auth failure, got nil")
	}

	if mock.Calls != 1 {
		t.Errorf("expected 1 call, got %d", mock.Calls)
	}
}

func TestRouterService_TestModel_UnsupportedKeepsQuotaClear(t *testing.T) {
	service, credentialService, _, _ := setupRouterService(t)

	credentialService.Add(context.Background(), credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "key1"},
	})

	result := service.TestModel(context.Background(), "mock/test-model", "chat")
	if result.OK || result.Code != "unsupported" {
		t.Fatalf("Go backends must report unsupported: %+v", result)
	}
	if result.QuotaExceeded {
		t.Error("unsupported must not set quota_exceeded")
	}
}
