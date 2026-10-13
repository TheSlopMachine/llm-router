package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
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
	if _, ok := h.loadVisibleProvider(r.Context(), providerID); !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	views, err := h.gormModelInfoService.MergedView(r.Context(), providerID)
	if err != nil {
		h.logError("Failed to read provider models", "provider_id", providerID, "error", err)
		h.jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if views == nil {
		views = []modelinfo.ModelView{}
	}
	// Viewing warms the cache on a miss; rebuild managed virtual models
	// from the warmed list so they never linger missing until a toggle.
	if _, err := h.gormVirtualService.SyncProviderModels(r.Context(), providerID); err != nil {
		h.logWarn("Managed virtual models sync failed", "provider_id", providerID, "error", err)
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
	if _, ok := h.loadVisibleProvider(r.Context(), providerID); !ok {
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
	override := models.ModelOverride{ProviderID: providerID, Name: modelName}
	if existing, err := h.gormModelInfoService.ListOverrides(r.Context(), providerID); err == nil {
		for _, existingOverride := range existing {
			if existingOverride.Name == modelName {
				override = *existingOverride
				break
			}
		}
	}
	if body.Disabled != nil {
		override.Disabled = *body.Disabled
	}
	if body.Custom != nil {
		override.Custom = *body.Custom
	}
	if body.DisplayName != nil {
		override.DisplayName = *body.DisplayName
	}
	if body.Capabilities != nil {
		override.Capabilities = body.Capabilities
	}
	if body.InputModalities != nil {
		override.InputModalities = body.InputModalities
	}
	if body.OutputModalities != nil {
		override.OutputModalities = body.OutputModalities
	}
	if err := h.gormModelInfoService.SetOverride(r.Context(), override); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Enable/disable flips reshape managed virtual models.
	if _, err := h.gormVirtualService.SyncProviderModels(r.Context(), providerID); err != nil {
		h.logWarn("Managed virtual models sync failed", "provider_id", providerID, "error", err)
	}
	h.logInfo("Model override saved", "provider_id", providerID, "model", modelName)
	h.json(w, http.StatusOK, override)
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
	if _, ok := h.loadVisibleProvider(r.Context(), providerID); !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	modelName := r.PathValue("model")
	if err := h.gormModelInfoService.DeleteOverride(r.Context(), providerID, modelName); err != nil {
		h.logError("Failed to remove model override", "provider_id", providerID, "model", modelName, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := h.gormVirtualService.SyncProviderModels(r.Context(), providerID); err != nil {
		h.logWarn("Managed virtual models sync failed", "provider_id", providerID, "error", err)
	}
	h.logInfo("Model override removed", "provider_id", providerID, "model", modelName)
	w.WriteHeader(http.StatusNoContent)
}

// apiProviderModelsRefresh fetches the provider model list again. The
// previous list stays live until a new one arrives: a failed fetch keeps
// serving the stale list instead of wiping it. Managed virtual models
// refresh their members on a successful import.
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
	if _, ok := h.loadVisibleProvider(r.Context(), providerID); !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	if _, err := h.gormModelInfoService.Refresh(r.Context(), providerID); err != nil {
		h.logWarn("Model refresh failed, serving stale list", "provider_id", providerID, "error", err)
	} else if _, err := h.gormVirtualService.SyncProviderModels(r.Context(), providerID); err != nil {
		h.logWarn("Managed virtual models sync failed", "provider_id", providerID, "error", err)
	}
	views, err := h.gormModelInfoService.MergedView(r.Context(), providerID)
	if err != nil {
		h.logError("Failed to read provider models", "provider_id", providerID, "error", err)
		h.jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	h.logInfo("Provider models refreshed", "provider_id", providerID, "count", len(views))
	h.json(w, http.StatusOK, views)
}

// apiProviderVirtualModelsSync ensures one managed virtual model per
// non-empty endpoint group (idempotent, cache-only, no upstream fetch)
// and returns the groups.
// @Summary      Sync provider virtual models
// @Description  Creates missing managed virtual models and refreshes member lists from the cache, then returns the endpoint groups.
// @Tags         Providers
// @Produce      json
// @Param        id path string true "Provider ID"
// @Success      200  {array}  virtual.ProviderVMGroup
// @Failure      401  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/virtual-models/sync [post]
func (h *Handler) apiProviderVirtualModelsSync(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("id")
	if _, ok := h.loadVisibleProvider(r.Context(), providerID); !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	groups, err := h.gormVirtualService.SyncProviderModels(r.Context(), providerID)
	if err != nil {
		h.logError("Managed virtual models sync failed", "provider_id", providerID, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if groups == nil {
		groups = []virtual.ProviderVMGroup{}
	}
	h.json(w, http.StatusOK, groups)
}

// apiProviderVirtualModels lists endpoint groups of a provider with their
// managed virtual models. Computed from the cache, no upstream fetch.
// @Summary      List provider virtual-model groups
// @Description  Returns one group per served endpoint with member models and the managed virtual model, if any.
// @Tags         Providers
// @Produce      json
// @Param        id path string true "Provider ID"
// @Success      200 {array} virtual.ProviderVMGroup
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/virtual-models [get]
func (h *Handler) apiProviderVirtualModels(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("id")
	if _, ok := h.loadVisibleProvider(r.Context(), providerID); !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	groups, err := h.gormVirtualService.GroupsForProvider(r.Context(), providerID)
	if err != nil {
		h.logError("Failed to list provider virtual models", "provider_id", providerID, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if groups == nil {
		groups = []virtual.ProviderVMGroup{}
	}
	h.json(w, http.StatusOK, groups)
}

// apiModelTest verifies one model through the type's check_model handler.
// @Summary      Test model
// @Description  Delegates liveness to the plugin check_model handler and reports success and latency. Endpoint passes through verbatim (chat default); the plugin reports unsupported endpoints.
// @Tags         Models
// @Accept       json
// @Produce      json
// @Param        body body object{model_id=string,endpoint=string} true "Full model id (provider/model) plus probe endpoint"
// @Success      200 {object} object{ok=bool,latency_ms=int,error=string,response=string,quota_exceeded=bool,code=string,summary=string,proxy=string}
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
	result := h.routerService.TestModel(r.Context(), modelID, body.Endpoint)
	if !result.OK {
		arguments := []any{"model", body.ModelID, "endpoint", body.Endpoint, "error", result.Error}
		if result.Proxy != "" {
			arguments = append(arguments, "proxy_url", result.Proxy)
		}
		h.logWarn("Model probe failed", arguments...)
	}
	h.json(w, http.StatusOK, result)
}
