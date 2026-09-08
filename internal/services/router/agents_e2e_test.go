package router_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/agent"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	agentsadapter "github.com/TheSlopMachine/llm-router/providers/agents"
)

// setupAgentsStack wires a full router where member requests resolve through
// a mock provider while agents/* resolves through the real agents adapter.
func setupAgentsStack(t *testing.T) (*router.Service, *agent.Service, *credential.Service) {
	t.Helper()
	database := testutil.SetupTestDB(t)

	providerSvc := provider.NewService(database)
	providerSvc.RegisterGoAdapter(testutil.NewMockAdapter("mock"))
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Agents", TypeKey: "agents"}); err != nil {
		t.Fatalf("create agents provider: %v", err)
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
	agentSvc := agent.New(database, providerSvc, modelInfoSvc)
	routerSvc := router.New(providerSvc, credSvc, modelInfoSvc, 3, slog.Default())

	agentsAdapter := &agentsadapter.Adapter{}
	agentsAdapter.SetRouterService(routerSvc)
	agentsAdapter.SetAgentService(agentSvc)
	agentsAdapter.SetLogger(slog.Default())
	providerSvc.RegisterGoAdapter(agentsAdapter)

	a := &models.Agent{
		Name:    "E2E",
		IsDraft: true,
		Models: []models.AgentModel{
			{ModelID: "mock/test-model", Priority: 1},
		},
	}
	if err := agentSvc.Create(a); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if a.ID != "e2e" {
		t.Fatalf("agent id: got %q, want %q", a.ID, "e2e")
	}

	return routerSvc, agentSvc, credSvc
}

func agentsRequest(model string) *models.ChatCompletionRequest {
	return &models.ChatCompletionRequest{
		Model:    models.ModelId(model),
		Messages: []models.ChatMessage{{Role: "user", Content: "test"}},
	}
}

func TestRouterAgentsResolvesByNameWithoutCredentials(t *testing.T) {
	routerSvc, _, credSvc := setupAgentsStack(t)

	rows, err := credSvc.ListByProvider("agents")
	if err != nil {
		t.Fatalf("list agents credentials: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected zero agents credentials, got %d", len(rows))
	}

	resp, err := routerSvc.Complete(context.Background(), agentsRequest("agents/e2e"), nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if string(resp.Model) != "mock/test-model" {
		t.Errorf("response model: got %q, want %q", resp.Model, "mock/test-model")
	}
}

func TestRouterAgentsStreamWithoutCredentials(t *testing.T) {
	routerSvc, _, _ := setupAgentsStack(t)

	if err := routerSvc.CompleteStream(context.Background(), agentsRequest("agents/e2e"), io.Discard, nil); err != nil {
		t.Fatalf("stream failed: %v", err)
	}
}

func TestRouterAgentsUnknownAgentIsInvalidRequest(t *testing.T) {
	routerSvc, _, _ := setupAgentsStack(t)

	_, err := routerSvc.Complete(context.Background(), agentsRequest("agents/nope"), nil)
	if err == nil {
		t.Fatal("expected error for unknown agent, got nil")
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
		t.Errorf("message must name the agent: got %q", provErr.Message)
	}

	if err := routerSvc.CompleteStream(context.Background(), agentsRequest("agents/nope"), io.Discard, nil); err == nil {
		t.Fatal("expected stream error for unknown agent, got nil")
	}
}
