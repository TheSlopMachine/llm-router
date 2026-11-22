package dashboard

import (
	"context"
	"net/http"
	"sort"
	"strconv"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

type availableModelView struct {
	FullModelID         string                 `json:"full_model_id"`
	ProviderID          string                 `json:"provider_id"`
	ProviderName        string                 `json:"provider_name"`
	ProviderType        string                 `json:"provider_type"`
	ModelName           string                 `json:"model_name"`
	DisplayName         string                 `json:"display_name"`
	Description         string                 `json:"description,omitempty"`
	ContextWindow       int64                  `json:"context_window,omitempty"`
	MaxTokens           int64                  `json:"max_tokens,omitempty"`
	Capabilities        []string               `json:"capabilities,omitempty"`
	InputModalities     []string               `json:"input_modalities,omitempty"`
	OutputModalities    []string               `json:"output_modalities,omitempty"`
	Reasoning           *models.ModelReasoning `json:"reasoning,omitempty"`
	SupportedParameters []string               `json:"supported_parameters,omitempty"`
	Disabled            bool                   `json:"disabled,omitempty"`
}

// apiAvailableModels documentation
// @Summary      List available models
// @Description  Returns a flat list of the models of enabled providers. Disabled models are listed only with include_disabled=true.
// @Tags         Models
// @Produce      json
// @Param        include_disabled query bool false "Also list disabled models, marked disabled"
// @Success      200 {array} object{full_model_id=string,provider_id=string,provider_name=string,provider_type=string,model_name=string,display_name=string,context_window=int,max_tokens=int,disabled=bool}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/models/available [get]
func (h *Handler) apiAvailableModels(w http.ResponseWriter, r *http.Request) {
	includeDisabled := false
	if raw := r.URL.Query().Get("include_disabled"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			h.jsonErr(w, http.StatusBadRequest, "include_disabled must be true or false")
			return
		}
		includeDisabled = parsed
	}

	items, err := h.availableModels(r.Context(), includeDisabled)
	if err != nil {
		h.logError("Failed to list available models", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.json(w, http.StatusOK, items)
}

// availableModels lists the models of enabled non-virtual providers; disabled
// models only when includeDisabled.
func (h *Handler) availableModels(ctx context.Context, includeDisabled bool) ([]availableModelView, error) {
	providers, err := h.gormProviderService.List(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]availableModelView, 0)
	for _, providerRecord := range providers {
		if providerRecord.TypeKey == provider.TypeVirtual || providerRecord.Disabled {
			continue
		}

		// Cache peek only: browsing the models tab must not ping upstreams.
		modelInfos, err := h.gormModelInfoService.PeekMergedView(ctx, providerRecord.ID)
		if err != nil {
			h.logWarn("Skipped provider models in the available list", "provider_id", providerRecord.ID, "error", err)
			continue
		}

		for _, modelInfo := range modelInfos {
			if modelInfo.Disabled && !includeDisabled {
				continue
			}
			displayName := modelInfo.DisplayName
			if displayName == "" {
				displayName = modelInfo.Name
			}

			items = append(items, availableModelView{
				FullModelID:         string(models.ModelId(providerRecord.ID + "/" + modelInfo.Name)),
				ProviderID:          providerRecord.ID,
				ProviderName:        providerRecord.Name,
				ProviderType:        providerRecord.TypeKey,
				ModelName:           modelInfo.Name,
				DisplayName:         displayName,
				Description:         modelInfo.Description,
				ContextWindow:       modelInfo.ContextWindow,
				MaxTokens:           modelInfo.MaxTokens,
				Capabilities:        modelInfo.Capabilities,
				InputModalities:     modelInfo.InputModalities,
				OutputModalities:    modelInfo.OutputModalities,
				Reasoning:           modelInfo.Reasoning,
				SupportedParameters: modelInfo.SupportedParameters,
				Disabled:            modelInfo.Disabled,
			})
		}
	}

	// Virtual models are excluded: they are not valid fall-through targets
	// (circular dependency) and list separately under virtual-models.
	sort.Slice(items, func(i, j int) bool {
		if items[i].ProviderName != items[j].ProviderName {
			return items[i].ProviderName < items[j].ProviderName
		}
		if items[i].DisplayName != items[j].DisplayName {
			return items[i].DisplayName < items[j].DisplayName
		}
		return items[i].FullModelID < items[j].FullModelID
	})

	return items, nil
}
