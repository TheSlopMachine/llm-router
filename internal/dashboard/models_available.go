package dashboard

import (
	"net/http"
	"sort"

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
}

// apiAvailableModels godoc
// @Summary      List available models
// @Description  Returns a flat list of currently available models from providers with usable credentials.
// @Tags         Models
// @Produce      json
// @Success      200 {array} object{full_model_id=string,provider_id=string,provider_name=string,provider_type=string,model_name=string,display_name=string,context_window=int,max_tokens=int}
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/models/available [get]
func (h *Handler) apiAvailableModels(w http.ResponseWriter, r *http.Request) {
	items, err := h.availableModels()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.json(w, http.StatusOK, items)
}

func (h *Handler) availableModels() ([]availableModelView, error) {
	providers, err := h.providerSvc.List()
	if err != nil {
		return nil, err
	}

	items := make([]availableModelView, 0)
	for _, providerRecord := range providers {
		if providerRecord.TypeKey == provider.TypeVirtual || providerRecord.Disabled {
			continue
		}

		// Cache peek only: browsing the models tab must not ping upstreams.
		modelInfos, err := h.modelInfoSvc.PeekMergedView(providerRecord.ID)
		if err != nil {
			continue
		}

		for _, modelInfo := range modelInfos {
			if modelInfo.Disabled {
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
