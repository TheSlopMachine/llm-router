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
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	virtualadapter "github.com/TheSlopMachine/llm-router/providers/virtual"
)

// setupVirtualStack wires a full router where member requests resolve through
// a mock provider while virtual/* resolves through the real virtual adapter.
func setupVirtualStack(t *testing.T) (*router.Service, *virtual.Service, *credential.Service) {
	t.Helper()
	database := testutil.SetupTestDB(t)

	providerSvc := provider.NewService(database)
	providerSvc.RegisterGoAdapter(testutil.NewMockAdapter("mock"))
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Virtual models", TypeKey: provider.TypeVirtual}); err != nil {
		t.Fatalf("create virtual provider: %v", err)
	}

	credSvc := credential.New(database, providerSvc)
	if _, err := credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "member cred",
		Data:       map[string]any{"api_key": "test-key"},
	}); err != nil {
		t.Fatalf("add member credential: %v", err)
	}

	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)
	virtualSvc := virtual.New(database, providerSvc, modelInfoSvc)
	routerSvc := router.New(providerSvc, credSvc, modelInfoSvc, slog.Default())

	virtualAdapter := virtualadapter.New(routerSvc, virtualSvc, slog.Default())
	providerSvc.RegisterGoAdapter(virtualAdapter)

	vm := &models.VirtualModel{
		Name:   "E2E",
		Models: []models.VirtualModelEntry{{ModelID: "mock/test-model"}},
	}
	if err := virtualSvc.Create(vm); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}
	if vm.ID != "e2e" {
		t.Fatalf("virtual model id: got %q, want %q", vm.ID, "e2e")
	}

	return routerSvc, virtualSvc, credSvc
}

func virtualRequest(model string) *models.ChatCompletionRequest {
	return &models.ChatCompletionRequest{
		Model:    models.ModelId(model),
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}
}

func TestRouterVirtualResolvesByNameWithoutCredentials(t *testing.T) {
	routerSvc, _, credSvc := setupVirtualStack(t)

	rows, err := credSvc.ListByProvider(provider.TypeVirtual)
	if err != nil {
		t.Fatalf("list virtual credentials: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected zero virtual credentials, got %d", len(rows))
	}

	resp, err := routerSvc.Complete(context.Background(), virtualRequest("virtual/e2e"), nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if string(resp.Model) != "mock/test-model" {
		t.Errorf("response model: got %q, want %q", resp.Model, "mock/test-model")
	}
}

func TestRouterVirtualStreamWithoutCredentials(t *testing.T) {
	routerSvc, _, _ := setupVirtualStack(t)

	if err := routerSvc.CompleteStream(context.Background(), virtualRequest("virtual/e2e"), io.Discard, nil); err != nil {
		t.Fatalf("stream failed: %v", err)
	}
}

func TestRouterVirtualFallsThroughToSecondModel(t *testing.T) {
	database := testutil.SetupTestDB(t)

	providerSvc := provider.NewService(database)
	mock := testutil.NewMockAdapter("mock").WithCompleteFunc(
		func(ctx context.Context, creds []*models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
			if req.Model == "mock/bad-model" {
				return nil, &models.ProviderError{StatusCode: 502, Type: models.ErrorTypeUpstream, Message: "overloaded"}
			}
			return &models.ChatCompletionResponse{
				ID:      "second-ok",
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   string(req.Model),
				Choices: []models.ChatCompletionChoice{{Index: 0, Message: models.ChatMessage{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
			}, nil
		})
	providerSvc.RegisterGoAdapter(mock)
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Virtual models", TypeKey: provider.TypeVirtual}); err != nil {
		t.Fatalf("create virtual provider: %v", err)
	}

	credSvc := credential.New(database, providerSvc)
	if _, err := credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "member cred",
		Data:       map[string]any{"api_key": "test-key"},
	}); err != nil {
		t.Fatalf("add member credential: %v", err)
	}

	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)
	virtualSvc := virtual.New(database, providerSvc, modelInfoSvc)
	routerSvc := router.New(providerSvc, credSvc, modelInfoSvc, slog.Default())

	virtualAdapter := virtualadapter.New(routerSvc, virtualSvc, slog.Default())
	providerSvc.RegisterGoAdapter(virtualAdapter)

	vm := &models.VirtualModel{
		Name: "Fallthrough",
		Models: []models.VirtualModelEntry{
			{ModelID: "mock/bad-model"},
			{ModelID: "mock/good-model"},
		},
	}
	if err := virtualSvc.Create(vm); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	resp, err := routerSvc.Complete(context.Background(), virtualRequest("virtual/fallthrough"), nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if string(resp.Model) != "mock/good-model" {
		t.Errorf("response model: got %q, want %q", resp.Model, "mock/good-model")
	}
}

func TestRouterVirtualUnknownAgentIsInvalidRequest(t *testing.T) {
	routerSvc, _, _ := setupVirtualStack(t)

	_, err := routerSvc.Complete(context.Background(), virtualRequest("virtual/nope"), nil)
	if err == nil {
		t.Fatal("expected error for unknown virtual model, got nil")
	}
	var provErr *models.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected ProviderError, got %T (%v)", err, err)
	}
	if provErr.Type != models.ErrorTypeInvalidRequest {
		t.Errorf("error type: got %v, want invalid_request", provErr.Type)
	}
	if provErr.StatusCode != 400 {
		t.Errorf("status: got %d, want 400", provErr.StatusCode)
	}
	if !strings.Contains(provErr.Message, "nope") {
		t.Errorf("message must name the virtual model: got %q", provErr.Message)
	}

	if err := routerSvc.CompleteStream(context.Background(), virtualRequest("virtual/nope"), io.Discard, nil); err == nil {
		t.Fatal("expected stream error for unknown virtual model, got nil")
	}
}

func TestRouterVirtualDisabledIsModelDisabled(t *testing.T) {
	routerSvc, virtualSvc, _ := setupVirtualStack(t)

	vm, err := virtualSvc.Get("e2e")
	if err != nil {
		t.Fatalf("get virtual model: %v", err)
	}
	vm.Disabled = true
	if err := virtualSvc.Update(vm.ID, vm); err != nil {
		t.Fatalf("disable virtual model: %v", err)
	}

	_, err = routerSvc.Complete(context.Background(), virtualRequest("virtual/e2e"), nil)
	if !errors.Is(err, apierrors.ErrModelDisabled) {
		t.Fatalf("expected ErrModelDisabled, got %v", err)
	}
}
