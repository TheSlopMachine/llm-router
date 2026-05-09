// Package agents implements a virtual provider adapter that orchestrates
// requests across multiple real providers with custom instructions and
// optional decision-based routing.
package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"

	sdk "github.com/TheSlopMachine/llm-router-sdk"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/agent"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
)

func init() {
	sdk.Register(&Adapter{})
}

// Adapter implements the provider.Adapter interface for virtual agents.
type Adapter struct {
	routerSvc *router.Service
	agentSvc  *agent.Service
	logger    *slog.Logger
	mu        sync.RWMutex
}

// SetRouterService injects the router service dependency.
func (a *Adapter) SetRouterService(svc *router.Service) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.routerSvc = svc
}

// SetAgentService injects the agent service dependency.
func (a *Adapter) SetAgentService(svc *agent.Service) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.agentSvc = svc
}

// SetLogger injects the logger dependency.
func (a *Adapter) SetLogger(logger *slog.Logger) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logger = logger
}

func (a *Adapter) getRouterService() *router.Service {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.routerSvc
}

func (a *Adapter) getAgentService() *agent.Service {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.agentSvc
}

func (a *Adapter) getLogger() *slog.Logger {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.logger == nil {
		return slog.Default()
	}
	return a.logger
}

// ─────────────────────────────────────────────
// Adapter Interface Implementation
// ─────────────────────────────────────────────

func (a *Adapter) TypeKey() string {
	return "agents"
}

func (a *Adapter) AuthType() sdk.AuthType {
	return sdk.AuthTypeAPIKey
}

func (a *Adapter) ValidateCredentials(data map[string]string) error {
	agentID := data["agent_id"]
	if agentID == "" {
		return fmt.Errorf("agent_id is required")
	}

	agentSvc := a.getAgentService()
	if agentSvc == nil {
		return fmt.Errorf("agent service not initialized")
	}

	_, err := agentSvc.Get(agentID)
	if err != nil {
		return fmt.Errorf("agent not found: %w", err)
	}

	return nil
}

func (a *Adapter) Complete(
	ctx context.Context,
	cred *sdk.Credential,
	req *sdk.ChatCompletionRequest,
) (*sdk.ChatCompletionResponse, error) {
	routerSvc := a.getRouterService()
	if routerSvc == nil {
		return nil, fmt.Errorf("router service not initialized")
	}

	agentSvc := a.getAgentService()
	if agentSvc == nil {
		return nil, fmt.Errorf("agent service not initialized")
	}

	logger := a.getLogger()

	// Get agent configuration
	agentID := cred.Data["agent_id"]
	agent, err := agentSvc.Get(agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}

	// Inject general instructions
	modifiedReq := *req
	modifiedReq.Messages = injectInstructions(req.Messages, agent.Instructions)

	// Decision model routing (if configured)
	var selectedModel *models.AgentModel
	if agent.DecisionModel != nil {
		selectedModel = a.routeWithDecisionModel(ctx, agent, &modifiedReq)
		if selectedModel != nil {
			logger.Info("decision model selected model",
				"agent", agent.Name,
				"selected", selectedModel.ModelID,
				"priority", selectedModel.Priority)
		}
	}

	// Reorder models: selected first, then by priority
	orderedModels := reorderModels(agent.Models, selectedModel)

	// Try each model in order
	for _, agentModel := range orderedModels {
		modelReq := modifiedReq

		// Apply model-specific instructions
		if agentModel.Instructions != "" {
			modelReq.Messages = injectModelInstructions(modelReq.Messages, agentModel.Instructions)
		}

		// Set target model
		modelReq.Model = agentModel.ModelID

		logger.Info("agent trying model",
			"agent", agent.Name,
			"model", agentModel.ModelID,
			"priority", agentModel.Priority)

		// Make internal request
		resp, err := routerSvc.Complete(ctx, &modelReq)

		if err == nil {
			logger.Info("agent request succeeded",
				"agent", agent.Name,
				"model", agentModel.ModelID)
			return resp, nil
		}

		// Check if retryable
		var provErr *provider.ProviderError
		if errors.As(err, &provErr) && provErr.IsRetryable() {
			logger.Info("agent model failed with retryable error, trying next",
				"agent", agent.Name,
				"model", agentModel.ModelID,
				"error", provErr.Message)
			continue
		}

		// Non-retryable error, fail immediately
		logger.Error("agent model failed with non-retryable error",
			"agent", agent.Name,
			"model", agentModel.ModelID,
			"error", err)
		return nil, err
	}

	return nil, fmt.Errorf("all agent models exhausted for agent %q", agent.Name)
}

