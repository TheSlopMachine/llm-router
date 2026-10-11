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
)

// firstByteGate tracks whether the first upstream byte reached the client.
// Once written, the stream belongs to that member: failover stops and the
// member's error ends the stream instead of trying the next member.
type firstByteGate struct {
	writer  io.Writer
	written bool
}

func (g *firstByteGate) Write(p []byte) (int, error) {
	n, err := g.writer.Write(p)
	if n > 0 {
		g.written = true
	}
	return n, err
}

func (g *firstByteGate) Written() bool { return g.written }

// Adapter implements the provider.GoAdapter interface for virtual models.
type Adapter struct {
	routerService  provider.Completer
	virtualService *virtual.GormService
	logger         *slog.Logger
}

// New constructs an Adapter with all dependencies. The router dependency
// enters as the narrow provider.Completer port, so construction order is
// explicit and no setter injection is needed.
func New(completer provider.Completer, virtualService *virtual.GormService, logger *slog.Logger) *Adapter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Adapter{routerService: completer, virtualService: virtualService, logger: logger}
}

// resolve looks up the virtual model targeted by the request model id
// (virtual/<agent-id>) and returns the agent, the member queue snapshotted
// for this request, and the request with the instruction prepended as the
// first user message.
func (a *Adapter) resolve(ctx context.Context, request *models.ChatCompletionRequest) (*models.VirtualModel, []models.ModelId, *models.ChatCompletionRequest, error) {
	_, agentID, err := request.Model.Parse()
	if err != nil {
		return nil, nil, nil, &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: fmt.Sprintf("invalid model id: %s", err)}
	}
	if agentID == "" {
		return nil, nil, nil, &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: "virtual model id is required"}
	}
	virtualService := a.virtualService
	if virtualService == nil {
		return nil, nil, nil, fmt.Errorf("virtual model service not initialized")
	}
	agent, err := virtualService.Get(ctx, agentID)
	if err != nil {
		return nil, nil, nil, &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: fmt.Sprintf("virtual model %q not found", agentID)}
	}
	if agent.Disabled {
		return nil, nil, nil, fmt.Errorf("%w: virtual/%s", apierrors.ErrModelDisabled, agentID)
	}
	members, err := virtualService.LiveMembers(ctx, agent)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("virtual model %q members: %w", agent.Name, err)
	}
	if len(members) == 0 && agent.ManagedBy != "" {
		a.logger.Warn("virtual model group is empty, nothing to try",
			"virtual_model", agent.Name, "managed_by", agent.ManagedBy)
	}

	modifiedRequest := *request
	if agent.Instruction != "" {
		modifiedRequest.Messages = append([]models.ChatMessage{{Role: "user", Content: agent.Instruction}}, request.Messages...)
	}
	return agent, members, &modifiedRequest, nil
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

	virtualService := a.virtualService
	if virtualService == nil {
		return fmt.Errorf("virtual model service not initialized")
	}

	_, err := virtualService.Get(context.Background(), agentID)
	if err != nil {
		return fmt.Errorf("virtual model not found: %w", err)
	}

	return nil
}

func (a *Adapter) Complete(
	ctx context.Context,
	credentials []*models.Credential,
	request *models.ChatCompletionRequest,
	_ map[string]any,
) (*models.ChatCompletionResponse, error) {
	routerService := a.routerService
	if routerService == nil {
		return nil, fmt.Errorf("router service not initialized")
	}

	agent, members, modifiedRequest, err := a.resolve(ctx, request)
	if err != nil {
		return nil, err
	}
	logger := a.logger

	// Fall-through queue in list order: each member is tried at most once.
	// The first success wins; otherwise the last error is returned as-is.
	var lastErr error
	for i, memberID := range members {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		modelRequest := *modifiedRequest
		modelRequest.Model = memberID
		logger.Debug("virtual model trying model",
			"virtual_model", agent.Name,
			"model", memberID)
		resp, err := routerService.Complete(ctx, &modelRequest, nil)
		if err == nil {
			logger.Debug("virtual model request succeeded",
				"virtual_model", agent.Name,
				"model", memberID)
			return resp, nil
		}
		lastErr = err
		if i < len(members)-1 {
			logger.Info("member model failed, trying next",
				"virtual_model", agent.Name,
				"failed_model", memberID.String(),
				"next_model", members[i+1].String(),
				"error", err)
		}
	}
	if lastErr == nil {
		return nil, fmt.Errorf("virtual model %q has no models to try", agent.Name)
	}
	logger.Warn("all member models failed",
		"virtual_model", agent.Name,
		"last_error", lastErr)
	return nil, lastErr
}

