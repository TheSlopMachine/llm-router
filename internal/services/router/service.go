// Package router implements the Router Service.
//
// Responsibilities:
//   - Resolving a ModelId to the correct Provider
//   - Fetching live Credentials from the Credential Pool
//   - Single-pass delegation to Lua plugins or built-in Go adapters
//   - Translating backend-specific errors back to OpenAI-compatible ones
package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

// Service routes validated API requests to the appropriate provider.
type Service struct {
	providerSvc  *provider.Service
	credSvc      *credential.Service
	modelInfoSvc *modelinfo.Service
	logger       *slog.Logger
}

// New constructs a new router Service.
func New(providerSvc *provider.Service, credSvc *credential.Service, modelInfoSvc *modelinfo.Service, logger *slog.Logger) *Service {
	return &Service{
		providerSvc:  providerSvc,
		credSvc:      credSvc,
		modelInfoSvc: modelInfoSvc,
		logger:       logger,
	}
}

// filterCredentials filters the credential list according to token rules.
// A nil token means no restriction (e.g. internal agent calls).
func (s *Service) filterCredentials(creds []*models.Credential, token *models.RouterToken) []*models.Credential {
	if token == nil || token.Rules.AllowAllCredentials {
		return creds
	}
	if len(token.Rules.AllowedCredentials) == 0 {
		return nil
	}
	allow := make(map[string]bool, len(token.Rules.AllowedCredentials))
	for _, id := range token.Rules.AllowedCredentials {
		allow[id] = true
	}
	out := make([]*models.Credential, 0, len(creds))
	for _, c := range creds {
		if allow[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

func (s *Service) completeOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
	cfg := resolved.Instance.Config
	if resolved.IsLua() {
		return s.providerSvc.LuaService().CompletePool(ctx, resolved.Instance.TypeKey, creds, req, cfg)
	}
	return resolved.Go.Complete(ctx, creds, req, cfg)
}

func (s *Service) completeStreamOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, req *models.ChatCompletionRequest, w io.Writer) error {
	cfg := resolved.Instance.Config
	if resolved.IsLua() {
		return s.providerSvc.LuaService().CompleteStreamPool(ctx, resolved.Instance.TypeKey, creds, req, w, cfg)
	}
	return resolved.Go.CompleteStream(ctx, creds, req, w, cfg)
}

func (s *Service) loadCredentials(p *models.ProviderInstance, token *models.RouterToken) ([]*models.Credential, error) {
	creds, err := s.credSvc.All(p.ID)
	if err != nil {
		return nil, fmt.Errorf("%w for provider %q", apierrors.ErrNoCredential, p.Name)
	}
	creds = s.filterCredentials(creds, token)
	if len(creds) == 0 {
		return nil, fmt.Errorf("%w for provider %q", apierrors.ErrCredentialNotAllowed, p.Name)
	}
	return creds, nil
}

// checkEndpoint rejects the request when the cached model card declares an
// endpoint list excluding the target. Models absent from the cache pass:
// gating acts on declared data only, never on guesses.
func (s *Service) checkEndpoint(providerID, modelName, endpoint string) error {
	for _, info := range s.modelInfoSvc.PeekModelInfos(providerID) {
		if info.Name != modelName {
			continue
		}
		if !info.SupportsEndpoint(endpoint) {
			return fmt.Errorf("%w: model %s/%s does not serve %s",
				apierrors.ErrEndpointNotSupported, providerID, modelName, endpoint)
		}
		return nil
	}
	return nil
}

// Complete routes a non-streaming chat completion request. The backend tries
// the credential pool in order, at most once per key; the first success wins
// and the last error is returned as-is.
func (s *Service) Complete(
	ctx context.Context,
	req *models.ChatCompletionRequest,
	token *models.RouterToken,
) (*models.ChatCompletionResponse, error) {
	return s.complete(ctx, req, token, false)
}

// complete is Complete with an explicit admin-override bypass for probes:
// TestModel passes true so manually disabled models stay testable.
func (s *Service) complete(
	ctx context.Context,
	req *models.ChatCompletionRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.ChatCompletionResponse, error) {
	providerID, modelName, err := req.Model.Parse()
	if err != nil {
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	resolved, err := provider.Resolve(s.providerSvc, providerID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrProviderNotFound, providerID)
	}
	if resolved.Instance.Disabled {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrProviderDisabled, providerID)
	}
	// Virtual agents resolve from the model name suffix and need no
	// credentials; token credential rules do not apply to them.
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return s.completeOne(ctx, resolved, nil, req)
	}
	if !allowDisabled && !s.modelInfoSvc.IsModelEnabled(providerID, modelName) {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrModelDisabled, req.Model)
	}
	if err := s.checkEndpoint(providerID, modelName, models.EndpointChatCompletions); err != nil {
		return nil, err
	}
	creds, err := s.loadCredentials(resolved.Instance, token)
	if err != nil {
		return nil, err
	}
	return s.completeOne(ctx, resolved, creds, req)
}

