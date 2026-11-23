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
	memberRouter   provider.MemberRouter
	virtualService *virtual.GormService
	logger         *slog.Logger
}

// New constructs an Adapter with all dependencies. The router dependency
// enters as the narrow provider.MemberRouter port, so construction order is
// explicit and no setter injection is needed.
func New(memberRouter provider.MemberRouter, virtualService *virtual.GormService, logger *slog.Logger) *Adapter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Adapter{memberRouter: memberRouter, virtualService: virtualService, logger: logger}
}

func (a *Adapter) currentLogger() *slog.Logger {
	if a == nil || a.logger == nil {
		return slog.Default()
	}
	return a.logger
}

// members looks up the virtual model targeted by the request model id
// (virtual/<agent-id>) and returns it with the member queue snapshotted for
// this request. A virtual model without members is an error.
func (a *Adapter) members(ctx context.Context, model models.ModelId, endpoint string) (*models.VirtualModel, []models.ModelId, error) {
	logger := a.currentLogger()
	if a.memberRouter == nil {
		fault := fmt.Errorf("router service not initialized")
		logger.Error("Routing virtual request failed", "model", model.String(), "endpoint", endpoint, "error", fault)
		return nil, nil, fault
	}
	_, agentID, err := model.Parse()
	if err != nil {
		fault := &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: fmt.Sprintf("invalid model id: %s", err)}
		logger.Error("Resolving virtual model failed", "model", model.String(), "endpoint", endpoint, "error", err)
		return nil, nil, fault
	}
	if agentID == "" {
		fault := &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: "virtual model id is required"}
		logger.Error("Resolving virtual model failed", "model", model.String(), "endpoint", endpoint, "reason", "virtual model id is required")
		return nil, nil, fault
	}
	if a.virtualService == nil {
		fault := fmt.Errorf("virtual model service not initialized")
		logger.Error("Resolving virtual model failed", "provider_id", agentID, "model", model.String(), "endpoint", endpoint, "error", fault)
		return nil, nil, fault
	}
	agent, err := a.virtualService.Get(ctx, agentID)
	if err != nil {
		fault := &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: fmt.Sprintf("virtual model %q not found", agentID)}
		logger.Error("Resolving virtual model failed", "provider_id", agentID, "model", model.String(), "endpoint", endpoint, "error", err)
		return nil, nil, fault
	}
	if agent.Disabled {
		fault := fmt.Errorf("%w: virtual/%s", apierrors.ErrModelDisabled, agentID)
		logger.Error("Resolving virtual model failed", "provider_id", agent.ID, "model", model.String(), "endpoint", endpoint, "error", fault)
		return nil, nil, fault
	}
	members, err := a.virtualService.LiveMembers(ctx, agent)
	if err != nil {
		logger.Error("Listing virtual members failed", "provider_id", agent.ID, "model", model.String(), "endpoint", endpoint, "error", err)
		return nil, nil, fmt.Errorf("virtual model %q members: %w", agent.Name, err)
	}
	if len(members) == 0 {
		fault := fmt.Errorf("virtual model %q has no models to try", agent.Name)
		logger.Error("Virtual model has no members", "provider_id", agent.ID, "endpoint", endpoint, "count", 0)
		return nil, nil, fault
	}
	return agent, members, nil
}

// fallThrough routes one request of a virtual model to its members in list
// order, each tried at most once. The first success wins; otherwise the last
// error is returned as-is. committed, when set, reports that the client
// already received output: the failing member then ends the request instead
// of the next member taking over.
func fallThrough[Response any](
	ctx context.Context,
	adapter *Adapter,
	model models.ModelId,
	endpoint string,
	committed func() bool,
	try func(agent *models.VirtualModel, member models.ModelId) (Response, error),
) (Response, error) {
	var empty Response
	agent, members, err := adapter.members(ctx, model, endpoint)
	if err != nil {
		return empty, err
	}
	logger := adapter.currentLogger()
	var lastErr error
	for i, member := range members {
		if err := ctx.Err(); err != nil {
			logger.Warn("Virtual request canceled", "provider_id", agent.ID, "model", member.String(), "endpoint", endpoint, "attempt", i, "error", err)
			return empty, err
		}
		logger.Debug("Trying member model", "provider_id", agent.ID, "model", member.String(), "endpoint", endpoint, "attempt", i, "count", len(members))
		response, err := try(agent, member)
		if err == nil {
			logger.Debug("Member model succeeded", "provider_id", agent.ID, "model", member.String(), "endpoint", endpoint, "attempt", i)
			return response, nil
		}
		lastErr = err
		if committed != nil && committed() {
			logger.Warn("Stopping failover after first byte", "provider_id", agent.ID, "model", member.String(), "endpoint", endpoint, "attempt", i, "error", err)
			return empty, err
		}
		if i < len(members)-1 {
			logger.Warn("Member model failed, trying next", "provider_id", agent.ID, "model", member.String(), "endpoint", endpoint, "attempt", i, "count", len(members), "error", err)
		}
	}
	logger.Warn("All member models failed", "provider_id", agent.ID, "endpoint", endpoint, "count", len(members), "error", lastErr)
	return empty, lastErr
}

