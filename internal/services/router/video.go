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

// tokenDenies enforces token rules for a stored video job model. Nil
// tokens skip checks; denials map to the shared provider/model sentinels.
func tokenDenies(token *models.RouterToken, model models.ModelId) error {
	if token == nil || model == "" {
		return nil
	}
	providerID, _, _ := model.Parse()
	if providerID != "" && !token.Rules.AllowsProvider(providerID) {
		return fmt.Errorf("%w: %s", apierrors.ErrProviderNotAllowed, providerID)
	}
	if !token.Rules.Allows(model) {
		return fmt.Errorf("%w: %s", apierrors.ErrModelNotAllowed, model)
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
		return nil, fmt.Errorf("video generation unavailable: job store not wired")
	}
	if request.PreviousJobID != "" {
		prev, err := s.videoJobsService.Get(ctx, request.PreviousJobID)
		if err != nil {
			return nil, &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: fmt.Sprintf("unknown previous_job_id %q", request.PreviousJobID)}
		}
		if prev.UpstreamJobID == "" {
			return nil, &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: "previous job has no upstream id"}
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
		return response, err
	}
	// Capability pre-check: fail loudly before touching the credential gate.
	_, goSupported := resolved.Go.(provider.VideoGenerator)
	if err := s.requireCapability(resolved, "generate_video", goSupported, fmt.Sprintf("provider %q does not support video generation", resolved.Instance.Name)); err != nil {
		return nil, err
	}
	allowed, credentials, err := s.gateCredentials(ctx, resolved, request.Model, token)
	if err != nil {
		return nil, err
	}
	response, _, err := s.submitVideoOne(ctx, resolved, credentials, allowed, request)
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
		return nil, fmt.Errorf("video generation unavailable: job store not wired")
	}
	job, err := s.videoJobsService.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if deny := tokenDenies(token, job.Model); deny != nil {
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
	s.dropMissingModel(ctx, resolvedRequest.providerID, resolvedRequest.modelName, err)
	if err != nil {
		return nil, err
	}
	if response.Status != job.Status {
		if statusError := s.videoJobsService.SetStatus(ctx, job.ID, response.Status); statusError != nil && s.logger != nil {
			s.logger.Warn("router: video job status persist failed", "job_id", job.ID, "error", statusError)
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
		return nil, fmt.Errorf("video generation unavailable: job store not wired")
	}
	job, err := s.videoJobsService.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if deny := tokenDenies(token, job.Model); deny != nil {
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
	s.dropMissingModel(ctx, resolvedRequest.providerID, resolvedRequest.modelName, err)
	if err != nil {
		return nil, err
	}
	if response.ContentType == "" {
		response.ContentType = models.VideoContentType("")
	}
	return response, nil
}