func (a *Adapter) CompleteStream(
	ctx context.Context,
	cred *sdk.Credential,
	req *sdk.ChatCompletionRequest,
	w io.Writer,
) error {
	routerSvc := a.getRouterService()
	if routerSvc == nil {
		return fmt.Errorf("router service not initialized")
	}

	agentSvc := a.getAgentService()
	if agentSvc == nil {
		return fmt.Errorf("agent service not initialized")
	}

	logger := a.getLogger()

	// Get agent configuration
	agentID := cred.Data["agent_id"]
	agent, err := agentSvc.Get(agentID)
	if err != nil {
		return fmt.Errorf("get agent: %w", err)
	}

	// Inject general instructions
	modifiedReq := *req
	modifiedReq.Messages = injectInstructions(req.Messages, agent.Instructions)

	// Decision model routing (if configured)
	var selectedModel *models.AgentModel
	if agent.DecisionModel != nil {
		selectedModel = a.routeWithDecisionModel(ctx, agent, &modifiedReq)
		if selectedModel != nil {
			logger.Info("decision model selected model (stream)",
				"agent", agent.Name,
				"selected", selectedModel.ModelID,
				"priority", selectedModel.Priority)
		}
	}

	// Reorder models: selected first, then by priority
	orderedModels := reorderModels(agent.Models, selectedModel)

	// Try each model in order
	for _, agentModel := range orderedModels {
		modelReq := modifiedReq

		// Apply model-specific instructions
		if agentModel.Instructions != "" {
			modelReq.Messages = injectModelInstructions(modelReq.Messages, agentModel.Instructions)
		}

		// Set target model
		modelReq.Model = agentModel.ModelID

		logger.Info("agent trying model (stream)",
			"agent", agent.Name,
			"model", agentModel.ModelID,
			"priority", agentModel.Priority)

		// Make internal request
		err := routerSvc.CompleteStream(ctx, &modelReq, w)

		if err == nil {
			logger.Info("agent stream succeeded",
				"agent", agent.Name,
				"model", agentModel.ModelID)
			return nil
		}

		// Check if retryable
		var provErr *provider.ProviderError
		if errors.As(err, &provErr) && provErr.IsRetryable() {
			logger.Info("agent model failed with retryable error, trying next (stream)",
				"agent", agent.Name,
				"model", agentModel.ModelID,
				"error", provErr.Message)
			continue
		}

		// Non-retryable error, fail immediately
		logger.Error("agent model failed with non-retryable error (stream)",
			"agent", agent.Name,
			"model", agentModel.ModelID,
			"error", err)
		return err
	}

	return fmt.Errorf("all agent models exhausted for agent %q", agent.Name)
}

func (a *Adapter) NeedsRefresh(cred *sdk.Credential) bool {
	return false
}

func (a *Adapter) RefreshCredential(ctx context.Context, cred *sdk.Credential) (*sdk.Credential, error) {
	return nil, sdk.ErrNoRefreshNeeded
}

func (a *Adapter) GetAuthFlow() sdk.AuthFlowHandler {
	return nil
}

func (a *Adapter) GetModelInfos(ctx context.Context, cred *sdk.Credential, providerQualifier string) ([]sdk.ModelInfo, error) {
	agentSvc := a.getAgentService()
	if agentSvc == nil {
		return nil, fmt.Errorf("agent service not initialized")
	}

	// List all agents and return as models
	agents, err := agentSvc.List()
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}

	infos := make([]sdk.ModelInfo, len(agents))
	for i, agent := range agents {
		infos[i] = sdk.ModelInfo{
			Name:        agent.Name,
			DisplayName: agent.Name,
		}
	}

	return infos, nil
}

func (a *Adapter) GetDefaultProviders() []sdk.ProviderInfo {
	return []sdk.ProviderInfo{
		{
			Name:      "Agents",
			Qualifier: "",
			BaseURL:   "",
			IconURL:   "",
		},
	}
}

// ─────────────────────────────────────────────
// Helper Functions
// ─────────────────────────────────────────────

type agentModelList []models.AgentModel

func reorderModels(models agentModelList, selected *models.AgentModel) agentModelList {
	// Sort by priority
	sorted := make(agentModelList, len(models))
	copy(sorted, models)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	// If a model was selected by decision model, move it to front
	if selected != nil {
		result := agentModelList{*selected}
		for _, m := range sorted {
			if m.ModelID != selected.ModelID {
				result = append(result, m)
			}
		}
		return result
	}

	return sorted
}

