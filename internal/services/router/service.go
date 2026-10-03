// Package router implements the Router Service.
//
// Responsibilities:
//   - Resolving a ModelId to the correct Provider
//   - Enforcing router-token credential rules as an allow-list for plugins
//   - Single-pass delegation to Lua plugins or built-in Go adapters
package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/batches"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/responses"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
)

// Service routes validated API requests to the appropriate provider.
type Service struct {
	providerSvc  *provider.Service
	credSvc      *credential.Service
	modelInfoSvc *modelinfo.Service
	videoJobsSvc *videojobs.Service
	responseSvc  *responses.Service
	batchSvc     *batches.Service
	logger       *slog.Logger
}

// New constructs a new router Service. videoJobsSvc may be nil (video
// submits fail closed). responseSvc and batchSvc may be nil
// (responses/batches fail closed). Production always wires all of them.
func New(providerSvc *provider.Service, credSvc *credential.Service, modelInfoSvc *modelinfo.Service, videoJobsSvc *videojobs.Service, responseSvc *responses.Service, batchSvc *batches.Service, logger *slog.Logger) *Service {
	return &Service{
		providerSvc:  providerSvc,
		credSvc:      credSvc,
		modelInfoSvc: modelInfoSvc,
		videoJobsSvc: videoJobsSvc,
		responseSvc:  responseSvc,
		batchSvc:     batchSvc,
		logger:       logger,
	}
}

// tokenRulesCtxKey carries the outer request's credential rules across
// re-entrant router calls (virtual-model fan-out). The virtual adapter
// forwards ctx untouched, so inner member calls inherit the same rules
// instead of running unrestricted on a nil token.
type tokenRulesCtxKey struct{}

// withTokenRules snapshots an explicit request token into ctx. A nil token
// with no snapshot already present stays unrestricted (admin probes).
func withTokenRules(ctx context.Context, token *models.RouterToken) context.Context {
	if token == nil {
		return ctx
	}
	rules := token.Rules
	return context.WithValue(ctx, tokenRulesCtxKey{}, &rules)
}

// effectiveToken resolves the credential policy for a routing step: the
// explicit token wins, otherwise the outer snapshot from ctx applies,
// otherwise the call is unrestricted.
func effectiveToken(ctx context.Context, token *models.RouterToken) *models.RouterToken {
	if token != nil {
		return token
	}
	if rules, ok := ctx.Value(tokenRulesCtxKey{}).(*models.TokenRules); ok && rules != nil {
		return &models.RouterToken{Rules: *rules}
	}
	return nil
}

// filterCredentials filters the credential list according to token rules.
// A nil token means no restriction (admin probes and other internal calls
// running outside any request snapshot). Provider-scoped rules evaluate
// against providerID; the legacy flat credential list is provider-blind.
func (s *Service) filterCredentials(providerID string, creds []*models.Credential, token *models.RouterToken) []*models.Credential {
	if token == nil || token.Rules.AllowAllCredentials {
		return creds
	}
	out := make([]*models.Credential, 0, len(creds))
	for _, c := range creds {
		if token.Rules.AllowsCredential(providerID, c.ID) {
			out = append(out, c)
		}
	}
	return out
}

func (s *Service) completeOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, allowed []string, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, string, error) {
	if resolved.IsLua() {
		resp, err := s.providerSvc.LuaService().Complete(ctx, s.meta(resolved, req.Model, allowed), req)
		return resp, "", err
	}
	resp, err := resolved.Go.Complete(ctx, creds, req, resolved.Instance.Config)
	return resp, "", err
}

func (s *Service) completeStreamOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, allowed []string, req *models.ChatCompletionRequest, w io.Writer) (string, error) {
	if resolved.IsLua() {
		return "", s.providerSvc.LuaService().CompleteStream(ctx, s.meta(resolved, req.Model, allowed), req, w)
	}
	return "", resolved.Go.CompleteStream(ctx, creds, req, w, resolved.Instance.Config)
}

