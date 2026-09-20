package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
)

// apiProviderModels returns the provider's models merged with admin overrides
// @Summary      List provider models with overrides
// @Description  Returns the upstream model list merged with enable/disable state and custom models.
// @Tags         Providers
// @Produce      json
// @Param        id path string true "Provider ID"
// @Success      200 {array} modelinfo.ModelView
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      502 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/models [get]
func (h *Handler) apiProviderModels(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("id")
	if _, ok := h.loadVisibleProvider(providerID); !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	views, err := h.modelInfoSvc.MergedView(r.Context(), providerID)
	if err != nil {
		h.jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if views == nil {
		views = []modelinfo.ModelView{}
	}
	h.json(w, http.StatusOK, views)
}

// apiProviderModelSetOverride creates or updates the override for one model
// @Summary      Set model override
// @Description  Disables/enables a model, adjusts display metadata, or registers a custom model.
// @Tags         Providers
// @Accept       json
// @Produce      json
// @Param        id path string true "Provider ID"
// @Param        model path string true "Model name"
// @Param        body body object{disabled=bool,custom=bool,display_name=string,capabilities=[]string,input_modalities=[]string,output_modalities=[]string} true "Override"
// @Success      200 {object} models.ModelOverride
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/models/{model} [put]
func (h *Handler) apiProviderModelSetOverride(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("id")
	if _, ok := h.loadVisibleProvider(providerID); !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	var body struct {
		Disabled         *bool    `json:"disabled"`
		Custom           *bool    `json:"custom"`
		DisplayName      *string  `json:"display_name"`
		Capabilities     []string `json:"capabilities"`
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	modelName := r.PathValue("model")
	if modelName == "" {
		h.jsonErr(w, http.StatusBadRequest, "model name is required")
		return
	}
	ov := models.ModelOverride{ProviderID: providerID, Name: modelName}
	if existing, err := h.modelInfoSvc.ListOverrides(providerID); err == nil {
		for _, e := range existing {
			if e.Name == modelName {
				ov = *e
				break
			}
		}
	}
	if body.Disabled != nil {
		ov.Disabled = *body.Disabled
	}
	if body.Custom != nil {
		ov.Custom = *body.Custom
	}
	if body.DisplayName != nil {
		ov.DisplayName = *body.DisplayName
	}
	if body.Capabilities != nil {
		ov.Capabilities = body.Capabilities
	}
	if body.InputModalities != nil {
		ov.InputModalities = body.InputModalities
	}
	if body.OutputModalities != nil {
		ov.OutputModalities = body.OutputModalities
	}
	if err := h.modelInfoSvc.SetOverride(ov); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, ov)
}

// apiProviderModelDeleteOverride removes the override for one model
// @Summary      Delete model override
// @Description  Removes the override; for custom models this removes the model.
// @Tags         Providers
// @Param        id path string true "Provider ID"
// @Param        model path string true "Model name"
// @Success      204 "No Content"
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/models/{model} [delete]
func (h *Handler) apiProviderModelDeleteOverride(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("id")
	if _, ok := h.loadVisibleProvider(providerID); !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	if err := h.modelInfoSvc.DeleteOverride(providerID, r.PathValue("model")); err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// apiProviderModelsRefresh fetches the provider model list again. The
// previous list stays live until a new one arrives: a failed fetch keeps
// serving the stale list instead of wiping it.
// @Summary      Refresh provider models
// @Description  Fetches the model list from the provider again; the previous list stays until a new one arrives.
// @Tags         Providers
// @Produce      json
// @Param        id path string true "Provider ID"
// @Success      200 {array} modelinfo.ModelView
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      502 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/models/refresh [post]
func (h *Handler) apiProviderModelsRefresh(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("id")
	if _, ok := h.loadVisibleProvider(providerID); !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	if _, err := h.modelInfoSvc.Refresh(r.Context(), providerID); err != nil {
		h.logger.Warn("model refresh failed, serving stale list", "provider_id", providerID, "err", err)
	}
	views, err := h.modelInfoSvc.MergedView(r.Context(), providerID)
	if err != nil {
		h.jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	h.json(w, http.StatusOK, views)
}

// apiModelCapabilities probes a model for supported features
// @Summary      Probe model capabilities
// @Description  Runs live probe requests to detect tool calling and JSON mode support.
// @Tags         Models
// @Accept       json
// @Produce      json
// @Param        body body object{model_id=string} true "Full model id (provider/model)"
// @Success      200 {object} object{capabilities=[]string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/models/capabilities [post]
func (h *Handler) apiModelCapabilities(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ModelID string `json:"model_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.ModelID == "" {
		h.jsonErr(w, http.StatusBadRequest, "model_id is required")
		return
	}
	caps, err := h.routerSvc.ProbeCapabilities(r.Context(), models.ModelId(body.ModelID))
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if caps == nil {
		caps = []string{}
	}
	h.json(w, http.StatusOK, map[string]any{"capabilities": caps})
}

// apiModelTest probes a model through the normal routing path
// @Summary      Test model
// @Description  Runs a minimal probe for a full ModelId and reports success and latency. Endpoint selects the probe: chat (default), vision, speech, transcription, image, embeddings.
// @Tags         Models
// @Accept       json
// @Produce      json
// @Param        body body object{model_id=string,endpoint=string} true "Full model id (provider/model) plus probe endpoint"
// @Success      200 {object} object{ok=bool,latency_ms=int,error=string,response=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/models/test [post]
func (h *Handler) apiModelTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ModelID  string `json:"model_id"`
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.ModelID == "" {
		h.jsonErr(w, http.StatusBadRequest, "model_id is required")
		return
	}
	modelID := models.ModelId(body.ModelID)
	var res router.TestResult
	switch body.Endpoint {
	case "", "chat":
		res = h.routerSvc.TestModel(r.Context(), modelID)
	case "vision":
		res = h.routerSvc.TestVision(r.Context(), modelID)
	case "speech":
		res = h.routerSvc.TestSpeech(r.Context(), modelID)
	case "transcription":
		res = h.routerSvc.TestTranscribe(r.Context(), modelID)
	case "image":
		res = h.routerSvc.TestImageGeneration(r.Context(), modelID)
	case "embeddings":
		res = h.routerSvc.TestEmbeddings(r.Context(), modelID)
	default:
		h.jsonErr(w, http.StatusBadRequest, "unknown probe endpoint")
		return
	}
	if !res.OK {
		h.logger.Warn("model probe failed", "model", body.ModelID, "endpoint", body.Endpoint, "err", res.Error)
	}
	h.json(w, http.StatusOK, res)
}
