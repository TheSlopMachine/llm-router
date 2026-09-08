// Package agents implements a virtual provider backend that orchestrates
// requests across multiple real providers with custom instructions and
// optional decision-based routing.
package agents

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/agent"
	"github.com/TheSlopMachine/llm-router/internal/services/retry"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
)

// Adapter implements the provider.GoAdapter interface for virtual agents.
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
// GoAdapter Implementation
// ─────────────────────────────────────────────

func (a *Adapter) TypeKey() string {
	return "agents"
}

func (a *Adapter) ValidateCredentials(data map[string]any) error {
	agentID, _ := data["agent_id"].(string)
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
	cred *models.Credential,
	req *models.ChatCompletionRequest,
	_ map[string]any,
) (*models.ChatCompletionResponse, error) {
	routerSvc := a.getRouterService()
	if routerSvc == nil {
		return nil, fmt.Errorf("router service not initialized")
	}

	agentSvc := a.getAgentService()
	if agentSvc == nil {
		return nil, fmt.Errorf("agent service not initialized")
	}

	logger := a.getLogger()

	// The agent resolves from the model name suffix (agents/<agent-id>).
	// Credentials carry no agent binding.
	_, agentID, err := req.Model.Parse()
	if err != nil {
		return nil, &models.ProviderError{StatusCode: 400, Type: models.ErrorTypeInvalidRequest, Message: fmt.Sprintf("invalid model id: %s", err)}
	}
	if agentID == "" {
		return nil, &models.ProviderError{StatusCode: 400, Type: models.ErrorTypeInvalidRequest, Message: "agent id is required"}
	}
	agent, err := agentSvc.Get(agentID)
	if err != nil {
		return nil, &models.ProviderError{StatusCode: 400, Type: models.ErrorTypeInvalidRequest, Message: fmt.Sprintf("agent %q not found", agentID)}
	}

	modifiedReq := *req
	modifiedReq.Messages = injectInstructions(req.Messages, agent.Instructions)

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

	orderedModels := reorderModels(agent.Models, selectedModel)

	candidates := make([]retry.Candidate[*models.ChatCompletionResponse], 0, len(orderedModels))
	for _, agentModel := range orderedModels {
		agentModel := agentModel
		candidates = append(candidates, retry.Candidate[*models.ChatCompletionResponse]{
			Label: string(agentModel.ModelID),
			Run: func(ctx context.Context) (*models.ChatCompletionResponse, error) {
				modelReq := modifiedReq
				if agentModel.Instructions != "" {
					modelReq.Messages = injectModelInstructions(modelReq.Messages, agentModel.Instructions)
				}
				modelReq.Model = agentModel.ModelID
				logger.Info("agent trying model",
					"agent", agent.Name,
					"model", agentModel.ModelID,
					"priority", agentModel.Priority)
				resp, err := routerSvc.Complete(ctx, &modelReq, nil)
				if err == nil {
					logger.Info("agent request succeeded",
						"agent", agent.Name,
						"model", agentModel.ModelID)
				}
				return resp, err
			},
		})
	}

	return retry.Run(ctx, candidates, logger)
}

func (a *Adapter) CompleteStream(
	ctx context.Context,
	cred *models.Credential,
	req *models.ChatCompletionRequest,
	w io.Writer,
	_ map[string]any,
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

	_, agentID, err := req.Model.Parse()
	if err != nil {
		return &models.ProviderError{StatusCode: 400, Type: models.ErrorTypeInvalidRequest, Message: fmt.Sprintf("invalid model id: %s", err)}
	}
	if agentID == "" {
		return &models.ProviderError{StatusCode: 400, Type: models.ErrorTypeInvalidRequest, Message: "agent id is required"}
	}
	agent, err := agentSvc.Get(agentID)
	if err != nil {
		return &models.ProviderError{StatusCode: 400, Type: models.ErrorTypeInvalidRequest, Message: fmt.Sprintf("agent %q not found", agentID)}
	}

	modifiedReq := *req
	modifiedReq.Messages = injectInstructions(req.Messages, agent.Instructions)

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

	orderedModels := reorderModels(agent.Models, selectedModel)

	candidates := make([]retry.StreamCandidate, 0, len(orderedModels))
	for _, agentModel := range orderedModels {
		agentModel := agentModel
		candidates = append(candidates, retry.StreamCandidate{
			Label: string(agentModel.ModelID),
			Run: func(ctx context.Context, w io.Writer) error {
				modelReq := modifiedReq
				if agentModel.Instructions != "" {
					modelReq.Messages = injectModelInstructions(modelReq.Messages, agentModel.Instructions)
				}
				modelReq.Model = agentModel.ModelID
				logger.Info("agent trying model (stream)",
					"agent", agent.Name,
					"model", agentModel.ModelID,
					"priority", agentModel.Priority)
				return routerSvc.CompleteStream(ctx, &modelReq, w, nil)
			},
		})
	}

	return retry.RunStream(ctx, candidates, w, logger)
}

func (a *Adapter) NeedsRefresh(cred *models.Credential) bool {
	return false
}

func (a *Adapter) RefreshCredential(ctx context.Context, cred *models.Credential) (map[string]any, error) {
	return nil, fmt.Errorf("no refresh needed for this credential type")
}

func (a *Adapter) GetModelInfos(ctx context.Context, cred *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	agentSvc := a.getAgentService()
	if agentSvc == nil {
		return nil, fmt.Errorf("agent service not initialized")
	}

	agents, err := agentSvc.List()
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}

	infos := make([]models.ModelInfo, len(agents))
	for i, agent := range agents {
		infos[i] = models.ModelInfo{
			Name:        agent.ID,
			DisplayName: agent.Name,
		}
	}

	return infos, nil
}

// ─────────────────────────────────────────────
// Helper Functions
// ─────────────────────────────────────────────

type agentModelList []models.AgentModel

func reorderModels(models agentModelList, selected *models.AgentModel) agentModelList {
	sorted := make(agentModelList, len(models))
	copy(sorted, models)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

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
