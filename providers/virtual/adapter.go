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

func (a *Adapter) currentLogger() *slog.Logger {
	if a == nil || a.logger == nil {
		return slog.Default()
	}
	return a.logger
}

// resolve looks up the virtual model targeted by the request model id
// (virtual/<agent-id>) and returns the agent, the member queue snapshotted
// for this request, and the request with the instruction prepended as the
// first user message.
func (a *Adapter) resolve(ctx context.Context, request *models.ChatCompletionRequest) (*models.VirtualModel, []models.ModelId, *models.ChatCompletionRequest, error) {
	logger := a.currentLogger()
	_, agentID, err := request.Model.Parse()
	if err != nil {
		fault := &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: fmt.Sprintf("invalid model id: %s", err)}
		logger.Error("Resolving virtual model failed", "model", request.Model.String(), "endpoint", "/chat/completions", "error", err)
		return nil, nil, nil, fault
	}
	if agentID == "" {
		fault := &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: "virtual model id is required"}
		logger.Error("Resolving virtual model failed", "model", request.Model.String(), "endpoint", "/chat/completions", "reason", "virtual model id is required")
		return nil, nil, nil, fault
	}
	virtualService := a.virtualService
	if virtualService == nil {
		fault := fmt.Errorf("virtual model service not initialized")
		logger.Error("Resolving virtual model failed", "provider_id", agentID, "model", request.Model.String(), "endpoint", "/chat/completions", "error", fault)
		return nil, nil, nil, fault
	}
	agent, err := virtualService.Get(ctx, agentID)
	if err != nil {
		fault := &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: fmt.Sprintf("virtual model %q not found", agentID)}
		logger.Error("Resolving virtual model failed", "provider_id", agentID, "model", request.Model.String(), "endpoint", "/chat/completions", "error", err)
		return nil, nil, nil, fault
	}
	if agent.Disabled {
		fault := fmt.Errorf("%w: virtual/%s", apierrors.ErrModelDisabled, agentID)
		logger.Error("Resolving virtual model failed", "provider_id", agent.ID, "model", request.Model.String(), "endpoint", "/chat/completions", "error", fault)
		return nil, nil, nil, fault
	}
	members, err := virtualService.LiveMembers(ctx, agent)
	if err != nil {
		fault := fmt.Errorf("virtual model %q members: %w", agent.Name, err)
		logger.Error("Listing virtual members failed", "provider_id", agent.ID, "model", request.Model.String(), "endpoint", "/chat/completions", "error", err)
		return nil, nil, nil, fault
	}
	if len(members) == 0 {
		logger.Warn("Virtual model group is empty", "provider_id", agent.ID, "count", 0)
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
	logger := a.currentLogger()
	agentID, _ := data["agent_id"].(string)
	if agentID == "" {
		fault := fmt.Errorf("agent_id is required")
		logger.Error("Validating credentials failed", "reason", "agent id is required")
		return fault
	}

	virtualService := a.virtualService
	if virtualService == nil {
		fault := fmt.Errorf("virtual model service not initialized")
		logger.Error("Validating credentials failed", "provider_id", agentID, "error", fault)
		return fault
	}

	_, err := virtualService.Get(context.Background(), agentID)
	if err != nil {
		fault := fmt.Errorf("virtual model not found: %w", err)
		logger.Error("Validating credentials failed", "provider_id", agentID, "error", err)
		return fault
	}

	logger.Debug("Validating credentials", "provider_id", agentID)
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
		fault := fmt.Errorf("router service not initialized")
		a.currentLogger().Error("Routing virtual request failed", "model", request.Model.String(), "endpoint", "/chat/completions", "error", fault)
		return nil, fault
	}

	agent, members, modifiedRequest, err := a.resolve(ctx, request)
	if err != nil {
		return nil, err
	}
	logger := a.currentLogger()

	// Fall-through queue in list order: each member is tried at most once.
	// The first success wins; otherwise the last error is returned as-is.
	var lastErr error
	for i, memberID := range members {
		if err := ctx.Err(); err != nil {
			logger.Warn("Virtual request canceled", "provider_id", agent.ID, "model", memberID.String(), "endpoint", "/chat/completions", "attempt", i, "error", err)
			return nil, err
		}
		modelRequest := *modifiedRequest
		modelRequest.Model = memberID
		logger.Debug("Trying member model",
			"provider_id", agent.ID,
			"model", memberID.String(),
			"endpoint", "/chat/completions",
			"attempt", i,
			"count", len(members))
		resp, err := routerService.Complete(ctx, &modelRequest, nil)
		if err == nil {
			logger.Debug("Member model succeeded",
				"provider_id", agent.ID,
				"model", memberID.String(),
				"endpoint", "/chat/completions",
				"attempt", i)
			return resp, nil
		}
		lastErr = err
		if i < len(members)-1 {
			logger.Warn("Member model failed, trying next",
				"provider_id", agent.ID,
				"model", memberID.String(),
				"endpoint", "/chat/completions",
				"attempt", i,
				"count", len(members),
				"error", err)
		}
	}
	if lastErr == nil {
		fault := fmt.Errorf("virtual model %q has no models to try", agent.Name)
		logger.Error("Virtual model has no members", "provider_id", agent.ID, "endpoint", "/chat/completions", "count", 0)
		return nil, fault
	}
	logger.Warn("All member models failed",
		"provider_id", agent.ID,
		"endpoint", "/chat/completions",
		"count", len(members),
		"error", lastErr)
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
		fault := fmt.Errorf("router service not initialized")
		a.currentLogger().Error("Routing virtual request failed", "model", request.Model.String(), "endpoint", "/chat/completions", "error", fault)
		return fault
	}

	agent, members, modifiedRequest, err := a.resolve(ctx, request)
	if err != nil {
		return err
	}
	logger := a.currentLogger()

	// Same fall-through queue as Complete, with one guard: once the first
	// byte reaches the client the stream belongs to that member and ends
	// with its error instead of continuing to the next member.
	gate := &firstByteGate{writer: writer}
	var lastErr error
	for i, memberID := range members {
		if err := ctx.Err(); err != nil {
			logger.Warn("Virtual request canceled", "provider_id", agent.ID, "model", memberID.String(), "endpoint", "/chat/completions", "attempt", i, "error", err)
			return err
		}
		modelRequest := *modifiedRequest
		modelRequest.Model = memberID
		logger.Debug("Trying member model for stream",
			"provider_id", agent.ID,
			"model", memberID.String(),
			"endpoint", "/chat/completions",
			"attempt", i,
			"count", len(members))
		if err := routerService.CompleteStream(ctx, &modelRequest, gate, nil); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if gate.Written() {
			logger.Warn("Stopping failover after first byte", "provider_id", agent.ID, "model", memberID.String(), "endpoint", "/chat/completions", "attempt", i, "error", lastErr)
			return lastErr
		}
		if i < len(members)-1 {
			logger.Warn("Member model failed, trying next",
				"provider_id", agent.ID,
				"model", memberID.String(),
				"endpoint", "/chat/completions",
				"attempt", i,
				"count", len(members),
				"error", lastErr)
		}
	}
	if lastErr == nil {
		fault := fmt.Errorf("virtual model %q has no models to try", agent.Name)
		logger.Error("Virtual model has no members", "provider_id", agent.ID, "endpoint", "/chat/completions", "count", 0)
		return fault
	}
	logger.Warn("All member models failed",
		"provider_id", agent.ID,
		"endpoint", "/chat/completions",
		"count", len(members),
		"error", lastErr)
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
		fault := fmt.Errorf("router service not initialized")
		a.currentLogger().Error("Routing virtual request failed", "model", request.Model.String(), "endpoint", "/videos", "error", fault)
		return nil, fault
	}
	_, agentID, err := request.Model.Parse()
	if err != nil || agentID == "" {
		fault := &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: "virtual model id is required"}
		a.currentLogger().Error("Resolving virtual model failed", "model", request.Model.String(), "endpoint", "/videos", "reason", "virtual model id is required")
		return nil, fault
	}
	virtualService := a.virtualService
	if virtualService == nil {
		fault := fmt.Errorf("virtual model service not initialized")
		a.currentLogger().Error("Resolving virtual model failed", "provider_id", agentID, "model", request.Model.String(), "endpoint", "/videos", "error", fault)
		return nil, fault
	}
	agent, err := virtualService.Get(ctx, agentID)
	if err != nil {
		fault := &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: fmt.Sprintf("virtual model %q not found", agentID)}
		a.currentLogger().Error("Resolving virtual model failed", "provider_id", agentID, "model", request.Model.String(), "endpoint", "/videos", "error", err)
		return nil, fault
	}
	if agent.Disabled {
		fault := fmt.Errorf("%w: virtual/%s", apierrors.ErrModelDisabled, agentID)
		a.currentLogger().Error("Resolving virtual model failed", "provider_id", agent.ID, "model", request.Model.String(), "endpoint", "/videos", "error", fault)
		return nil, fault
	}
	members, err := virtualService.LiveMembers(ctx, agent)
	if err != nil {
		a.currentLogger().Error("Listing virtual members failed", "provider_id", agent.ID, "model", request.Model.String(), "endpoint", "/videos", "error", err)
		return nil, fmt.Errorf("virtual model %q members: %w", agent.Name, err)
	}
	if len(members) == 0 {
		a.currentLogger().Warn("Virtual model group is empty", "provider_id", agent.ID, "endpoint", "/videos", "count", 0)
	}
	logger := a.currentLogger()
	var lastErr error
	for i, memberID := range members {
		if err := ctx.Err(); err != nil {
			logger.Warn("Virtual request canceled", "provider_id", agent.ID, "model", memberID.String(), "endpoint", "/videos", "attempt", i, "error", err)
			return nil, err
		}
		memberRequest := *request
		memberRequest.Model = memberID
		logger.Debug("Trying member model",
			"provider_id", agent.ID,
			"model", memberID.String(),
			"endpoint", "/videos",
			"attempt", i,
			"count", len(members))
		resp, err := routerService.SubmitVideo(ctx, &memberRequest, nil)
		if err == nil {
			logger.Debug("Member model succeeded",
				"provider_id", agent.ID,
				"model", memberID.String(),
				"endpoint", "/videos",
				"attempt", i)
			return resp, nil
		}
		lastErr = err
		if i < len(members)-1 {
			logger.Warn("Member model failed, trying next",
				"provider_id", agent.ID,
				"model", memberID.String(),
				"endpoint", "/videos",
				"attempt", i,
				"count", len(members),
				"error", err)
		}
	}
	if lastErr == nil {
		fault := fmt.Errorf("virtual model %q has no models to try", agent.Name)
		logger.Error("Virtual model has no members", "provider_id", agent.ID, "endpoint", "/videos", "count", 0)
		return nil, fault
	}
	logger.Warn("All member video models failed",
		"provider_id", agent.ID,
		"endpoint", "/videos",
		"count", len(members),
		"error", lastErr)
	return nil, lastErr
}

func (a *Adapter) NeedsRefresh(credential *models.Credential) bool {
	a.currentLogger().Debug("Checking credential refresh need")
	return false
}

func (a *Adapter) RefreshCredential(ctx context.Context, credential *models.Credential) (map[string]any, error) {
	return nil, provider.ErrNotRefreshable
}

func (a *Adapter) GetModelInfos(ctx context.Context, credential *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	virtualService := a.virtualService
	if virtualService == nil {
		fault := fmt.Errorf("virtual model service not initialized")
		a.currentLogger().Error("Listing virtual models failed", "endpoint", "/models", "error", fault)
		return nil, fault
	}

	agents, err := virtualService.List(ctx)
	if err != nil {
		a.currentLogger().Error("Listing virtual models failed", "endpoint", "/models", "error", err)
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

	a.currentLogger().Debug("Listing virtual models", "endpoint", "/models", "count", len(modelInfos))
	return modelInfos, nil
}
