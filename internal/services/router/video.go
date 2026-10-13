package router

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

// tokenDenies enforces token rules for a stored record model. Nil tokens
// skip checks; denials map to the shared provider/model sentinels. An empty
// jobID omits the job key: only video polls carry a job.
func (s *Service) tokenDenies(token *models.RouterToken, model models.ModelId, jobID string) error {
	if token == nil || model == "" {
		return nil
	}
	providerID, _, _ := model.Parse()
	if providerID != "" && !token.Rules.AllowsProvider(providerID) {
		deny := fmt.Errorf("%w: %s", apierrors.ErrProviderNotAllowed, providerID)
		arguments := []any{
			"model", model.String(),
			"token_id", tokenIdentifier(token),
			"reason", "provider_not_allowed",
			"error", deny,
		}
		if jobID != "" {
			arguments = append(arguments, "job", jobID)
		}
		s.warn("Denying stored record access by token rules", arguments...)
		return deny
	}
	if !token.Rules.Allows(model) {
		deny := fmt.Errorf("%w: %s", apierrors.ErrModelNotAllowed, model)
		arguments := []any{
			"model", model.String(),
			"token_id", tokenIdentifier(token),
			"reason", "model_not_allowed",
			"error", deny,
		}
		if jobID != "" {
			arguments = append(arguments, "job", jobID)
		}
		s.warn("Denying stored record access by token rules", arguments...)
		return deny
	}
	return nil
}

// submitVideoOne runs a single video submit pass. Lua plugins iterate
// internally; Go adapters receive the gated pool.
func (s *Service) submitVideoOne(ctx context.Context, resolved *provider.Resolved, credentials []*models.Credential, allowed []string, request *models.VideoGenerationRequest) (*models.VideoGenerationResponse, string, error) {
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		submitter, ok := resolved.Go.(provider.VideoSubmitter)
		if !ok {
			return nil, "", fmt.Errorf("%w: virtual backend does not serve video generation", apierrors.ErrEndpointNotSupported)
		}
		s.logBuiltInBackend(resolved, request.Model, len(credentials), allowed == nil)
		response, err := submitter.SubmitVideo(ctx, credentials, request, resolved.Instance.Config)
		return response, "", err
	}
	if resolved.IsLua() {
		response, err := s.providerService.LuaService().SubmitVideo(ctx, s.handlerMetadata(resolved, request.Model, allowed), request)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, "", fmt.Errorf("%w: provider %q has no generate_video handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return response, "", err
	}
	generator, ok := resolved.Go.(provider.VideoGenerator)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support video generation", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	s.logBuiltInBackend(resolved, request.Model, len(credentials), allowed == nil)
	response, err := generator.SubmitVideo(ctx, credentials, request, resolved.Instance.Config)
	return response, "", err
}

// SubmitVideo routes a POST /v1/videos request in a single backend pass
// over the credential pool and persists the router-side job row mapping
// the local job ID to the upstream one.
func (s *Service) SubmitVideo(
	ctx context.Context,
	request *models.VideoGenerationRequest,
	token *models.RouterToken,
) (*models.VideoGenerationResponse, error) {
	return s.submitVideo(ctx, request, token, false)
}

