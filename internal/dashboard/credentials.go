package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
)

type credView struct {
	ID           string     `json:"id"`
	ProviderID   string     `json:"provider_id"`
	ProviderName string     `json:"provider_name"`
	Label        string     `json:"label"`
	IsExpired    bool       `json:"is_expired"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// apiCredentialsList lists all credentials
// @Summary      List credentials
// @Description  Returns all stored provider credentials.
// @Tags         Credentials
// @Produce      json
// @Success      200 {array} object{id=string,provider_id=string,provider_name=string,label=string,is_expired=bool,expires_at=string,updated_at=string}
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/credentials [get]
func (h *Handler) apiCredentialsList(w http.ResponseWriter, r *http.Request) {
	providers, _ := h.providerSvc.List()
	providerMap := make(map[string]string, len(providers))
	for _, p := range providers {
		providerMap[p.ID] = p.Name
	}
	creds, err := h.credSvc.ListAll()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]credView, len(creds))
	for i, c := range creds {
		out[i] = credView{
			ID:           c.ID,
			ProviderID:   c.ProviderID,
			ProviderName: providerMap[c.ProviderID],
			Label:        c.Label,
			IsExpired:    c.IsExpired(),
			ExpiresAt:    c.ExpiresAt,
			UpdatedAt:    c.UpdatedAt,
		}
	}
	h.json(w, http.StatusOK, out)
}

// apiCredentialsCreate saves a credential from single-step schema data.
// @Summary      Create credential
// @Description  Validates and stores a credential for a provider.
// @Tags         Credentials
// @Accept       json
// @Produce      json
// @Param        body body object{provider_id=string,label=string,data=object} true "Credential details"
// @Success      200 {object} object{id=string,provider_id=string,provider_name=string,label=string,is_expired=bool,expires_at=string,updated_at=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/credentials [post]
func (h *Handler) apiCredentialsCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProviderID string         `json:"provider_id"`
		Label      string         `json:"label"`
		Data       map[string]any `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.ProviderID == "" {
		h.jsonErr(w, http.StatusBadRequest, "provider_id is required")
		return
	}
	p, ok := h.loadVisibleProvider(body.ProviderID)
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	label := body.Label
	if label == "" {
		label = buildAutoCredentialLabel(p.Name, body.Data)
	}
	cred, err := h.credSvc.Add(credential.AddOptions{
		ProviderID: body.ProviderID,
		Label:      label,
		Data:       body.Data,
	})
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, credView{
		ID:           cred.ID,
		ProviderID:   cred.ProviderID,
		ProviderName: p.Name,
		Label:        cred.Label,
		IsExpired:    cred.IsExpired(),
		ExpiresAt:    cred.ExpiresAt,
		UpdatedAt:    cred.UpdatedAt,
	})
}

// apiCredentialsDelete deletes a credential
// @Summary      Delete credential
// @Description  Removes a stored credential.
// @Tags         Credentials
// @Produce      json
// @Param        id path string true "Credential ID"
// @Success      204 "No Content"
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/credentials/{id} [delete]
func (h *Handler) apiCredentialsDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.credSvc.Delete(r.PathValue("id")); err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// apiModels fetches models for providers
// @Summary      Get models by providers
// @Description  Fetches available models and their metadata for specified providers.
// @Tags         Models
// @Produce      json
// @Param        provider_ids query []string true "Provider IDs" collectionFormat(multi)
// @Success      200 {object} object{providers=[]object{provider_id=string,provider_name=string,provider_type=string,models=[]string,model_info=[]models.ModelInfo,error=string}}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/models [get]
func (h *Handler) apiModels(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	providerIDs := r.Form["provider_ids"]
	if len(providerIDs) == 0 {
		h.jsonErr(w, http.StatusBadRequest, "no provider_ids provided")
		return
	}

	type ProviderModels struct {
		ProviderID   string             `json:"provider_id"`
		ProviderName string             `json:"provider_name"`
		ProviderType string             `json:"provider_type"`
		Models       []string           `json:"models,omitempty"`
		ModelInfo    []models.ModelInfo `json:"model_info,omitempty"`
		Error        string             `json:"error,omitempty"`
	}

	type result struct{ pm ProviderModels }
	ch := make(chan result, len(providerIDs))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	for _, pid := range providerIDs {
		go func(providerID string) {
			p, err := h.providerSvc.Get(providerID)
			if err != nil {
				ch <- result{pm: ProviderModels{ProviderID: providerID, Error: "provider not found"}}
				return
			}

			pm := ProviderModels{
				ProviderID:   p.ID,
				ProviderName: p.Name,
				ProviderType: p.TypeKey,
			}

			modelInfos, err := h.modelInfoSvc.GetModelInfos(ctx, providerID)
			if err != nil {
				h.logger.Warn("dashboard apiModels: model discovery failed", "provider_id", providerID, "err", err)
				pm.Error = err.Error()
			} else {
				pm.ModelInfo = modelInfos

				pm.Models = make([]string, len(modelInfos))
				for i, m := range modelInfos {
					pm.Models[i] = m.Name
				}
			}

			ch <- result{pm: pm}
		}(pid)
	}

	results := make([]ProviderModels, 0, len(providerIDs))
	for range providerIDs {
		results = append(results, (<-ch).pm)
	}
	h.json(w, http.StatusOK, map[string]any{"providers": results})
}
