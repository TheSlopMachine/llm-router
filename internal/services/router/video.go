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
func (s *Service) submitVideoOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, allowed []string, req *models.VideoGenerationRequest) (*models.VideoGenerationResponse, string, error) {
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		vs, ok := resolved.Go.(provider.VideoSubmitter)
		if !ok {
			return nil, "", fmt.Errorf("%w: virtual backend does not serve video generation", apierrors.ErrEndpointNotSupported)
		}
		resp, err := vs.SubmitVideo(ctx, creds, req, resolved.Instance.Config)
		return resp, "", err
	}
	if resolved.IsLua() {
		resp, err := s.providerSvc.LuaService().SubmitVideo(ctx, s.meta(resolved, req.Model, allowed), req)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, "", fmt.Errorf("%w: provider %q has no generate_video handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return resp, "", err
	}
	vg, ok := resolved.Go.(provider.VideoGenerator)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support video generation", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	resp, err := vg.SubmitVideo(ctx, creds, req, resolved.Instance.Config)
	return resp, "", err
}

// SubmitVideo routes a POST /v1/videos request in a single backend pass
// over the credential pool and persists the router-side job row mapping
// the local job ID to the upstream one.
func (s *Service) SubmitVideo(
	ctx context.Context,
	req *models.VideoGenerationRequest,
	token *models.RouterToken,
) (*models.VideoGenerationResponse, error) {
	return s.submitVideo(ctx, req, token, false)
}

