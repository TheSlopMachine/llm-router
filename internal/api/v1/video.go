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
// @Success      202 {object} models.VideoGenerationResponse "Request accepted"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Router       /v1/videos [post]
// @Security     BearerAuth
func (h *Handler) submitVideo(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	var req models.VideoGenerationRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if err := validateVideoRequest(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}
	if deny := authorizeModel(t, req.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.msg, deny.param)
		return
	}
	resp, err := h.router.SubmitVideo(r.Context(), &req, t)
	duration := time.Since(start)
	h.recordRouteMetric(r.Context(), start, req.Model, t, duration, err, nil)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusAccepted, resp)
}

// validateVideoRequest enforces edge validation for POST /v1/videos.
func validateVideoRequest(req *models.VideoGenerationRequest) error {
	if req.Model == "" {
		return fmt.Errorf("missing required field 'model'")
	}
	if strings.TrimSpace(req.Prompt) == "" && len(req.FrameImages) == 0 && len(req.InputReferences) == 0 {
		return fmt.Errorf("missing required field 'prompt' (or provide frame_images/input_references)")
	}
	if req.Duration < 0 {
		return fmt.Errorf("duration must be positive")
	}
	if req.Size != "" && !videoSizePattern.MatchString(req.Size) {
		return fmt.Errorf("invalid size %q: expected WIDTHxHEIGHT", req.Size)
	}
	for _, f := range req.FrameImages {
		if f.FrameType != "" && f.FrameType != "first_frame" && f.FrameType != "last_frame" {
			return fmt.Errorf("invalid frame_type %q: expected first_frame or last_frame", f.FrameType)
		}
		if f.ImageURL == nil || strings.TrimSpace(f.ImageURL.URL) == "" {
			return fmt.Errorf("frame_images entries require image_url.url")
		}
	}
	for _, ref := range req.InputReferences {
		switch ref.Type {
		case "image_url", "audio_url", "video_url":
		default:
			return fmt.Errorf("invalid input reference type %q: expected image_url, audio_url or video_url", ref.Type)
		}
	}
	if req.CallbackURL != "" && !strings.HasPrefix(req.CallbackURL, "https://") {
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
// @Success      200 {object} models.VideoGenerationResponse "Job status"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown job"
// @Router       /v1/videos/{jobId} [get]
// @Security     BearerAuth
func (h *Handler) pollVideo(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	jobID := r.PathValue("jobId")
	if jobID == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "job id is required", nil)
		return
	}
	resp, err := h.router.PollVideo(r.Context(), jobID, t)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.recordRouteMetric(r.Context(), start, "", t, time.Since(start), nil, nil)
	h.writeJSON(w, http.StatusOK, resp)
}

// videoContent handles GET /v1/videos/{jobId}/content
// @Summary      Download generated video content
// @Description  Streams the generated video asset from the upstream provider.
// @Tags         OpenAI API
// @Produce      video/mp4
// @Param        jobId path string true "Video job ID"
// @Param        index query int false "Asset index (default 0)"
// @Success      200 "Video bytes"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown job"
// @Router       /v1/videos/{jobId}/content [get]
// @Security     BearerAuth
func (h *Handler) videoContent(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	jobID := r.PathValue("jobId")
	if jobID == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "job id is required", nil)
		return
	}
	index := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("index")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > maxVideoContentIndex {
			h.writeError(w, http.StatusBadRequest, "invalid_request_error",
				fmt.Sprintf("invalid index %q: expected 0..%d", raw, maxVideoContentIndex), nil)
			return
		}
		index = n
	}
	resp, err := h.router.VideoContent(r.Context(), jobID, index, t)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.recordRouteMetric(r.Context(), start, "", t, time.Since(start), nil, nil)
	w.Header().Set("Content-Type", models.VideoContentType(resp.ContentType))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp.Video)
}

// listVideoModels handles GET /v1/videos/models - global, independent of
// token rules. Entries derive from the cached model catalog filtered to
// the videos endpoint; capability details stay null when unreported.
func (h *Handler) listVideoModels(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	byFullID := map[string]modelinfo.ModelView{}
	var entries []models.VideoModel
	if h.providerSvc != nil && h.modelInfoSvc != nil {
		if providers, err := h.providerSvc.List(); err == nil {
			for _, p := range providers {
				if p.TypeKey == provider.TypeVirtual || p.Disabled {
					continue
				}
				infos, err := h.modelInfoSvc.MergedView(r.Context(), p.ID)
				if err != nil {
					continue
				}
				for _, mi := range infos {
					if mi.Disabled {
						continue
					}
					byFullID[p.ID+"/"+mi.Name] = mi
					if !mi.SupportsEndpoint(models.EndpointVideos) {
						continue
					}
					entries = append(entries, models.VideoModel{
						ID:            p.ID + "/" + mi.Name,
						CanonicalSlug: p.ID + "/" + mi.Name,
						Name:          mi.DisplayName,
						Created:       p.CreatedAt.Unix(),
						Description:   mi.Description,
					})
				}
			}
		}
	}
	if h.virtualSvc != nil {
		if agents, err := h.virtualSvc.List(); err == nil {
			var mu sync.Mutex
			var resolved []*models.VirtualModel
			var wg sync.WaitGroup
			for _, a := range agents {
				if a.Disabled {
					continue
				}
				wg.Add(1)
				go func(a *models.VirtualModel) {
					defer wg.Done()
					serves := false
					for _, e := range a.Models {
						mv, ok := byFullID[string(e.ModelID)]
						if !ok {
							continue
						}
						if mv.SupportsEndpoint(models.EndpointVideos) {
							serves = true
							break
						}
					}
					if !serves {
						return
					}
					mu.Lock()
					resolved = append(resolved, a)
					mu.Unlock()
				}(a)
			}
			wg.Wait()
			for _, a := range resolved {
				entries = append(entries, models.VideoModel{
					ID:            provider.TypeVirtual + "/" + a.ID,
					CanonicalSlug: provider.TypeVirtual + "/" + a.ID,
					Name:          a.Name,
					Created:       a.CreatedAt.Unix(),
					Description:   a.Description,
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