// memberChatRequest addresses a chat request to one member, with the
// virtual model's instruction prepended as the first user message.
func memberChatRequest(request *models.ChatCompletionRequest, agent *models.VirtualModel, member models.ModelId) *models.ChatCompletionRequest {
	memberRequest := *request
	memberRequest.Model = member
	if agent.Instruction != "" {
		memberRequest.Messages = append([]models.ChatMessage{{Role: "user", Content: agent.Instruction}}, request.Messages...)
	}
	return &memberRequest
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
	_ []*models.Credential,
	request *models.ChatCompletionRequest,
	_ map[string]any,
) (*models.ChatCompletionResponse, error) {
	return fallThrough(ctx, a, request.Model, models.EndpointChatCompletions, nil,
		func(agent *models.VirtualModel, member models.ModelId) (*models.ChatCompletionResponse, error) {
			return a.memberRouter.Complete(ctx, memberChatRequest(request, agent, member), nil)
		})
}

func (a *Adapter) CompleteStream(
	ctx context.Context,
	_ []*models.Credential,
	request *models.ChatCompletionRequest,
	writer io.Writer,
	_ map[string]any,
) error {
	gate := &firstByteGate{writer: writer}
	_, err := fallThrough(ctx, a, request.Model, models.EndpointChatCompletions, gate.Written,
		func(agent *models.VirtualModel, member models.ModelId) (struct{}, error) {
			return struct{}{}, a.memberRouter.CompleteStream(ctx, memberChatRequest(request, agent, member), gate, nil)
		})
	return err
}

func (a *Adapter) Transcribe(
	ctx context.Context,
	_ []*models.Credential,
	request *models.TranscriptionRequest,
	_ map[string]any,
) (*models.TranscriptionResponse, error) {
	endpoint := models.EndpointAudioTranscription
	if request.Task == "translate" {
		endpoint = models.EndpointAudioTranslation
	}
	return fallThrough(ctx, a, request.Model, endpoint, nil,
		func(_ *models.VirtualModel, member models.ModelId) (*models.TranscriptionResponse, error) {
			memberRequest := *request
			memberRequest.Model = member
			return a.memberRouter.Transcribe(ctx, &memberRequest, nil)
		})
}

func (a *Adapter) Speech(
	ctx context.Context,
	_ []*models.Credential,
	request *models.SpeechRequest,
	_ map[string]any,
) (*models.SpeechResponse, error) {
	return fallThrough(ctx, a, request.Model, models.EndpointAudioSpeech, nil,
		func(_ *models.VirtualModel, member models.ModelId) (*models.SpeechResponse, error) {
			memberRequest := *request
			memberRequest.Model = member
			return a.memberRouter.Speech(ctx, &memberRequest, nil)
		})
}

func (a *Adapter) GenerateImage(
	ctx context.Context,
	_ []*models.Credential,
	request *models.ImageGenerationRequest,
	_ map[string]any,
) (*models.ImageGenerationResponse, error) {
	return fallThrough(ctx, a, request.Model, models.EndpointImagesGenerations, nil,
		func(_ *models.VirtualModel, member models.ModelId) (*models.ImageGenerationResponse, error) {
			memberRequest := *request
			memberRequest.Model = member
			return a.memberRouter.GenerateImage(ctx, &memberRequest, nil)
		})
}

func (a *Adapter) Embed(
	ctx context.Context,
	_ []*models.Credential,
	request *models.EmbeddingsRequest,
	_ map[string]any,
) (*models.EmbeddingsResponse, error) {
	return fallThrough(ctx, a, request.Model, models.EndpointEmbeddings, nil,
		func(_ *models.VirtualModel, member models.ModelId) (*models.EmbeddingsResponse, error) {
			memberRequest := *request
			memberRequest.Model = member
			return a.memberRouter.Embed(ctx, &memberRequest, nil)
		})
}

func (a *Adapter) Moderate(
	ctx context.Context,
	_ []*models.Credential,
	request *models.ModerationRequest,
	_ map[string]any,
) (*models.ModerationResponse, error) {
	return fallThrough(ctx, a, request.Model, models.EndpointModerations, nil,
		func(_ *models.VirtualModel, member models.ModelId) (*models.ModerationResponse, error) {
			memberRequest := *request
			memberRequest.Model = member
			return a.memberRouter.Moderate(ctx, &memberRequest, nil)
		})
}

// SubmitVideo fans one video generation submit out over the member queue.
// The first success wins and its router-side job row (keyed by the winning
// member model) is returned as-is: later polls authorize against that member
// model and route to it directly.
func (a *Adapter) SubmitVideo(
	ctx context.Context,
	_ []*models.Credential,
	request *models.VideoGenerationRequest,
	_ map[string]any,
) (*models.VideoGenerationResponse, error) {
	return fallThrough(ctx, a, request.Model, models.EndpointVideos, nil,
		func(_ *models.VirtualModel, member models.ModelId) (*models.VideoGenerationResponse, error) {
			memberRequest := *request
			memberRequest.Model = member
			return a.memberRouter.SubmitVideo(ctx, &memberRequest, nil)
		})
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