func (s *Service) submitVideo(
	ctx context.Context,
	req *models.VideoGenerationRequest,
	token *models.RouterToken,
	allowDisabled bool,
) (*models.VideoGenerationResponse, error) {
	if s.videoJobsSvc == nil {
		return nil, fmt.Errorf("video generation unavailable: job store not wired")
	}
	if req.PreviousJobID != "" {
		prev, err := s.videoJobsSvc.Get(req.PreviousJobID)
		if err != nil {
			return nil, &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: fmt.Sprintf("unknown previous_job_id %q", req.PreviousJobID)}
		}
		if prev.UpstreamJobID == "" {
			return nil, &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: "previous job has no upstream id"}
		}
		continued := *req
		continued.PreviousJobID = prev.UpstreamJobID
		req = &continued
	}
	ctx, rr, err := s.resolveRequest(ctx, req.Model, token, allowDisabled, models.EndpointVideos)
	if err != nil {
		return nil, err
	}
	resolved := rr.resolved
	if resolved.Instance.TypeKey == provider.TypeVirtual {
		resp, _, err := s.submitVideoOne(ctx, resolved, nil, nil, req)
		return resp, err
	}
	// Capability pre-check: fail loudly before touching the credential gate.
	if resolved.IsLua() {
		if !s.providerSvc.LuaService().HasHandler(resolved.Instance.TypeKey, "generate_video") {
			return nil, fmt.Errorf("%w: provider %q has no generate_video handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
	} else if _, ok := resolved.Go.(provider.VideoGenerator); !ok {
		return nil, fmt.Errorf("%w: provider %q does not support video generation", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	allowed, creds, err := s.gateCredentials(ctx, resolved, req.Model, token)
	if err != nil {
		return nil, err
	}
	resp, _, err := s.submitVideoOne(ctx, resolved, creds, allowed, req)
	s.dropMissingModel(rr.providerID, rr.modelName, err)
	if err != nil {
		return nil, err
	}
	job := &models.VideoJob{
		ID:            models.NewVideoJobID(time.Now()),
		Model:         req.Model,
		BackendModel:  req.Model,
		ProviderID:    rr.providerID,
		TypeKey:       resolved.Instance.TypeKey,
		UpstreamJobID: resp.ID,
		Status:        resp.Status,
	}
	if err := s.videoJobsSvc.Create(job); err != nil {
		return nil, err
	}
	out := *resp
	out.ID = job.ID
	out.PollingURL = "/v1/videos/" + job.ID
	return &out, nil
}

// pollVideoOne runs a single video status poll. Lua plugins iterate
// internally; Go adapters receive the gated pool.
func (s *Service) pollVideoOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, allowed []string, backendModel models.ModelId, upstreamJobID string) (*models.VideoGenerationResponse, string, error) {
	if resolved.IsLua() {
		resp, err := s.providerSvc.LuaService().PollVideo(ctx, s.meta(resolved, backendModel, allowed), backendModel, upstreamJobID)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, "", fmt.Errorf("%w: provider %q has no poll_video handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return resp, "", err
	}
	vg, ok := resolved.Go.(provider.VideoGenerator)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support video generation", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	resp, err := vg.PollVideo(ctx, creds, backendModel, upstreamJobID, resolved.Instance.Config)
	return resp, "", err
}

// PollVideo routes a GET /v1/videos/{jobId} request: the stored job row
// supplies the backend model and provider, the credential pool supplies
// fresh credentials for the upstream poll.
func (s *Service) PollVideo(
	ctx context.Context,
	jobID string,
	token *models.RouterToken,
) (*models.VideoGenerationResponse, error) {
	if s.videoJobsSvc == nil {
		return nil, fmt.Errorf("video generation unavailable: job store not wired")
	}
	job, err := s.videoJobsSvc.Get(jobID)
	if err != nil {
		return nil, err
	}
	if deny := tokenDenies(token, job.Model); deny != nil {
		return nil, deny
	}
	ctx, rr, err := s.resolveRequest(ctx, job.BackendModel, token, false, models.EndpointVideos)
	if err != nil {
		return nil, err
	}
	resolved := rr.resolved
	allowed, creds, err := s.gateCredentials(ctx, resolved, job.BackendModel, token)
	if err != nil {
		return nil, err
	}
	resp, _, err := s.pollVideoOne(ctx, resolved, creds, allowed, job.BackendModel, job.UpstreamJobID)
	s.dropMissingModel(rr.providerID, rr.modelName, err)
	if err != nil {
		return nil, err
	}
	if resp.Status != job.Status {
		if serr := s.videoJobsSvc.SetStatus(job.ID, resp.Status); serr != nil && s.logger != nil {
			s.logger.Warn("router: video job status persist failed", "job_id", job.ID, "error", serr)
		}
	}
	out := *resp
	out.ID = job.ID
	out.PollingURL = "/v1/videos/" + job.ID
	return &out, nil
}

// videoContentOne runs a single video asset download. Lua plugins iterate
// internally; Go adapters receive the gated pool.
func (s *Service) videoContentOne(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, allowed []string, backendModel models.ModelId, upstreamJobID string, index int) (*models.VideoContentResponse, string, error) {
	if resolved.IsLua() {
		resp, err := s.providerSvc.LuaService().VideoContent(ctx, s.meta(resolved, backendModel, allowed), backendModel, upstreamJobID, index)
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, "", fmt.Errorf("%w: provider %q has no video_content handler", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
		}
		return resp, "", err
	}
	vg, ok := resolved.Go.(provider.VideoGenerator)
	if !ok {
		return nil, "", fmt.Errorf("%w: provider %q does not support video generation", apierrors.ErrEndpointNotSupported, resolved.Instance.Name)
	}
	resp, err := vg.VideoContent(ctx, creds, backendModel, upstreamJobID, index, resolved.Instance.Config)
	return resp, "", err
}

// VideoContent routes a GET /v1/videos/{jobId}/content request.
func (s *Service) VideoContent(
	ctx context.Context,
	jobID string,
	index int,
	token *models.RouterToken,
) (*models.VideoContentResponse, error) {
	if s.videoJobsSvc == nil {
		return nil, fmt.Errorf("video generation unavailable: job store not wired")
	}
	job, err := s.videoJobsSvc.Get(jobID)
	if err != nil {
		return nil, err
	}
	if deny := tokenDenies(token, job.Model); deny != nil {
		return nil, deny
	}
	ctx, rr, err := s.resolveRequest(ctx, job.BackendModel, token, false, models.EndpointVideos)
	if err != nil {
		return nil, err
	}
	resolved := rr.resolved
	allowed, creds, err := s.gateCredentials(ctx, resolved, job.BackendModel, token)
	if err != nil {
		return nil, err
	}
	resp, _, err := s.videoContentOne(ctx, resolved, creds, allowed, job.BackendModel, job.UpstreamJobID, index)
	s.dropMissingModel(rr.providerID, rr.modelName, err)
	if err != nil {
		return nil, err
	}
	if resp.ContentType == "" {
		resp.ContentType = models.VideoContentType("")
	}
	return resp, nil
}