func (a *Adapter) CompleteStream(
	ctx context.Context,
	credentials []*models.Credential,
	request *models.ChatCompletionRequest,
	writer io.Writer,
	_ map[string]any,
) error {
	routerService := a.routerService
	if routerService == nil {
		return fmt.Errorf("router service not initialized")
	}

	agent, members, modifiedRequest, err := a.resolve(ctx, request)
	if err != nil {
		return err
	}
	logger := a.logger

	// Same fall-through queue as Complete, with one guard: once the first
	// byte reaches the client the stream belongs to that member and ends
	// with its error instead of continuing to the next member.
	gate := &firstByteGate{writer: writer}
	var lastErr error
	for i, memberID := range members {
		if err := ctx.Err(); err != nil {
			return err
		}
		modelRequest := *modifiedRequest
		modelRequest.Model = memberID
		logger.Debug("virtual model trying model (stream)",
			"virtual_model", agent.Name,
			"model", memberID)
		if err := routerService.CompleteStream(ctx, &modelRequest, gate, nil); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if gate.Written() {
			return lastErr
		}
		if i < len(members)-1 {
			logger.Info("member model failed, trying next",
				"virtual_model", agent.Name,
				"failed_model", memberID.String(),
				"next_model", members[i+1].String(),
				"error", lastErr)
		}
	}
	if lastErr == nil {
		return fmt.Errorf("virtual model %q has no models to try", agent.Name)
	}
	logger.Warn("all member models failed",
		"virtual_model", agent.Name,
		"last_error", lastErr)
	return lastErr
}

// SubmitVideo fans one video generation submit out over the member queue
// (healthy members first, exhausted members last). The first success wins and its router-side job row (keyed
// by the winning member model) is returned as-is: later polls authorize
// against that member model and route to it directly. Otherwise the last
// error is returned as-is.
func (a *Adapter) SubmitVideo(
	ctx context.Context,
	_ []*models.Credential,
	request *models.VideoGenerationRequest,
	_ map[string]any,
) (*models.VideoGenerationResponse, error) {
	routerService := a.routerService
	if routerService == nil {
		return nil, fmt.Errorf("router service not initialized")
	}
	_, agentID, err := request.Model.Parse()
	if err != nil || agentID == "" {
		return nil, &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: "virtual model id is required"}
	}
	virtualService := a.virtualService
	if virtualService == nil {
		return nil, fmt.Errorf("virtual model service not initialized")
	}
	agent, err := virtualService.Get(ctx, agentID)
	if err != nil {
		return nil, &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: fmt.Sprintf("virtual model %q not found", agentID)}
	}
	if agent.Disabled {
		return nil, fmt.Errorf("%w: virtual/%s", apierrors.ErrModelDisabled, agentID)
	}
	members, err := virtualService.LiveMembers(ctx, agent)
	if err != nil {
		return nil, fmt.Errorf("virtual model %q members: %w", agent.Name, err)
	}
	logger := a.logger
	var lastErr error
	for i, memberID := range members {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		memberRequest := *request
		memberRequest.Model = memberID
		logger.Debug("virtual model trying video model",
			"virtual_model", agent.Name,
			"model", memberID)
		resp, err := routerService.SubmitVideo(ctx, &memberRequest, nil)
		if err == nil {
			logger.Debug("virtual model video request succeeded",
				"virtual_model", agent.Name,
				"model", memberID)
			return resp, nil
		}
		lastErr = err
		if i < len(members)-1 {
			logger.Info("member video model failed, trying next",
				"virtual_model", agent.Name,
				"failed_model", memberID.String(),
				"next_model", members[i+1].String(),
				"error", err)
		}
	}
	if lastErr == nil {
		return nil, fmt.Errorf("virtual model %q has no models to try", agent.Name)
	}
	logger.Warn("all member video models failed",
		"virtual_model", agent.Name,
		"last_error", lastErr)
	return nil, lastErr
}

func (a *Adapter) NeedsRefresh(credential *models.Credential) bool {
	return false
}

func (a *Adapter) RefreshCredential(ctx context.Context, credential *models.Credential) (map[string]any, error) {
	return nil, provider.ErrNotRefreshable
}

func (a *Adapter) GetModelInfos(ctx context.Context, credential *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	virtualService := a.virtualService
	if virtualService == nil {
		return nil, fmt.Errorf("virtual model service not initialized")
	}

	agents, err := virtualService.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list virtual models: %w", err)
	}

	modelInfos := make([]models.ModelInfo, len(agents))
	for i, agent := range agents {
		modelInfos[i] = models.ModelInfo{
			Name:        agent.ID,
			DisplayName: agent.Name,
			Description: agent.Description,
		}
	}

	return modelInfos, nil
}
