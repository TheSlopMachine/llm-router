// Package virtual implements the virtual-model provider backend: a virtual
// model routes each request to its models in list order (fall-through) and
// fails only when every model failed.
package virtual

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
)

// Adapter implements the provider.GoAdapter interface for virtual models.
type Adapter struct {
	routerSvc  *router.Service
	virtualSvc *virtual.Service
	logger     *slog.Logger
	mu         sync.RWMutex
}

// SetRouterService injects the router service dependency.
func (a *Adapter) SetRouterService(svc *router.Service) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.routerSvc = svc
}

// SetVirtualService injects the virtual-model service dependency.
func (a *Adapter) SetVirtualService(svc *virtual.Service) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.virtualSvc = svc
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

func (a *Adapter) getVirtualService() *virtual.Service {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.virtualSvc
}

func (a *Adapter) getLogger() *slog.Logger {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.logger == nil {
		return slog.Default()
	}
	return a.logger
}

// resolve looks up the virtual model targeted by the request model id
// (virtual/<agent-id>) and returns the request with the instruction prepended
// as the first user message.
func (a *Adapter) resolve(req *models.ChatCompletionRequest) (*models.VirtualModel, *models.ChatCompletionRequest, error) {
	_, agentID, err := req.Model.Parse()
	if err != nil {
		return nil, nil, &models.ProviderError{StatusCode: 400, Type: models.ErrorTypeInvalidRequest, Message: fmt.Sprintf("invalid model id: %s", err)}
	}
	if agentID == "" {
		return nil, nil, &models.ProviderError{StatusCode: 400, Type: models.ErrorTypeInvalidRequest, Message: "virtual model id is required"}
	}
	virtualSvc := a.getVirtualService()
	if virtualSvc == nil {
		return nil, nil, fmt.Errorf("virtual model service not initialized")
	}
	agent, err := virtualSvc.Get(agentID)
	if err != nil {
		return nil, nil, &models.ProviderError{StatusCode: 400, Type: models.ErrorTypeInvalidRequest, Message: fmt.Sprintf("virtual model %q not found", agentID)}
	}

	modifiedReq := *req
	if agent.Instruction != "" {
		modifiedReq.Messages = append([]models.ChatMessage{{Role: "user", Content: agent.Instruction}}, req.Messages...)
	}
	return agent, &modifiedReq, nil
}

// ─────────────────────────────────────────────
// GoAdapter Implementation
// ─────────────────────────────────────────────

func (a *Adapter) TypeKey() string {
	return provider.TypeVirtual
}

func (a *Adapter) ValidateCredentials(data map[string]any) error {
	agentID, _ := data["agent_id"].(string)
	if agentID == "" {
		return fmt.Errorf("agent_id is required")
	}

	virtualSvc := a.getVirtualService()
	if virtualSvc == nil {
		return fmt.Errorf("virtual model service not initialized")
	}

	_, err := virtualSvc.Get(agentID)
	if err != nil {
		return fmt.Errorf("virtual model not found: %w", err)
	}

	return nil
}

func (a *Adapter) Complete(
	ctx context.Context,
	creds []*models.Credential,
	req *models.ChatCompletionRequest,
	_ map[string]any,
) (*models.ChatCompletionResponse, error) {
	routerSvc := a.getRouterService()
	if routerSvc == nil {
		return nil, fmt.Errorf("router service not initialized")
	}

	agent, modifiedReq, err := a.resolve(req)
	if err != nil {
		return nil, err
	}
	logger := a.getLogger()

	// Fall-through queue, not retries: each member is tried at most once, in
	// list order. The first success wins; otherwise the last error is
	// returned as-is.
	var lastErr error
	for _, agentModel := range agent.Models {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		modelReq := *modifiedReq
		modelReq.Model = agentModel.ModelID
		logger.Info("virtual model trying model",
			"virtual_model", agent.Name,
			"model", agentModel.ModelID)
		resp, err := routerSvc.Complete(ctx, &modelReq, nil)
		if err == nil {
			logger.Info("virtual model request succeeded",
				"virtual_model", agent.Name,
				"model", agentModel.ModelID)
			return resp, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		return nil, fmt.Errorf("virtual model %q has no models to try", agent.Name)
	}
	return nil, lastErr
}

func (a *Adapter) CompleteStream(
	ctx context.Context,
	creds []*models.Credential,
	req *models.ChatCompletionRequest,
	w io.Writer,
	_ map[string]any,
) error {
	routerSvc := a.getRouterService()
	if routerSvc == nil {
		return fmt.Errorf("router service not initialized")
	}

	agent, modifiedReq, err := a.resolve(req)
	if err != nil {
		return err
	}
	logger := a.getLogger()

	// Same fall-through queue as Complete: attempts continue even after
	// partial writes, members are interchangeable by advertised capabilities.
	var lastErr error
	for _, agentModel := range agent.Models {
		if err := ctx.Err(); err != nil {
			return err
		}
		modelReq := *modifiedReq
		modelReq.Model = agentModel.ModelID
		logger.Info("virtual model trying model (stream)",
			"virtual_model", agent.Name,
			"model", agentModel.ModelID)
		if err := routerSvc.CompleteStream(ctx, &modelReq, w, nil); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		return fmt.Errorf("virtual model %q has no models to try", agent.Name)
	}
	return lastErr
}

func (a *Adapter) NeedsRefresh(cred *models.Credential) bool {
	return false
}

func (a *Adapter) RefreshCredential(ctx context.Context, cred *models.Credential) (map[string]any, error) {
	return nil, provider.ErrNotRefreshable
}

func (a *Adapter) GetModelInfos(ctx context.Context, cred *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	virtualSvc := a.getVirtualService()
	if virtualSvc == nil {
		return nil, fmt.Errorf("virtual model service not initialized")
	}

	agents, err := virtualSvc.List()
	if err != nil {
		return nil, fmt.Errorf("list virtual models: %w", err)
	}

	infos := make([]models.ModelInfo, len(agents))
	for i, agent := range agents {
		infos[i] = models.ModelInfo{
			Name:          agent.ID,
			DisplayName:   agent.Name,
			Description:   agent.Description,
			ContextWindow: agent.ContextLength,
			MaxTokens:     agent.MaxCompletionTokens,
			Capabilities:  agent.Capabilities,
		}
	}

	return infos, nil
}