// CompleteStream routes a streaming chat completion request.
// Server-Sent Events are written directly to w.
func (s *Service) CompleteStream(
	ctx context.Context,
	req *models.ChatCompletionRequest,
	w io.Writer,
	token *models.RouterToken,
) error {
	providerID, modelName, err := req.Model.Parse()
	if err != nil {
		return fmt.Errorf("invalid model id: %w", err)
	}
	resolved, err := provider.Resolve(s.providerSvc, providerID)
	if err != nil {
		return fmt.Errorf("%w: %s", apierrors.ErrProviderNotFound, providerID)
	}
	if resolved.Instance.Disabled {
		return fmt.Errorf("%w: %s", apierrors.ErrProviderDisabled, providerID)
	}
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return s.completeStreamOne(ctx, resolved, nil, req, w)
	}
	if !s.modelInfoSvc.IsModelEnabled(providerID, modelName) {
		return fmt.Errorf("%w: %s", apierrors.ErrModelDisabled, req.Model)
	}
	if err := s.checkEndpoint(providerID, modelName, models.EndpointChatCompletions); err != nil {
		return err
	}
	creds, err := s.loadCredentials(resolved.Instance, token)
	if err != nil {
		return err
	}
	return s.completeStreamOne(ctx, resolved, creds, req, w)
}