// meta builds the handler identity for one routed request: the provider
// instance and type serving it, the requested model, the provider config,
// and the token allow-list of credential IDs the plugin may use.
// A nil allow-list means unrestricted (admin probes).
func (s *Service) meta(resolved *provider.Resolved, model models.ModelId, allowed []string) luaplugin.HandlerMeta {
	return luaplugin.HandlerMeta{
		ProviderID:         resolved.Instance.ID,
		TypeKey:            resolved.Instance.TypeKey,
		Model:              model,
		ProviderConfig:     resolved.Instance.Config,
		AllowedCredentials: allowed,
	}
}

// gateCredentials resolves the token allow-list of credential IDs for one
// routed request. Types without credential surfaces (anonymous plugins)
// skip the gate entirely: nil means unrestricted and no credentials are
// required. Otherwise unavailable pools deny with NoCredential; token rules
// filtering out every credential deny with CredentialNotAllowed. A nil
// token means unrestricted: every available credential ID is allowed.
// Virtual providers carry no credentials and skip the gate at the call site.
func (s *Service) gateCredentials(ctx context.Context, resolved *provider.Resolved, model models.ModelId, token *models.RouterToken) ([]string, []*models.Credential, error) {
	p := resolved.Instance
	if resolved.IsLua() && !s.providerSvc.CredentialsEnabled(p.TypeKey) {
		return nil, nil, nil
	}
	creds, err := s.credSvc.All(p.ID)
	if err != nil {
		s.logger.Debug("router: no available credentials", "provider_id", p.ID, "reason", "no_available_credentials")
		return nil, nil, fmt.Errorf("%w for provider %q", apierrors.ErrNoCredential, p.Name)
	}
	filtered := s.filterCredentials(p.ID, creds, effectiveToken(ctx, token))
	if len(filtered) == 0 {
		s.logger.Debug("router: credential pool empty after filtering",
			"provider_id", p.ID, "model", model.String(), "reason", "empty_after_filter",
			"total", len(creds))
		return nil, nil, fmt.Errorf("%w for provider %q", apierrors.ErrCredentialNotAllowed, p.Name)
	}
	allowed := make([]string, 0, len(filtered))
	for _, c := range filtered {
		allowed = append(allowed, c.ID)
	}
	return allowed, filtered, nil
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

// requireModelEnabled enforces the admin model override gate fail-closed:
// missing overrides allow routing, storage failures deny routing.
func (s *Service) requireModelEnabled(providerID, modelName string, model models.ModelId, allowDisabled bool) error {
	if allowDisabled {
		return nil
	}
	enabled, err := s.modelInfoSvc.IsModelEnabled(providerID, modelName)
	if err != nil {
		return err
	}
	if !enabled {
		return fmt.Errorf("%w: %s", apierrors.ErrModelDisabled, model)
	}
	return nil
}

// dropMissingModel removes the model from the info cache when the backend
// reports it does not exist upstream. Best-effort: the original error is
// always returned untouched.
func (s *Service) dropMissingModel(providerID, modelName string, err error) {
	if err == nil {
		return
	}
	var perr *models.ProviderError
	if !errors.As(err, &perr) || perr.Code != "not_found" {
		return
	}
	removed, rerr := s.modelInfoSvc.RemoveModel(providerID, modelName)
	if rerr != nil {
		s.logger.Warn("router: drop missing model from cache failed",
			"provider_id", providerID, "model", modelName, "error", rerr)
		return
	}
	if removed {
		s.logger.Warn("router: upstream reports model does not exist, dropped from cache",
			"provider_id", providerID, "model", modelName)
	}
}

// Complete routes a non-streaming chat completion request in a single
// backend pass. Lua plugins run their own credential/proxy iteration and
// return the first success or the terminal error as-is.
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
	resp, _, err := s.completeWithProxy(ctx, req, token, allowDisabled)
	return resp, err
}

