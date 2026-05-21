package dashboard

import (
	"context"
	"net/http"
	"sort"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

type availableModelView struct {
	FullModelID   string `json:"full_model_id"`
	ProviderID    string `json:"provider_id"`
	ProviderName  string `json:"provider_name"`
	ProviderType  string `json:"provider_type"`
	ModelName     string `json:"model_name"`
	DisplayName   string `json:"display_name"`
	ContextWindow int64  `json:"context_window,omitempty"`
	MaxTokens     int64  `json:"max_tokens,omitempty"`
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
	items, err := h.availableModels(r.Context())
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.json(w, http.StatusOK, items)
}

func (h *Handler) availableModels(ctx context.Context) ([]availableModelView, error) {
	providers, err := h.providerSvc.List()
	if err != nil {
		return nil, err
	}

	items := make([]availableModelView, 0)
	for _, providerRecord := range providers {
		if providerRecord.Type == "agents" {
			continue
		}

		if _, err := h.credSvc.All(providerRecord.ID); err != nil {
			continue
		}

		modelInfos, err := h.modelInfoSvc.GetModelInfos(ctx, providerRecord.ID)
		if err != nil {
			continue
		}

		for _, modelInfo := range modelInfos {
			displayName := modelInfo.DisplayName
			if displayName == "" {
				displayName = modelInfo.Name
			}

			items = append(items, availableModelView{
				FullModelID:   string(models.ModelId(providerRecord.ID + "/" + modelInfo.Name)),
				ProviderID:    providerRecord.ID,
				ProviderName:  providerRecord.Name,
				ProviderType:  providerRecord.Type,
				ModelName:     modelInfo.Name,
				DisplayName:   displayName,
				ContextWindow: modelInfo.ContextWindow,
				MaxTokens:     modelInfo.MaxTokens,
			})
		}
	}

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
