package router_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	virtualadapter "github.com/TheSlopMachine/llm-router/providers/virtual"
)

// setupVirtualStack wires a full router where member requests resolve through
// a mock provider while virtual/* resolves through the real virtual adapter.
func setupVirtualStack(t *testing.T) (*router.Service, *virtual.GormService, *credential.GormService) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()

	providerService := provider.NewGorm(database)
	providerService.RegisterGoAdapter(testutil.NewMockAdapter("mock"))
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Virtual models", TypeKey: provider.TypeVirtual}); err != nil {
		t.Fatalf("create virtual provider: %v", err)
	}

	credentialService := credential.NewGorm(database, providerService)
	if _, err := credentialService.Add(ctx, credential.AddOptions{
		ProviderID: "mock",
		Label:      "member cred",
		Data:       map[string]any{"api_key": "test-key"},
	}); err != nil {
		t.Fatalf("add member credential: %v", err)
	}

	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)
	virtualService := virtual.NewGorm(database, providerService, modelInfoService)
	routerService := router.New(providerService, credentialService, modelInfoService, videojobs.New(database), nil, nil, slog.Default())

	virtualAdapter := virtualadapter.New(routerService, virtualService, slog.Default())
	providerService.RegisterGoAdapter(virtualAdapter)

	virtualModel := &models.VirtualModel{
		Name:   "E2E",
		Models: []models.VirtualModelEntry{{ModelID: "mock/test-model"}},
	}
	if err := virtualService.Create(ctx, virtualModel); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}
	if virtualModel.ID != "e2e" {
		t.Fatalf("virtual model id: got %q, want %q", virtualModel.ID, "e2e")
	}

	return routerService, virtualService, credentialService
}

func virtualRequest(model string) *models.ChatCompletionRequest {
	return &models.ChatCompletionRequest{
		Model:    models.ModelId(model),
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}
}

func TestRouterVirtualResolvesByNameWithoutCredentials(t *testing.T) {
	routerService, _, credentialService := setupVirtualStack(t)

	rows, err := credentialService.ListByProvider(context.Background(), provider.TypeVirtual)
	if err != nil {
		t.Fatalf("list virtual credentials: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected zero virtual credentials, got %d", len(rows))
	}

	response, err := routerService.Complete(context.Background(), virtualRequest("virtual/e2e"), nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if string(response.Model) != "mock/test-model" {
		t.Errorf("response model: got %q, want %q", response.Model, "mock/test-model")
	}
}

func TestRouterVirtualStreamWithoutCredentials(t *testing.T) {
	routerService, _, _ := setupVirtualStack(t)

	if err := routerService.CompleteStream(context.Background(), virtualRequest("virtual/e2e"), io.Discard, nil); err != nil {
		t.Fatalf("stream failed: %v", err)
	}
}

func TestRouterVirtualFallsThroughToSecondModel(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()

	providerService := provider.NewGorm(database)
	mock := testutil.NewMockAdapter("mock").WithCompleteFunc(
		func(ctx context.Context, creds []*models.Credential, request *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
			if request.Model == "mock/bad-model" {
				return nil, &models.ProviderError{StatusCode: 502, Code: "server_error", Message: "overloaded"}
			}
			return &models.ChatCompletionResponse{
				ID:      "second-ok",
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   string(request.Model),
				Choices: []models.ChatCompletionChoice{{Index: 0, Message: models.ChatMessage{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
			}, nil
		})
	providerService.RegisterGoAdapter(mock)
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Virtual models", TypeKey: provider.TypeVirtual}); err != nil {
		t.Fatalf("create virtual provider: %v", err)
	}

	credentialService := credential.NewGorm(database, providerService)
	if _, err := credentialService.Add(ctx, credential.AddOptions{
		ProviderID: "mock",
		Label:      "member cred",
		Data:       map[string]any{"api_key": "test-key"},
	}); err != nil {
		t.Fatalf("add member credential: %v", err)
	}

	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)
	virtualService := virtual.NewGorm(database, providerService, modelInfoService)
	routerService := router.New(providerService, credentialService, modelInfoService, videojobs.New(database), nil, nil, slog.Default())

	virtualAdapter := virtualadapter.New(routerService, virtualService, slog.Default())
	providerService.RegisterGoAdapter(virtualAdapter)

	virtualModel := &models.VirtualModel{
		Name: "Fallthrough",
		Models: []models.VirtualModelEntry{
			{ModelID: "mock/bad-model"},
			{ModelID: "mock/good-model"},
		},
	}
	if err := virtualService.Create(ctx, virtualModel); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	response, err := routerService.Complete(context.Background(), virtualRequest("virtual/fallthrough"), nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if string(response.Model) != "mock/good-model" {
		t.Errorf("response model: got %q, want %q", response.Model, "mock/good-model")
	}
}

func TestRouterVirtualUnknownAgentIsInvalidRequest(t *testing.T) {
	routerService, _, _ := setupVirtualStack(t)

	_, err := routerService.Complete(context.Background(), virtualRequest("virtual/nope"), nil)
	if err == nil {
		t.Fatal("expected error for unknown virtual model, got nil")
	}
	var provErr *models.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected ProviderError, got %T (%v)", err, err)
	}
	if provErr.Code != "invalid_request_error" {
		t.Errorf("error code: got %q, want invalid_request_error", provErr.Code)
	}
	if provErr.StatusCode != 400 {
		t.Errorf("status: got %d, want 400", provErr.StatusCode)
	}
	if !strings.Contains(provErr.Message, "nope") {
		t.Errorf("message must name the virtual model: got %q", provErr.Message)
	}

	if err := routerService.CompleteStream(context.Background(), virtualRequest("virtual/nope"), io.Discard, nil); err == nil {
		t.Fatal("expected stream error for unknown virtual model, got nil")
	}
}

func TestRouterVirtualDisabledIsModelDisabled(t *testing.T) {
	routerService, virtualService, _ := setupVirtualStack(t)

	virtualModel, err := virtualService.Get(context.Background(), "e2e")
	if err != nil {
		t.Fatalf("get virtual model: %v", err)
	}
	virtualModel.Disabled = true
	if err := virtualService.Update(context.Background(), virtualModel.ID, virtualModel); err != nil {
		t.Fatalf("disable virtual model: %v", err)
	}

	_, err = routerService.Complete(context.Background(), virtualRequest("virtual/e2e"), nil)
	if !errors.Is(err, apierrors.ErrModelDisabled) {
		t.Fatalf("expected ErrModelDisabled, got %v", err)
	}
}