// completeWithProxy is complete plus a proxy placeholder kept for caller
// compatibility. Plugins own proxy selection in 0.7.0: it always returns "".
func (s *Service) completeWithProxy(
	ctx context.Context,
	req *models.ChatCompletionRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.ChatCompletionResponse, string, error) {
	ctx, rr, err := s.resolveRequest(ctx, req.Model, token, allowDisabled, models.EndpointChatCompletions)
	if err != nil {
		return nil, "", err
	}
	resolved := rr.resolved
	// Virtual agents resolve from the model name suffix and need no
	// credentials of their own; the outer token snapshot in ctx still
	// restricts the credentials of the member models tried inside.
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return s.completeOne(ctx, resolved, nil, nil, req)
	}
	allowed, creds, err := s.gateCredentials(ctx, resolved, req.Model, token)
	if err != nil {
		return nil, "", err
	}
	resp, _, err := s.completeOne(ctx, resolved, creds, allowed, req)
	s.dropMissingModel(rr.providerID, rr.modelName, err)
	return resp, "", err
}

// CompleteStream routes a streaming chat completion request.
// Server-Sent Events are written directly to w.
func (s *Service) CompleteStream(
	ctx context.Context,
	req *models.ChatCompletionRequest,
	w io.Writer,
	token *models.RouterToken,
) error {
	ctx, rr, err := s.resolveRequest(ctx, req.Model, token, false, models.EndpointChatCompletions)
	if err != nil {
		return err
	}
	resolved := rr.resolved
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		_, err := s.completeStreamOne(ctx, resolved, nil, nil, req, w)
		return err
	}
	allowed, creds, err := s.gateCredentials(ctx, resolved, req.Model, token)
	if err != nil {
		return err
	}
	_, err = s.completeStreamOne(ctx, resolved, creds, allowed, req, w)
	s.dropMissingModel(rr.providerID, rr.modelName, err)
	return err
}

