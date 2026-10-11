package v1

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

// maxVideoContentIndex caps the asset index of content downloads.
const maxVideoContentIndex = 64

var videoSizePattern = regexp.MustCompile(`^[1-9][0-9]*x[1-9][0-9]*$`)

// submitVideo handles POST /v1/videos
// @Summary      Submit video generation
// @Description  Submits a video generation request and returns a polling URL to check status.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        request body models.VideoGenerationRequest true "Video generation request"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      202 {object} models.VideoGenerationResponse "Request accepted"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Router       /v1/videos [post]
// @Security     BearerAuth
func (h *Handler) submitVideo(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	var request models.VideoGenerationRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if err := validateVideoRequest(&request); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}
	if deny := authorizeModel(token, request.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.message, deny.parameter)
		return
	}
	response, err := h.router.SubmitVideo(r.Context(), &request, token)
	duration := time.Since(start)
	h.recordRouteMetric(r.Context(), start, request.Model, token, duration, err, nil)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusAccepted, response)
}

// validateVideoRequest enforces edge validation for POST /v1/videos.
func validateVideoRequest(request *models.VideoGenerationRequest) error {
	if request.Model == "" {
		return fmt.Errorf("missing required field 'model'")
	}
	if strings.TrimSpace(request.Prompt) == "" && len(request.FrameImages) == 0 && len(request.InputReferences) == 0 {
		return fmt.Errorf("missing required field 'prompt' (or provide frame_images/input_references)")
	}
	if request.Duration < 0 {
		return fmt.Errorf("duration must be positive")
	}
	if request.Size != "" && !videoSizePattern.MatchString(request.Size) {
		return fmt.Errorf("invalid size %q: expected WIDTHxHEIGHT", request.Size)
	}
	for _, frame := range request.FrameImages {
		if frame.FrameType != "" && frame.FrameType != "first_frame" && frame.FrameType != "last_frame" {
			return fmt.Errorf("invalid frame_type %q: expected first_frame or last_frame", frame.FrameType)
		}
		if frame.ImageURL == nil || strings.TrimSpace(frame.ImageURL.URL) == "" {
			return fmt.Errorf("frame_images entries require image_url.url")
		}
	}
	for _, reference := range request.InputReferences {
		switch reference.Type {
		case "image_url", "audio_url", "video_url":
		default:
			return fmt.Errorf("invalid input reference type %q: expected image_url, audio_url or video_url", reference.Type)
		}
	}
	if request.CallbackURL != "" && !strings.HasPrefix(request.CallbackURL, "https://") {
		return fmt.Errorf("callback_url must be an HTTPS URL")
	}
	return nil
}

// pollVideo handles GET /v1/videos/{jobId}
// @Summary      Poll video generation status
// @Description  Returns job status and content URLs when completed.
// @Tags         OpenAI API
// @Produce      json
// @Param        jobId path string true "Video job ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.VideoGenerationResponse "Job status"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown job"
// @Router       /v1/videos/{jobId} [get]
// @Security     BearerAuth
func (h *Handler) pollVideo(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	jobID := r.PathValue("jobId")
	if jobID == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "job id is required", nil)
		return
	}
	response, err := h.router.PollVideo(r.Context(), jobID, token)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.recordRouteMetric(r.Context(), start, "", token, time.Since(start), nil, nil)
	h.writeJSON(w, http.StatusOK, response)
}

// videoContent handles GET /v1/videos/{jobId}/content
// @Summary      Download generated video content
// @Description  Streams the generated video asset from the upstream provider.
// @Tags         OpenAI API
// @Produce      video/mp4
// @Param        jobId path string true "Video job ID"
// @Param        index query int false "Asset index (default 0)"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 "Video bytes"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown job"
// @Router       /v1/videos/{jobId}/content [get]
// @Security     BearerAuth
func (h *Handler) videoContent(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	jobID := r.PathValue("jobId")
	if jobID == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "job id is required", nil)
		return
	}
	index := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("index")); raw != "" {
		parsedIndex, err := strconv.Atoi(raw)
		if err != nil || parsedIndex < 0 || parsedIndex > maxVideoContentIndex {
			h.writeError(w, http.StatusBadRequest, "invalid_request_error",
				fmt.Sprintf("invalid index %q: expected 0..%d", raw, maxVideoContentIndex), nil)
			return
		}
		index = parsedIndex
	}
	response, err := h.router.VideoContent(r.Context(), jobID, index, token)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.recordRouteMetric(r.Context(), start, "", token, time.Since(start), nil, nil)
	w.Header().Set("Content-Type", models.VideoContentType(response.ContentType))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(response.Video)
}

// listVideoModels handles GET /v1/videos/models - token-authenticated
// listing. Entries derive from the cached model catalog filtered to
// the videos endpoint; capability details stay null when unreported.
// @Summary      List video models
// @Description  Lists models serving the videos endpoint as provider/model ids.
// @Tags         OpenAI API
// @Produce      json
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.VideoModelsListResponse "Video model list"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Router       /v1/videos/models [get]
// @Security     BearerAuth
func (h *Handler) listVideoModels(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	byFullID := map[string]modelinfo.ModelView{}
	var entries []models.VideoModel
	if h.providerSvc != nil && h.modelInfoSvc != nil {
		if providers, err := h.providerSvc.List(r.Context()); err == nil {
			for _, providerInstance := range providers {
				if providerInstance.TypeKey == provider.TypeVirtual || providerInstance.Disabled {
					continue
				}
				modelViews, err := h.modelInfoSvc.MergedView(r.Context(), providerInstance.ID)
				if err != nil {
					continue
				}
				for _, memberView := range modelViews {
					if memberView.Disabled {
						continue
					}
					byFullID[providerInstance.ID+"/"+memberView.Name] = memberView
					if !memberView.SupportsEndpoint(models.EndpointVideos) {
						continue
					}
					entries = append(entries, models.VideoModel{
						ID:            providerInstance.ID + "/" + memberView.Name,
						CanonicalSlug: providerInstance.ID + "/" + memberView.Name,
						Name:          memberView.DisplayName,
						Created:       providerInstance.CreatedAt.Unix(),
						Description:   memberView.Description,
					})
				}
			}
		}
	}
	if h.virtualSvc != nil {
		if agents, err := h.virtualSvc.List(r.Context()); err == nil {
			var mutex sync.Mutex
			var resolved []*models.VirtualModel
			var waitGroup sync.WaitGroup
			for _, agent := range agents {
				if agent.Disabled {
					continue
				}
				waitGroup.Add(1)
				go func(agent *models.VirtualModel) {
					defer waitGroup.Done()
					serves := false
					for _, entry := range agent.Models {
						memberView, ok := byFullID[string(entry.ModelID)]
						if !ok {
							continue
						}
						if memberView.SupportsEndpoint(models.EndpointVideos) {
							serves = true
							break
						}
					}
					if !serves {
						return
					}
					mutex.Lock()
					resolved = append(resolved, agent)
					mutex.Unlock()
				}(agent)
			}
			waitGroup.Wait()
			for _, agent := range resolved {
				entries = append(entries, models.VideoModel{
					ID:            provider.TypeVirtual + "/" + agent.ID,
					CanonicalSlug: provider.TypeVirtual + "/" + agent.ID,
					Name:          agent.Name,
					Created:       agent.CreatedAt.Unix(),
					Description:   agent.Description,
				})
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	if entries == nil {
		entries = []models.VideoModel{}
	}
	h.writeJSON(w, http.StatusOK, models.VideoModelsListResponse{Data: entries})
}