// transcribeOne runs a single transcription pass against the credential pool.
func (s *Service) transcribeOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, req *models.TranscriptionRequest) (*models.TranscriptionResponse, error) {
	cfg := resolved.Instance.Config
	if resolved.IsLua() {
		resp, err := s.providerSvc.LuaService().TranscribePool(ctx, resolved.Instance.TypeKey, creds, req, cfg)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, fmt.Errorf("%w: provider %q has no transcribe handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return resp, err
	}
	tr, ok := resolved.Go.(provider.Transcriber)
	if !ok {
		return nil, fmt.Errorf("%w: provider %q does not support audio transcription", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	return tr.Transcribe(ctx, creds, req, cfg)
}

// Transcribe routes a POST /v1/audio/transcriptions request in a single
// backend pass over the credential pool.
func (s *Service) Transcribe(
	ctx context.Context,
	req *models.TranscriptionRequest,
	token *models.RouterToken,
) (*models.TranscriptionResponse, error) {
	providerID, modelName, err := req.Model.Parse()
	if err != nil {
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	resolved, err := provider.Resolve(s.providerSvc, providerID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrProviderNotFound, providerID)
	}
	if resolved.Instance.Disabled {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrProviderDisabled, providerID)
	}
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return nil, fmt.Errorf("%w: virtual models do not serve audio transcription", apierrors.ErrEndpointNotSupported)
	}
	if !s.modelInfoSvc.IsModelEnabled(providerID, modelName) {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrModelDisabled, req.Model)
	}
	if err := s.checkEndpoint(providerID, modelName, models.EndpointAudioTranscription); err != nil {
		return nil, err
	}
	// Capability pre-check: fail loudly before touching the credential pool.
	if resolved.IsLua() {
		if !s.providerSvc.LuaService().HasHandler(resolved.Instance.TypeKey, "transcribe") {
			return nil, fmt.Errorf("%w: provider %q has no transcribe handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
	} else if _, ok := resolved.Go.(provider.Transcriber); !ok {
		return nil, fmt.Errorf("%w: provider %q does not support audio transcription", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	creds, err := s.loadCredentials(resolved.Instance, token)
	if err != nil {
		return nil, err
	}
	return s.transcribeOne(ctx, resolved, creds, req)
}

// speechOne runs a single speech pass against the credential pool.
func (s *Service) speechOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, req *models.SpeechRequest) (*models.SpeechResponse, error) {
	cfg := resolved.Instance.Config
	if resolved.IsLua() {
		resp, err := s.providerSvc.LuaService().SpeechPool(ctx, resolved.Instance.TypeKey, creds, req, cfg)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, fmt.Errorf("%w: provider %q has no speech handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return resp, err
	}
	sp, ok := resolved.Go.(provider.Speaker)
	if !ok {
		return nil, fmt.Errorf("%w: provider %q does not support text-to-speech", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	return sp.Speech(ctx, creds, req, cfg)
}

// Speech routes a POST /v1/audio/speech request in a single backend pass
// over the credential pool.
func (s *Service) Speech(
	ctx context.Context,
	req *models.SpeechRequest,
	token *models.RouterToken,
) (*models.SpeechResponse, error) {
	providerID, modelName, err := req.Model.Parse()
	if err != nil {
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	resolved, err := provider.Resolve(s.providerSvc, providerID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrProviderNotFound, providerID)
	}
	if resolved.Instance.Disabled {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrProviderDisabled, providerID)
	}
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return nil, fmt.Errorf("%w: virtual models do not serve text-to-speech", apierrors.ErrEndpointNotSupported)
	}
	if !s.modelInfoSvc.IsModelEnabled(providerID, modelName) {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrModelDisabled, req.Model)
	}
	if err := s.checkEndpoint(providerID, modelName, models.EndpointAudioSpeech); err != nil {
		return nil, err
	}
	// Capability pre-check: fail loudly before touching the credential pool.
	if resolved.IsLua() {
		if !s.providerSvc.LuaService().HasHandler(resolved.Instance.TypeKey, "speech") {
			return nil, fmt.Errorf("%w: provider %q has no speech handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
	} else if _, ok := resolved.Go.(provider.Speaker); !ok {
		return nil, fmt.Errorf("%w: provider %q does not support text-to-speech", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	creds, err := s.loadCredentials(resolved.Instance, token)
	if err != nil {
		return nil, err
	}
	return s.speechOne(ctx, resolved, creds, req)
}

// generateImageOne runs a single image generation pass against the credential pool.
func (s *Service) generateImageOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, req *models.ImageGenerationRequest) (*models.ImageGenerationResponse, error) {
	cfg := resolved.Instance.Config
	if resolved.IsLua() {
		resp, err := s.providerSvc.LuaService().GenerateImagePool(ctx, resolved.Instance.TypeKey, creds, req, cfg)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, fmt.Errorf("%w: provider %q has no generate_image handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return resp, err
	}
	ig, ok := resolved.Go.(provider.ImageGenerator)
	if !ok {
		return nil, fmt.Errorf("%w: provider %q does not support image generation", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	return ig.GenerateImage(ctx, creds, req, cfg)
}

// GenerateImage routes a POST /v1/images/generations request in a single
// backend pass over the credential pool.
func (s *Service) GenerateImage(
	ctx context.Context,
	req *models.ImageGenerationRequest,
	token *models.RouterToken,
) (*models.ImageGenerationResponse, error) {
	providerID, modelName, err := req.Model.Parse()
	if err != nil {
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	resolved, err := provider.Resolve(s.providerSvc, providerID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrProviderNotFound, providerID)
	}
	if resolved.Instance.Disabled {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrProviderDisabled, providerID)
	}
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return nil, fmt.Errorf("%w: virtual models do not serve image generation", apierrors.ErrEndpointNotSupported)
	}
	if !s.modelInfoSvc.IsModelEnabled(providerID, modelName) {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrModelDisabled, req.Model)
	}
	if err := s.checkEndpoint(providerID, modelName, models.EndpointImagesGenerations); err != nil {
		return nil, err
	}
	// Capability pre-check: fail loudly before touching the credential pool.
	if resolved.IsLua() {
		if !s.providerSvc.LuaService().HasHandler(resolved.Instance.TypeKey, "generate_image") {
			return nil, fmt.Errorf("%w: provider %q has no generate_image handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
	} else if _, ok := resolved.Go.(provider.ImageGenerator); !ok {
		return nil, fmt.Errorf("%w: provider %q does not support image generation", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	creds, err := s.loadCredentials(resolved.Instance, token)
	if err != nil {
		return nil, err
	}
	return s.generateImageOne(ctx, resolved, creds, req)
}

// embedOne runs a single embeddings pass against the credential pool.
func (s *Service) embedOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, req *models.EmbeddingsRequest) (*models.EmbeddingsResponse, error) {
	cfg := resolved.Instance.Config
	if resolved.IsLua() {
		resp, err := s.providerSvc.LuaService().EmbedPool(ctx, resolved.Instance.TypeKey, creds, req, cfg)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, fmt.Errorf("%w: provider %q has no embed handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return resp, err
	}
	em, ok := resolved.Go.(provider.Embedder)
	if !ok {
		return nil, fmt.Errorf("%w: provider %q does not support embeddings", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	return em.Embed(ctx, creds, req, cfg)
}

// Embed routes a POST /v1/embeddings request in a single backend pass over
// the credential pool.
func (s *Service) Embed(
	ctx context.Context,
	req *models.EmbeddingsRequest,
	token *models.RouterToken,
) (*models.EmbeddingsResponse, error) {
	providerID, modelName, err := req.Model.Parse()
	if err != nil {
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	resolved, err := provider.Resolve(s.providerSvc, providerID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrProviderNotFound, providerID)
	}
	if resolved.Instance.Disabled {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrProviderDisabled, providerID)
	}
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return nil, fmt.Errorf("%w: virtual models do not serve embeddings", apierrors.ErrEndpointNotSupported)
	}
	if !s.modelInfoSvc.IsModelEnabled(providerID, modelName) {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrModelDisabled, req.Model)
	}
	if err := s.checkEndpoint(providerID, modelName, models.EndpointEmbeddings); err != nil {
		return nil, err
	}
	// Capability pre-check: fail loudly before touching the credential pool.
	if resolved.IsLua() {
		if !s.providerSvc.LuaService().HasHandler(resolved.Instance.TypeKey, "embed") {
			return nil, fmt.Errorf("%w: provider %q has no embed handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
	} else if _, ok := resolved.Go.(provider.Embedder); !ok {
		return nil, fmt.Errorf("%w: provider %q does not support embeddings", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	creds, err := s.loadCredentials(resolved.Instance, token)
	if err != nil {
		return nil, err
	}
	return s.embedOne(ctx, resolved, creds, req)
}

// GetProviderIDForModel returns the composite provider ID for a given model.
func (s *Service) GetProviderIDForModel(ctx context.Context, modelID models.ModelId) (string, error) {
	providerID, _, err := modelID.Parse()
	if err != nil {
		return "", err
	}
	_, err = s.providerSvc.Get(providerID)
	if err != nil {
		return "", err
	}
	return providerID, nil
}

// TestResult reports the outcome of a credential or model probe.
type TestResult struct {
	OK       bool   `json:"ok"`
	Latency  int64  `json:"latency_ms"`
	Error    string `json:"error,omitempty"`
	Response string `json:"response,omitempty"`
}

// probeRequest builds a minimal completion request for connectivity tests.
func probeRequest(model models.ModelId) *models.ChatCompletionRequest {
	return &models.ChatCompletionRequest{
		Model:     model,
		MaxTokens: 16,
		Messages:  []models.ChatMessage{{Role: "user", Content: "Reply with: ok"}},
	}
}

// TestCredential runs a single probe request pinned to one credential,
// bypassing pool rotation. The probe model is the provider's first listed
// model, or overrideModel when given.
func (s *Service) TestCredential(ctx context.Context, providerID, credentialID string, overrideModel string) TestResult {
	resolved, err := provider.Resolve(s.providerSvc, providerID)
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	cred, err := s.credSvc.Get(credentialID)
	if err != nil {
		return TestResult{Error: "credential not found"}
	}
	if cred.ProviderID != providerID {
		return TestResult{Error: "credential does not belong to this provider"}
	}
	model := overrideModel
	if model == "" {
		infos, err := s.modelInfoSvc.GetModelInfos(ctx, providerID)
		if err != nil || len(infos) == 0 {
			return TestResult{Error: "no model available for probe: model discovery failed"}
		}
		model = infos[0].Name
	}
	req := probeRequest(models.ModelId(providerID + "/" + model))
	start := time.Now()
	resp, err := s.completeOne(ctx, resolved, []*models.Credential{cred}, req)
	res := TestResult{OK: err == nil, Latency: time.Since(start).Milliseconds()}
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if len(resp.Choices) > 0 {
		res.Response = resp.Choices[0].Message.TextContent()
	}
	return res
}

// TestModel runs a probe through the normal routing path (credential pool
// included), as an internal unrestricted call. Manually disabled models stay
// testable: the admin override gates routing, not probing.
func (s *Service) TestModel(ctx context.Context, modelID models.ModelId) TestResult {
	start := time.Now()
	resp, err := s.complete(ctx, probeRequest(modelID), nil, true)
	res := TestResult{OK: err == nil, Latency: time.Since(start).Milliseconds()}
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if len(resp.Choices) > 0 {
		res.Response = resp.Choices[0].Message.TextContent()
	}
	return res
}

// ProbeCapabilities detects model features with live probe requests.
// Each probe consumes a small amount of quota. Unprobed capabilities are
// simply absent from the result.
func (s *Service) ProbeCapabilities(ctx context.Context, modelID models.ModelId) ([]string, error) {
	providerID, _, err := modelID.Parse()
	if err != nil {
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	if _, err := provider.Resolve(s.providerSvc, providerID); err != nil {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrProviderNotFound, providerID)
	}

	caps := []string{}

	// Reasoning models spend tokens on thinking before any tool call or
	// JSON output; probes need real headroom.
	toolReq := probeRequest(modelID)
	toolReq.MaxTokens = 512
	toolReq.Messages[0].Content = "Call the function report_weather with city=Paris."
	toolReq.Tools = []models.ChatTool{{
		Type: "function",
		Function: &models.ChatToolFunction{
			Name:        "report_weather",
			Description: "Report weather for a city",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
				"required":   []string{"city"},
			},
		},
	}}
	toolReq.ToolChoice = map[string]any{"type": "function", "function": map[string]any{"name": "report_weather"}}
	if resp, err := s.Complete(ctx, toolReq, nil); err == nil && len(resp.Choices) > 0 && len(resp.Choices[0].Message.ToolCalls) > 0 {
		caps = append(caps, "tools")
	}

	jsonReq := probeRequest(modelID)
	jsonReq.MaxTokens = 512
	jsonReq.Messages[0].Content = `Output exactly: {"ok": true}`
	jsonReq.ResponseFormat = map[string]any{"type": "json_object"}
	if _, err := s.Complete(ctx, jsonReq, nil); err == nil {
		caps = append(caps, "json_mode")
	}

	return caps, nil
}