func (s *Service) submitVideo(
	ctx context.Context,
	request *models.VideoGenerationRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.VideoGenerationResponse, error) {
	if s.videoJobsService == nil {
		err := fmt.Errorf("video generation unavailable: job store not wired")
		s.logError("Rejecting video request without job store",
			"model", request.Model.String(), "endpoint", models.EndpointVideos,
			"reason", "store_not_wired", "token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if request.PreviousJobID != "" {
		prev, err := s.videoJobsService.Get(ctx, request.PreviousJobID)
		if err != nil {
			deny := &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: fmt.Sprintf("unknown previous_job_id %q", request.PreviousJobID)}
			s.debug("Denying video request with unknown previous job",
				"model", request.Model.String(), "endpoint", models.EndpointVideos,
				"reason", "unknown_previous_job", "token_id", tokenIdentifier(token),
				"job", request.PreviousJobID, "error", err)
			return nil, deny
		}
		if prev.UpstreamJobID == "" {
			deny := &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: "previous job has no upstream id"}
			s.debug("Denying video request without upstream job",
				"model", request.Model.String(), "endpoint", models.EndpointVideos,
				"reason", "previous_job_missing_upstream", "token_id", tokenIdentifier(token),
				"job", request.PreviousJobID)
			return nil, deny
		}
		continued := *request
		continued.PreviousJobID = prev.UpstreamJobID
		request = &continued
	}
	ctx, resolvedRequest, err := s.resolveRequest(ctx, request.Model, token, allowDisabled, models.EndpointVideos)
	if err != nil {
		return nil, err
	}
	resolved := resolvedRequest.resolved
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		response, _, err := s.submitVideoOne(ctx, resolved, nil, nil, request)
		if err != nil {
			s.logError("Video submit request fails",
				"provider_id", resolvedRequest.providerID, "type", resolved.Instance.TypeKey,
				"model", request.Model.String(), "endpoint", models.EndpointVideos,
				"token_id", tokenIdentifier(token), "error", err)
		}
		return response, err
	}
	// Capability pre-check: fail loudly before touching the credential gate.
	_, goSupported := resolved.Go.(provider.VideoGenerator)
	if err := s.requireCapability(resolved, "generate_video", goSupported, fmt.Sprintf("provider %q does not support video generation", resolved.Instance.Name)); err != nil {
		s.debug("Denying request without backend capability",
			"provider_id", resolvedRequest.providerID, "type", resolved.Instance.TypeKey,
			"model", request.Model.String(), "endpoint", models.EndpointVideos,
			"reason", "missing_handler", "token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	allowed, credentials, err := s.gateCredentials(ctx, resolved, request.Model, token)
	if err != nil {
		return nil, err
	}
	response, _, err := s.submitVideoOne(ctx, resolved, credentials, allowed, request)
	if err != nil {
		s.logError("Video submit request fails",
			"provider_id", resolvedRequest.providerID, "type", resolved.Instance.TypeKey,
			"model", request.Model.String(), "endpoint", models.EndpointVideos,
			"token_id", tokenIdentifier(token), "error", err)
	}
	s.dropMissingModel(ctx, resolvedRequest.providerID, resolvedRequest.modelName, err)
	if err != nil {
		return nil, err
	}
	job := &models.VideoJob{
		ID:            models.NewVideoJobID(time.Now()),
		Model:         request.Model,
		BackendModel:  request.Model,
		ProviderID:    resolvedRequest.providerID,
		TypeKey:       resolved.Instance.TypeKey,
		UpstreamJobID: response.ID,
		Status:        response.Status,
	}
	if err := s.videoJobsService.Create(ctx, job); err != nil {
		s.logError("Persisting video job fails",
			"provider_id", resolvedRequest.providerID, "type", resolved.Instance.TypeKey,
			"model", request.Model.String(), "endpoint", models.EndpointVideos,
			"operation", "submit_video", "job", job.ID, "detail", response.ID, "error", err)
		return nil, err
	}
	output := *response
	output.ID = job.ID
	output.PollingURL = "/v1/videos/" + job.ID
	return &output, nil
}

// pollVideoOne runs a single video status poll. Lua plugins iterate
// internally; Go adapters receive the gated pool.
func (s *Service) pollVideoOne(ctx context.Context, resolved *provider.Resolved, credentials []*models.Credential, allowed []string, backendModel models.ModelId, upstreamJobID string) (*models.VideoGenerationResponse, string, error) {
	if resolved.IsLua() {
		response, err := s.providerService.LuaService().PollVideo(ctx, s.handlerMetadata(resolved, backendModel, allowed), backendModel, upstreamJobID)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, "", fmt.Errorf("%w: provider %q has no poll_video handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return response, "", err
	}
	generator, ok := resolved.Go.(provider.VideoGenerator)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support video generation", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	s.logBuiltInBackend(resolved, backendModel, len(credentials), allowed == nil)
	response, err := generator.PollVideo(ctx, credentials, backendModel, upstreamJobID, resolved.Instance.Config)
	return response, "", err
}

// PollVideo routes a GET /v1/videos/{jobId} request: the stored job row
// supplies the backend model and provider, the credential pool supplies
// fresh credentials for the upstream poll.
func (s *Service) PollVideo(
	ctx context.Context,
	jobID string,
	token *models.RouterToken,
) (*models.VideoGenerationResponse, error) {
	if s.videoJobsService == nil {
		err := fmt.Errorf("video generation unavailable: job store not wired")
		s.logError("Rejecting video poll without job store",
			"endpoint", models.EndpointVideos, "operation", "poll_video",
			"reason", "store_not_wired", "token_id", tokenIdentifier(token),
			"job", jobID, "error", err)
		return nil, err
	}
	job, err := s.videoJobsService.Get(ctx, jobID)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored video job misses",
				"endpoint", models.EndpointVideos, "operation", "poll_video",
				"token_id", tokenIdentifier(token), "job", jobID, "error", err)
		} else {
			s.logError("Loading stored video job fails",
				"endpoint", models.EndpointVideos, "operation", "poll_video",
				"token_id", tokenIdentifier(token), "job", jobID, "error", err)
		}
		return nil, err
	}
	if deny := s.tokenDenies(token, job.Model, job.ID); deny != nil {
		return nil, deny
	}
	ctx, resolvedRequest, err := s.resolveRequest(ctx, job.BackendModel, token, false, models.EndpointVideos)
	if err != nil {
		return nil, err
	}
	resolved := resolvedRequest.resolved
	allowed, credentials, err := s.gateCredentials(ctx, resolved, job.BackendModel, token)
	if err != nil {
		return nil, err
	}
	response, _, err := s.pollVideoOne(ctx, resolved, credentials, allowed, job.BackendModel, job.UpstreamJobID)
	if err != nil {
		s.logError("Video poll request fails",
			"provider_id", resolvedRequest.providerID, "type", resolved.Instance.TypeKey,
			"model", job.BackendModel.String(), "endpoint", models.EndpointVideos,
			"token_id", tokenIdentifier(token), "job", job.ID, "error", err)
	}
	s.dropMissingModel(ctx, resolvedRequest.providerID, resolvedRequest.modelName, err)
	if err != nil {
		return nil, err
	}
	if response.Status != job.Status {
		if statusError := s.videoJobsService.SetStatus(ctx, job.ID, response.Status); statusError != nil {
			s.logError("Persisting video job status fails",
				"endpoint", models.EndpointVideos, "operation", "poll_video",
				"model", job.BackendModel.String(), "job", job.ID, "error", statusError)
		}
	}
	output := *response
	output.ID = job.ID
	output.PollingURL = "/v1/videos/" + job.ID
	return &output, nil
}

