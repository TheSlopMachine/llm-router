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
	providerService   *provider.Service
	credentialService *credential.Service
	modelInfoService  *modelinfo.Service
	videoJobsService  *videojobs.Service
	responseService   *responses.Service
	batchService      *batches.Service
	logger            *slog.Logger
}

// New constructs a new router Service. videoJobsService may be nil (video
// submits fail closed). responseService and batchService may be nil
// (responses/batches fail closed). Production always wires all of them.
func New(providerService *provider.Service, credentialService *credential.Service, modelInfoService *modelinfo.Service, videoJobsService *videojobs.Service, responseService *responses.Service, batchService *batches.Service, logger *slog.Logger) *Service {
	return &Service{
		providerService:   providerService,
		credentialService: credentialService,
		modelInfoService:  modelInfoService,
		videoJobsService:  videoJobsService,
		responseService:   responseService,
		batchService:      batchService,
		logger:            logger,
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
func (s *Service) filterCredentials(providerID string, credentials []*models.Credential, token *models.RouterToken) []*models.Credential {
	if token == nil || token.Rules.AllowAllCredentials {
		return credentials
	}
	output := make([]*models.Credential, 0, len(credentials))
	for _, credential := range credentials {
		if token.Rules.AllowsCredential(providerID, credential.ID) {
			output = append(output, credential)
		}
	}
	return output
}

// BackendAttempt groups one backend pass inputs. Resolved identifies the
// backend; Creds carries the gated pool; Allowed carries the token
// allow-list plugins may use (nil means unrestricted).
type BackendAttempt struct {
	Resolved *provider.Resolved
	Creds    []*models.Credential
	Allowed  []string
}

func (s *Service) completeOne(ctx context.Context, attempt BackendAttempt, request *models.ChatCompletionRequest) (*models.ChatCompletionResponse, string, error) {
	resolved := attempt.Resolved
	allowed := attempt.Allowed
	credentials := attempt.Creds
	if resolved.IsLua() {
		response, err := s.providerService.LuaService().Complete(ctx, s.handlerMetadata(resolved, request.Model, allowed), request)
		return response, "", err
	}
	response, err := resolved.Go.Complete(ctx, credentials, request, resolved.Instance.Config)
	return response, "", err
}

func (s *Service) completeStreamOne(ctx context.Context, attempt BackendAttempt, request *models.ChatCompletionRequest, w io.Writer) (string, error) {
	resolved := attempt.Resolved
	if resolved.IsLua() {
		return "", s.providerService.LuaService().CompleteStream(ctx, s.handlerMetadata(resolved, request.Model, attempt.Allowed), request, w)
	}
	return "", resolved.Go.CompleteStream(ctx, attempt.Creds, request, w, resolved.Instance.Config)
}

// handlerMetadata builds the handler identity for one routed request: the provider
// instance and type serving it, the requested model, the provider config,
// and the token allow-list of credential IDs the plugin may use.
// A nil allow-list means unrestricted (admin probes).
func (s *Service) handlerMetadata(resolved *provider.Resolved, model models.ModelId, allowed []string) luaplugin.HandlerMeta {
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
	instance := resolved.Instance
	if resolved.IsLua() && !s.providerService.CredentialsEnabled(instance.TypeKey) {
		return nil, nil, nil
	}
	credentials, err := s.credentialService.All(instance.ID)
	if err != nil {
		s.logger.Debug("router: no available credentials", "provider_id", instance.ID, "reason", "no_available_credentials")
		return nil, nil, fmt.Errorf("%w for provider %q", apierrors.ErrNoCredential, instance.Name)
	}
	filtered := s.filterCredentials(instance.ID, credentials, effectiveToken(ctx, token))
	if len(filtered) == 0 {
		s.logger.Debug("router: credential pool empty after filtering",
			"provider_id", instance.ID, "model", model.String(), "reason", "empty_after_filter",
			"total", len(credentials))
		return nil, nil, fmt.Errorf("%w for provider %q", apierrors.ErrCredentialNotAllowed, instance.Name)
	}
	allowed := make([]string, 0, len(filtered))
	for _, credential := range filtered {
		allowed = append(allowed, credential.ID)
	}
	return allowed, filtered, nil
}

// checkEndpoint rejects the request when the cached model card declares an
// endpoint list excluding the target. Models absent from the cache pass:
// gating acts on declared data only, never on guesses.
func (s *Service) checkEndpoint(providerID, modelName, endpoint string) error {
	for _, info := range s.modelInfoService.PeekModelInfos(providerID) {
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
	enabled, err := s.modelInfoService.IsModelEnabled(providerID, modelName)
	if err != nil {
		return err
	}
	if !enabled {
		return fmt.Errorf("%w: %s", apierrors.ErrModelDisabled, model)
	}
	return nil
}

// requireCapability rejects requests when the backend lacks the handler.
// Lua types check the plugin handler table; Go types check the interface.
func (s *Service) requireCapability(resolved *provider.Resolved, handler string, goSupported bool, goMessage string) error {
	if resolved.IsLua() {
		if !s.providerService.LuaService().HasHandler(resolved.Instance.TypeKey, handler) {
			return fmt.Errorf("%w: provider %q has no %s handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name, handler)
		}
		return nil
	}
	if !goSupported {
		return fmt.Errorf("%w: %s", apierrors.ErrEndpointNotSupported, goMessage)
	}
	return nil
}

// mapHandlerNotFound converts a missing Lua handler into the shared
// endpoint error. Other errors pass through unchanged.
func mapHandlerNotFound(resolved *provider.Resolved, handler string, err error) error {
	if errors.Is(err, luaplugin.ErrHandlerNotFound) {
		return fmt.Errorf("%w: provider %q has no %s handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name, handler)
	}
	return err
}

// isCredentialAllowed reports whether id passes the allow-list. Nil means
// unrestricted (admin probes).
func isCredentialAllowed(allowed []string, id string) bool {
	if allowed == nil {
		return true
	}
	for _, a := range allowed {
		if a == id {
			return true
		}
	}
	return false
}

// dropMissingModel removes the model from the info cache when the backend
// reports it does not exist upstream. Best-effort: the original error is
// always returned untouched.
func (s *Service) dropMissingModel(providerID, modelName string, err error) {
	if err == nil {
		return
	}
	var providerError *models.ProviderError
	if !errors.As(err, &providerError) || providerError.Code != "not_found" {
		return
	}
	removed, removeError := s.modelInfoService.RemoveModel(providerID, modelName)
	if removeError != nil {
		s.logger.Warn("router: drop missing model from cache failed",
			"provider_id", providerID, "model", modelName, "error", removeError)
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
	request *models.ChatCompletionRequest,
	token *models.RouterToken,
) (*models.ChatCompletionResponse, error) {
	return s.complete(ctx, request, token, false)
}

// complete is Complete with an explicit admin-override bypass for probes:
// TestModel passes true so manually disabled models stay testable.
func (s *Service) complete(
	ctx context.Context,
	request *models.ChatCompletionRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.ChatCompletionResponse, error) {
	response, _, err := s.completeWithProxy(ctx, request, token, allowDisabled)
	return response, err
}

// completeWithProxy is complete plus a proxy placeholder kept for caller
// compatibility. Plugins own proxy selection in 0.7.0: it always returns "".
func (s *Service) completeWithProxy(
	ctx context.Context,
	request *models.ChatCompletionRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.ChatCompletionResponse, string, error) {
	ctx, resolvedRequest, err := s.resolveRequest(ctx, request.Model, token, allowDisabled, models.EndpointChatCompletions)
	if err != nil {
		return nil, "", err
	}
	resolved := resolvedRequest.resolved
	// Virtual agents resolve from the model name suffix and need no
	// credentials of their own; the outer token snapshot in ctx still
	// restricts the credentials of the member models tried inside.
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return s.completeOne(ctx, BackendAttempt{Resolved: resolved}, request)
	}
	allowed, credentials, err := s.gateCredentials(ctx, resolved, request.Model, token)
	if err != nil {
		return nil, "", err
	}
	response, _, err := s.completeOne(ctx, BackendAttempt{Resolved: resolved, Creds: credentials, Allowed: allowed}, request)
	s.dropMissingModel(resolvedRequest.providerID, resolvedRequest.modelName, err)
	return response, "", err
}

// CompleteStream routes a streaming chat completion request.
// Server-Sent Events are written directly to w.
func (s *Service) CompleteStream(
	ctx context.Context,
	request *models.ChatCompletionRequest,
	w io.Writer,
	token *models.RouterToken,
) error {
	ctx, resolvedRequest, err := s.resolveRequest(ctx, request.Model, token, false, models.EndpointChatCompletions)
	if err != nil {
		return err
	}
	resolved := resolvedRequest.resolved
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		_, err := s.completeStreamOne(ctx, BackendAttempt{Resolved: resolved}, request, w)
		return err
	}
	allowed, credentials, err := s.gateCredentials(ctx, resolved, request.Model, token)
	if err != nil {
		return err
	}
	_, err = s.completeStreamOne(ctx, BackendAttempt{Resolved: resolved, Creds: credentials, Allowed: allowed}, request, w)
	s.dropMissingModel(resolvedRequest.providerID, resolvedRequest.modelName, err)
	return err
}

// transcribeOne runs a single transcription pass. Lua plugins iterate
// credentials and proxies internally; Go adapters receive the gated pool.
func (s *Service) transcribeOne(ctx context.Context, resolved *provider.Resolved, credentials []*models.Credential, allowed []string, request *models.TranscriptionRequest) (*models.TranscriptionResponse, string, error) {
	if resolved.IsLua() {
		response, err := s.providerService.LuaService().Transcribe(ctx, s.handlerMetadata(resolved, request.Model, allowed), request)
		if err = mapHandlerNotFound(resolved, "transcribe", err); err != nil {
			return nil, "", err
		}
		return response, "", nil
	}
	transcriber, ok := resolved.Go.(provider.Transcriber)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support audio transcription", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	response, err := transcriber.Transcribe(ctx, credentials, request, resolved.Instance.Config)
	return response, "", err
}

// Transcribe routes a POST /v1/audio/transcriptions request in a single
// backend pass over the credential pool.
func (s *Service) Transcribe(
	ctx context.Context,
	request *models.TranscriptionRequest,
	token *models.RouterToken,
) (*models.TranscriptionResponse, error) {
	return s.transcribe(ctx, request, token, false)
}

func (s *Service) transcribe(
	ctx context.Context,
	request *models.TranscriptionRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.TranscriptionResponse, error) {
	response, _, err := s.transcribeWithProxy(ctx, request, token, allowDisabled)
	return response, err
}

// transcribeWithProxy is transcribe plus a proxy placeholder kept for
// caller compatibility. Plugins own proxy selection: it always returns "".
func (s *Service) transcribeWithProxy(
	ctx context.Context,
	request *models.TranscriptionRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.TranscriptionResponse, string, error) {
	ctx, resolvedRequest, err := s.resolveRequest(ctx, request.Model, token, allowDisabled, models.EndpointAudioTranscription)
	if err != nil {
		return nil, "", err
	}
	resolved := resolvedRequest.resolved
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return nil, "", fmt.Errorf("%w: virtual models do not serve audio transcription", apierrors.ErrEndpointNotSupported)
	}
	// Capability pre-check: fail loudly before touching the credential gate.
	_, goSupported := resolved.Go.(provider.Transcriber)
	if err := s.requireCapability(resolved, "transcribe", goSupported, fmt.Sprintf("provider %q does not support audio transcription", resolved.Instance.Name)); err != nil {
		return nil, "", err
	}
	allowed, credentials, err := s.gateCredentials(ctx, resolved, request.Model, token)
	if err != nil {
		return nil, "", err
	}
	response, _, err := s.transcribeOne(ctx, resolved, credentials, allowed, request)
	s.dropMissingModel(resolvedRequest.providerID, resolvedRequest.modelName, err)
	return response, "", err
}

// speechOne runs a single speech pass. Lua plugins iterate internally;
// Go adapters receive the gated pool.
func (s *Service) speechOne(ctx context.Context, resolved *provider.Resolved, credentials []*models.Credential, allowed []string, request *models.SpeechRequest) (*models.SpeechResponse, string, error) {
	if resolved.IsLua() {
		response, err := s.providerService.LuaService().Speech(ctx, s.handlerMetadata(resolved, request.Model, allowed), request)
		if err = mapHandlerNotFound(resolved, "speech", err); err != nil {
			return nil, "", err
		}
		return response, "", nil
	}
	speaker, ok := resolved.Go.(provider.Speaker)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support text-to-speech", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	response, err := speaker.Speech(ctx, credentials, request, resolved.Instance.Config)
	return response, "", err
}

// Speech routes a POST /v1/audio/speech request in a single backend pass
// over the credential pool.
func (s *Service) Speech(
	ctx context.Context,
	request *models.SpeechRequest,
	token *models.RouterToken,
) (*models.SpeechResponse, error) {
	return s.speech(ctx, request, token, false)
}

func (s *Service) speech(
	ctx context.Context,
	request *models.SpeechRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.SpeechResponse, error) {
	response, _, err := s.speechWithProxy(ctx, request, token, allowDisabled)
	return response, err
}

// speechWithProxy is speech plus a proxy placeholder kept for caller
// compatibility. Plugins own proxy selection: it always returns "".
func (s *Service) speechWithProxy(
	ctx context.Context,
	request *models.SpeechRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.SpeechResponse, string, error) {
	ctx, resolvedRequest, err := s.resolveRequest(ctx, request.Model, token, allowDisabled, models.EndpointAudioSpeech)
	if err != nil {
		return nil, "", err
	}
	resolved := resolvedRequest.resolved
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return nil, "", fmt.Errorf("%w: virtual models do not serve text-to-speech", apierrors.ErrEndpointNotSupported)
	}
	// Capability pre-check: fail loudly before touching the credential gate.
	_, goSupported := resolved.Go.(provider.Speaker)
	if err := s.requireCapability(resolved, "speech", goSupported, fmt.Sprintf("provider %q does not support text-to-speech", resolved.Instance.Name)); err != nil {
		return nil, "", err
	}
	allowed, credentials, err := s.gateCredentials(ctx, resolved, request.Model, token)
	if err != nil {
		return nil, "", err
	}
	response, _, err := s.speechOne(ctx, resolved, credentials, allowed, request)
	s.dropMissingModel(resolvedRequest.providerID, resolvedRequest.modelName, err)
	return response, "", err
}

// generateImageOne runs a single image generation pass. Lua plugins iterate
// internally; Go adapters receive the gated pool.
func (s *Service) generateImageOne(ctx context.Context, resolved *provider.Resolved, credentials []*models.Credential, allowed []string, request *models.ImageGenerationRequest) (*models.ImageGenerationResponse, string, error) {
	if resolved.IsLua() {
		response, err := s.providerService.LuaService().GenerateImage(ctx, s.handlerMetadata(resolved, request.Model, allowed), request)
		if err = mapHandlerNotFound(resolved, "generate_image", err); err != nil {
			return nil, "", err
		}
		return response, "", nil
	}
	imageGenerator, ok := resolved.Go.(provider.ImageGenerator)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support image generation", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	response, err := imageGenerator.GenerateImage(ctx, credentials, request, resolved.Instance.Config)
	return response, "", err
}

// GenerateImage routes a POST /v1/images/generations request in a single
// backend pass over the credential pool.
func (s *Service) GenerateImage(
	ctx context.Context,
	request *models.ImageGenerationRequest,
	token *models.RouterToken,
) (*models.ImageGenerationResponse, error) {
	return s.generateImage(ctx, request, token, false)
}

func (s *Service) generateImage(
	ctx context.Context,
	request *models.ImageGenerationRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.ImageGenerationResponse, error) {
	response, _, err := s.generateImageWithProxy(ctx, request, token, allowDisabled)
	return response, err
}

// generateImageWithProxy is generateImage plus a proxy placeholder kept
// for caller compatibility. Plugins own proxy selection: always "".
func (s *Service) generateImageWithProxy(
	ctx context.Context,
	request *models.ImageGenerationRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.ImageGenerationResponse, string, error) {
	ctx, resolvedRequest, err := s.resolveRequest(ctx, request.Model, token, allowDisabled, models.EndpointImagesGenerations)
	if err != nil {
		return nil, "", err
	}
	resolved := resolvedRequest.resolved
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return nil, "", fmt.Errorf("%w: virtual models do not serve image generation", apierrors.ErrEndpointNotSupported)
	}
	// Capability pre-check: fail loudly before touching the credential gate.
	_, goSupported := resolved.Go.(provider.ImageGenerator)
	if err := s.requireCapability(resolved, "generate_image", goSupported, fmt.Sprintf("provider %q does not support image generation", resolved.Instance.Name)); err != nil {
		return nil, "", err
	}
	allowed, credentials, err := s.gateCredentials(ctx, resolved, request.Model, token)
	if err != nil {
		return nil, "", err
	}
	response, _, err := s.generateImageOne(ctx, resolved, credentials, allowed, request)
	s.dropMissingModel(resolvedRequest.providerID, resolvedRequest.modelName, err)
	return response, "", err
}

// embedOne runs a single embeddings pass. Lua plugins iterate internally;
// Go adapters receive the gated pool.
func (s *Service) embedOne(ctx context.Context, resolved *provider.Resolved, credentials []*models.Credential, allowed []string, request *models.EmbeddingsRequest) (*models.EmbeddingsResponse, string, error) {
	if resolved.IsLua() {
		response, err := s.providerService.LuaService().Embed(ctx, s.handlerMetadata(resolved, request.Model, allowed), request)
		if err = mapHandlerNotFound(resolved, "embed", err); err != nil {
			return nil, "", err
		}
		return response, "", nil
	}
	embedder, ok := resolved.Go.(provider.Embedder)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support embeddings", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	response, err := embedder.Embed(ctx, credentials, request, resolved.Instance.Config)
	return response, "", err
}

// Embed routes a POST /v1/embeddings request in a single backend pass over
// the credential pool.
func (s *Service) Embed(
	ctx context.Context,
	request *models.EmbeddingsRequest,
	token *models.RouterToken,
) (*models.EmbeddingsResponse, error) {
	return s.embed(ctx, request, token, false)
}

func (s *Service) embed(
	ctx context.Context,
	request *models.EmbeddingsRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.EmbeddingsResponse, error) {
	response, _, err := s.embedWithProxy(ctx, request, token, allowDisabled)
	return response, err
}

// embedWithProxy is embed plus a proxy placeholder kept for caller
// compatibility. Plugins own proxy selection: it always returns "".
func (s *Service) embedWithProxy(
	ctx context.Context,
	request *models.EmbeddingsRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.EmbeddingsResponse, string, error) {
	ctx, resolvedRequest, err := s.resolveRequest(ctx, request.Model, token, allowDisabled, models.EndpointEmbeddings)
	if err != nil {
		return nil, "", err
	}
	resolved := resolvedRequest.resolved
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		return nil, "", fmt.Errorf("%w: virtual models do not serve embeddings", apierrors.ErrEndpointNotSupported)
	}
	// Capability pre-check: fail loudly before touching the credential gate.
	_, goSupported := resolved.Go.(provider.Embedder)
	if err := s.requireCapability(resolved, "embed", goSupported, fmt.Sprintf("provider %q does not support embeddings", resolved.Instance.Name)); err != nil {
		return nil, "", err
	}
	allowed, credentials, err := s.gateCredentials(ctx, resolved, request.Model, token)
	if err != nil {
		return nil, "", err
	}
	response, _, err := s.embedOne(ctx, resolved, credentials, allowed, request)
	s.dropMissingModel(resolvedRequest.providerID, resolvedRequest.modelName, err)
	return response, "", err
}

// GetProviderIDForModel returns the composite provider ID for a given model.
func (s *Service) GetProviderIDForModel(ctx context.Context, modelID models.ModelId) (string, error) {
	providerID, _, err := modelID.Parse()
	if err != nil {
		return "", err
	}
	_, err = s.providerService.Get(providerID)
	if err != nil {
		return "", err
	}
	return providerID, nil
}
