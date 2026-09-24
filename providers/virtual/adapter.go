// Package virtual implements the virtual-model provider backend: a virtual
// model routes each request to its models in list order (fall-through) and
// fails only when every model failed.
package virtual

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
	"github.com/TheSlopMachine/llm-router/internal/streamgate"
)

// Adapter implements the provider.GoAdapter interface for virtual models.
type Adapter struct {
	routerSvc  provider.Completer
	virtualSvc *virtual.Service
	logger     *slog.Logger
}

// New constructs an Adapter with all dependencies. The router dependency
// enters as the narrow provider.Completer port, so construction order is
// explicit and no setter injection is needed.
func New(completer provider.Completer, virtualSvc *virtual.Service, logger *slog.Logger) *Adapter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Adapter{routerSvc: completer, virtualSvc: virtualSvc, logger: logger}
}

// resolve looks up the virtual model targeted by the request model id
// (virtual/<agent-id>) and returns the agent, the member queue snapshotted
// for this request, and the request with the instruction prepended as the
// first user message.
func (a *Adapter) resolve(req *models.ChatCompletionRequest) (*models.VirtualModel, []models.ModelId, *models.ChatCompletionRequest, error) {
	_, agentID, err := req.Model.Parse()
	if err != nil {
		return nil, nil, nil, &models.ProviderError{StatusCode: 400, Type: models.ErrorTypeInvalidRequest, Message: fmt.Sprintf("invalid model id: %s", err)}
	}
	if agentID == "" {
		return nil, nil, nil, &models.ProviderError{StatusCode: 400, Type: models.ErrorTypeInvalidRequest, Message: "virtual model id is required"}
	}
	virtualSvc := a.virtualSvc
	if virtualSvc == nil {
		return nil, nil, nil, fmt.Errorf("virtual model service not initialized")
	}
	agent, err := virtualSvc.Get(agentID)
	if err != nil {
		return nil, nil, nil, &models.ProviderError{StatusCode: 400, Type: models.ErrorTypeInvalidRequest, Message: fmt.Sprintf("virtual model %q not found", agentID)}
	}
	if agent.Disabled {
		return nil, nil, nil, fmt.Errorf("%w: virtual/%s", apierrors.ErrModelDisabled, agentID)
	}
	members, err := virtualSvc.LiveMembers(agent)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("virtual model %q members: %w", agent.Name, err)
	}
	if len(members) == 0 && agent.ManagedBy != "" {
		a.logger.Warn("virtual model group is empty, nothing to try",
			"virtual_model", agent.Name, "managed_by", agent.ManagedBy)
	}

	modifiedReq := *req
	if agent.Instruction != "" {
		modifiedReq.Messages = append([]models.ChatMessage{{Role: "user", Content: agent.Instruction}}, req.Messages...)
	}
	return agent, members, &modifiedReq, nil
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

	virtualSvc := a.virtualSvc
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
	routerSvc := a.routerSvc
	if routerSvc == nil {
		return nil, fmt.Errorf("router service not initialized")
	}

	agent, members, modifiedReq, err := a.resolve(req)
	if err != nil {
		return nil, err
	}
	logger := a.logger

	// Fall-through queue, not retries: each member is tried at most once, in
	// list order. The first success wins; otherwise the last error is
	// returned as-is.
	var lastErr error
	for _, memberID := range members {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		modelReq := *modifiedReq
		modelReq.Model = memberID
		logger.Info("virtual model trying model",
			"virtual_model", agent.Name,
			"model", memberID)
		resp, err := routerSvc.Complete(ctx, &modelReq, nil)
		if err == nil {
			logger.Info("virtual model request succeeded",
				"virtual_model", agent.Name,
				"model", memberID)
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
	routerSvc := a.routerSvc
	if routerSvc == nil {
		return fmt.Errorf("router service not initialized")
	}

	agent, members, modifiedReq, err := a.resolve(req)
	if err != nil {
		return err
	}
	logger := a.logger

	// Same fall-through queue as Complete, with one guard: once the first
	// byte reaches the client the stream belongs to that member and ends
	// with its error instead of continuing to the next member.
	gate := streamgate.New(w)
	var lastErr error
	for _, memberID := range members {
		if err := ctx.Err(); err != nil {
			return err
		}
		modelReq := *modifiedReq
		modelReq.Model = memberID
		logger.Info("virtual model trying model (stream)",
			"virtual_model", agent.Name,
			"model", memberID)
		if err := routerSvc.CompleteStream(ctx, &modelReq, gate, nil); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if gate.Written() {
			return lastErr
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
	virtualSvc := a.virtualSvc
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
			Name:        agent.ID,
			DisplayName: agent.Name,
			Description: agent.Description,
		}
	}

	return infos, nil
}