// videoContentOne runs a single video asset download. Lua plugins iterate
// internally; Go adapters receive the gated pool.
func (s *Service) videoContentOne(ctx context.Context, resolved *provider.Resolved, credentials []*models.Credential, allowed []string, backendModel models.ModelId, upstreamJobID string, index int) (*models.VideoContentResponse, string, error) {
	if resolved.IsLua() {
		response, err := s.providerService.LuaService().VideoContent(ctx, s.handlerMetadata(resolved, backendModel, allowed), backendModel, upstreamJobID, index)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, "", fmt.Errorf("%w: provider %q has no video_content handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return response, "", err
	}
	generator, ok := resolved.Go.(provider.VideoGenerator)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support video generation", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	s.logBuiltInBackend(resolved, backendModel, len(credentials), allowed == nil)
	response, err := generator.VideoContent(ctx, credentials, backendModel, upstreamJobID, index, resolved.Instance.Config)
	return response, "", err
}

// VideoContent routes a GET /v1/videos/{jobId}/content request.
func (s *Service) VideoContent(
	ctx context.Context,
	jobID string,
	index int,
	token *models.RouterToken,
) (*models.VideoContentResponse, error) {
	if s.videoJobsService == nil {
		err := fmt.Errorf("video generation unavailable: job store not wired")
		s.logError("Rejecting video content without job store",
			"endpoint", models.EndpointVideos, "operation", "video_content",
			"reason", "store_not_wired", "token_id", tokenIdentifier(token),
			"job", jobID, "error", err)
		return nil, err
	}
	job, err := s.videoJobsService.Get(ctx, jobID)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored video job misses",
				"endpoint", models.EndpointVideos, "operation", "video_content",
				"token_id", tokenIdentifier(token), "job", jobID, "error", err)
		} else {
			s.logError("Loading stored video job fails",
				"endpoint", models.EndpointVideos, "operation", "video_content",
				"token_id", tokenIdentifier(token), "job", jobID, "error", err)
		}
		return nil, err
	}
	if deny := s.tokenDenies(token, job.Model, job.ID); deny != nil {
		return nil, deny
	}
	ctx, resolvedRequest, err := s.resolveRequest(ctx, job.BackendModel, token, false, models.EndpointVideos)
	if err != nil {
		return nil, err
	}
	resolved := resolvedRequest.resolved
	allowed, credentials, err := s.gateCredentials(ctx, resolved, job.BackendModel, token)
	if err != nil {
		return nil, err
	}
	response, _, err := s.videoContentOne(ctx, resolved, credentials, allowed, job.BackendModel, job.UpstreamJobID, index)
	if err != nil {
		s.logError("Video content request fails",
			"provider_id", resolvedRequest.providerID, "type", resolved.Instance.TypeKey,
			"model", job.BackendModel.String(), "endpoint", models.EndpointVideos,
			"token_id", tokenIdentifier(token), "job", job.ID, "error", err)
	}
	s.dropMissingModel(ctx, resolvedRequest.providerID, resolvedRequest.modelName, err)
	if err != nil {
		return nil, err
	}
	if response.ContentType == "" {
		response.ContentType = models.VideoContentType("")
	}
	return response, nil
}