// transcribeOne runs a single transcription pass. Lua plugins iterate
// credentials and proxies internally; Go adapters receive the gated pool.
func (s *Service) transcribeOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, allowed []string, req *models.TranscriptionRequest) (*models.TranscriptionResponse, string, error) {
	if resolved.IsLua() {
		resp, err := s.providerSvc.LuaService().Transcribe(ctx, s.meta(resolved, req.Model, allowed), req)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, "", fmt.Errorf("%w: provider %q has no transcribe handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return resp, "", err
	}
	tr, ok := resolved.Go.(provider.Transcriber)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support audio transcription", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	resp, err := tr.Transcribe(ctx, creds, req, resolved.Instance.Config)
	return resp, "", err
}

// Transcribe routes a POST /v1/audio/transcriptions request in a single
// backend pass over the credential pool.
func (s *Service) Transcribe(
	ctx context.Context,
	req *models.TranscriptionRequest,
	token *models.RouterToken,
) (*models.TranscriptionResponse, error) {
	return s.transcribe(ctx, req, token, false)
}

func (s *Service) transcribe(
	ctx context.Context,
	req *models.TranscriptionRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.TranscriptionResponse, error) {
	resp, _, err := s.transcribeWithProxy(ctx, req, token, allowDisabled)
	return resp, err
}

// transcribeWithProxy is transcribe plus a proxy placeholder kept for
// caller compatibility. Plugins own proxy selection: it always returns "".
func (s *Service) transcribeWithProxy(
	ctx context.Context,
	req *models.TranscriptionRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.TranscriptionResponse, string, error) {
	ctx, rr, err := s.resolveRequest(ctx, req.Model, token, allowDisabled, models.EndpointAudioTranscription)
	if err != nil {
		return nil, "", err
	}
	resolved := rr.resolved
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return nil, "", fmt.Errorf("%w: virtual models do not serve audio transcription", apierrors.ErrEndpointNotSupported)
	}
	// Capability pre-check: fail loudly before touching the credential gate.
	if resolved.IsLua() {
		if !s.providerSvc.LuaService().HasHandler(resolved.Instance.TypeKey, "transcribe") {
			return nil, "", fmt.Errorf("%w: provider %q has no transcribe handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
	} else if _, ok := resolved.Go.(provider.Transcriber); !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support audio transcription", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	allowed, creds, err := s.gateCredentials(ctx, resolved, req.Model, token)
	if err != nil {
		return nil, "", err
	}
	resp, _, err := s.transcribeOne(ctx, resolved, creds, allowed, req)
	s.dropMissingModel(rr.providerID, rr.modelName, err)
	return resp, "", err
}

// speechOne runs a single speech pass. Lua plugins iterate internally;
// Go adapters receive the gated pool.
func (s *Service) speechOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, allowed []string, req *models.SpeechRequest) (*models.SpeechResponse, string, error) {
	if resolved.IsLua() {
		resp, err := s.providerSvc.LuaService().Speech(ctx, s.meta(resolved, req.Model, allowed), req)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, "", fmt.Errorf("%w: provider %q has no speech handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return resp, "", err
	}
	sp, ok := resolved.Go.(provider.Speaker)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support text-to-speech", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	resp, err := sp.Speech(ctx, creds, req, resolved.Instance.Config)
	return resp, "", err
}

// Speech routes a POST /v1/audio/speech request in a single backend pass
// over the credential pool.
func (s *Service) Speech(
	ctx context.Context,
	req *models.SpeechRequest,
	token *models.RouterToken,
) (*models.SpeechResponse, error) {
	return s.speech(ctx, req, token, false)
}

func (s *Service) speech(
	ctx context.Context,
	req *models.SpeechRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.SpeechResponse, error) {
	resp, _, err := s.speechWithProxy(ctx, req, token, allowDisabled)
	return resp, err
}

// speechWithProxy is speech plus a proxy placeholder kept for caller
// compatibility. Plugins own proxy selection: it always returns "".
func (s *Service) speechWithProxy(
	ctx context.Context,
	req *models.SpeechRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.SpeechResponse, string, error) {
	ctx, rr, err := s.resolveRequest(ctx, req.Model, token, allowDisabled, models.EndpointAudioSpeech)
	if err != nil {
		return nil, "", err
	}
	resolved := rr.resolved
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return nil, "", fmt.Errorf("%w: virtual models do not serve text-to-speech", apierrors.ErrEndpointNotSupported)
	}
	// Capability pre-check: fail loudly before touching the credential gate.
	if resolved.IsLua() {
		if !s.providerSvc.LuaService().HasHandler(resolved.Instance.TypeKey, "speech") {
			return nil, "", fmt.Errorf("%w: provider %q has no speech handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
	} else if _, ok := resolved.Go.(provider.Speaker); !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support text-to-speech", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	allowed, creds, err := s.gateCredentials(ctx, resolved, req.Model, token)
	if err != nil {
		return nil, "", err
	}
	resp, _, err := s.speechOne(ctx, resolved, creds, allowed, req)
	s.dropMissingModel(rr.providerID, rr.modelName, err)
	return resp, "", err
}

// generateImageOne runs a single image generation pass. Lua plugins iterate
// internally; Go adapters receive the gated pool.
func (s *Service) generateImageOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, allowed []string, req *models.ImageGenerationRequest) (*models.ImageGenerationResponse, string, error) {
	if resolved.IsLua() {
		resp, err := s.providerSvc.LuaService().GenerateImage(ctx, s.meta(resolved, req.Model, allowed), req)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, "", fmt.Errorf("%w: provider %q has no generate_image handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return resp, "", err
	}
	ig, ok := resolved.Go.(provider.ImageGenerator)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support image generation", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	resp, err := ig.GenerateImage(ctx, creds, req, resolved.Instance.Config)
	return resp, "", err
}

// GenerateImage routes a POST /v1/images/generations request in a single
// backend pass over the credential pool.
func (s *Service) GenerateImage(
	ctx context.Context,
	req *models.ImageGenerationRequest,
	token *models.RouterToken,
) (*models.ImageGenerationResponse, error) {
	return s.generateImage(ctx, req, token, false)
}

func (s *Service) generateImage(
	ctx context.Context,
	req *models.ImageGenerationRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.ImageGenerationResponse, error) {
	resp, _, err := s.generateImageWithProxy(ctx, req, token, allowDisabled)
	return resp, err
}

// generateImageWithProxy is generateImage plus a proxy placeholder kept
// for caller compatibility. Plugins own proxy selection: always "".
func (s *Service) generateImageWithProxy(
	ctx context.Context,
	req *models.ImageGenerationRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.ImageGenerationResponse, string, error) {
	ctx, rr, err := s.resolveRequest(ctx, req.Model, token, allowDisabled, models.EndpointImagesGenerations)
	if err != nil {
		return nil, "", err
	}
	resolved := rr.resolved
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return nil, "", fmt.Errorf("%w: virtual models do not serve image generation", apierrors.ErrEndpointNotSupported)
	}
	// Capability pre-check: fail loudly before touching the credential gate.
	if resolved.IsLua() {
		if !s.providerSvc.LuaService().HasHandler(resolved.Instance.TypeKey, "generate_image") {
			return nil, "", fmt.Errorf("%w: provider %q has no generate_image handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
	} else if _, ok := resolved.Go.(provider.ImageGenerator); !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support image generation", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	allowed, creds, err := s.gateCredentials(ctx, resolved, req.Model, token)
	if err != nil {
		return nil, "", err
	}
	resp, _, err := s.generateImageOne(ctx, resolved, creds, allowed, req)
	s.dropMissingModel(rr.providerID, rr.modelName, err)
	return resp, "", err
}

// embedOne runs a single embeddings pass. Lua plugins iterate internally;
// Go adapters receive the gated pool.
func (s *Service) embedOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, allowed []string, req *models.EmbeddingsRequest) (*models.EmbeddingsResponse, string, error) {
	if resolved.IsLua() {
		resp, err := s.providerSvc.LuaService().Embed(ctx, s.meta(resolved, req.Model, allowed), req)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, "", fmt.Errorf("%w: provider %q has no embed handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return resp, "", err
	}
	em, ok := resolved.Go.(provider.Embedder)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support embeddings", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	resp, err := em.Embed(ctx, creds, req, resolved.Instance.Config)
	return resp, "", err
}

// Embed routes a POST /v1/embeddings request in a single backend pass over
// the credential pool.
func (s *Service) Embed(
	ctx context.Context,
	req *models.EmbeddingsRequest,
	token *models.RouterToken,
) (*models.EmbeddingsResponse, error) {
	return s.embed(ctx, req, token, false)
}

func (s *Service) embed(
	ctx context.Context,
	req *models.EmbeddingsRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.EmbeddingsResponse, error) {
	resp, _, err := s.embedWithProxy(ctx, req, token, allowDisabled)
	return resp, err
}

// embedWithProxy is embed plus a proxy placeholder kept for caller
// compatibility. Plugins own proxy selection: it always returns "".
func (s *Service) embedWithProxy(
	ctx context.Context,
	req *models.EmbeddingsRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.EmbeddingsResponse, string, error) {
	ctx, rr, err := s.resolveRequest(ctx, req.Model, token, allowDisabled, models.EndpointEmbeddings)
	if err != nil {
		return nil, "", err
	}
	resolved := rr.resolved
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return nil, "", fmt.Errorf("%w: virtual models do not serve embeddings", apierrors.ErrEndpointNotSupported)
	}
	// Capability pre-check: fail loudly before touching the credential gate.
	if resolved.IsLua() {
		if !s.providerSvc.LuaService().HasHandler(resolved.Instance.TypeKey, "embed") {
			return nil, "", fmt.Errorf("%w: provider %q has no embed handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
	} else if _, ok := resolved.Go.(provider.Embedder); !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support embeddings", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	allowed, creds, err := s.gateCredentials(ctx, resolved, req.Model, token)
	if err != nil {
		return nil, "", err
	}
	resp, _, err := s.embedOne(ctx, resolved, creds, allowed, req)
	s.dropMissingModel(rr.providerID, rr.modelName, err)
	return resp, "", err
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
