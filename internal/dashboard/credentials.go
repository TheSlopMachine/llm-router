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
	Disabled     bool       `json:"disabled"`
	Order        int        `json:"order,omitempty"`
	RequestCount int64      `json:"request_count"`
	SuccessCount int64      `json:"success_count"`
	QuotaResetAt *time.Time `json:"quota_reset_at,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func toCredView(c *models.Credential, providerName string) credView {
	return credView{
		ID:           c.ID,
		ProviderID:   c.ProviderID,
		ProviderName: providerName,
		Label:        c.Label,
		IsExpired:    c.IsExpired(),
		Disabled:     c.Disabled,
		Order:        c.Order,
		RequestCount: c.RequestCount,
		SuccessCount: c.SuccessCount,
		QuotaResetAt: c.QuotaResetAt,
		ExpiresAt:    c.ExpiresAt,
		UpdatedAt:    c.UpdatedAt,
	}
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
		out[i] = toCredView(c, providerMap[c.ProviderID])
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
	h.json(w, http.StatusOK, toCredView(cred, p.Name))
}

// apiCredentialsUpdate edits label, enable/disable state or data of a credential
// @Summary      Update credential
// @Description  Renames, enables/disables or replaces the data of a stored credential.
// @Tags         Credentials
// @Accept       json
// @Produce      json
// @Param        id path string true "Credential ID"
// @Param        body body object{label=string,disabled=bool,data=object} true "Fields to update"
// @Success      200 {object} object{id=string,provider_id=string,provider_name=string,label=string,is_expired=bool,disabled=bool,order=int,updated_at=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/credentials/{id} [put]
func (h *Handler) apiCredentialsUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Label    *string        `json:"label"`
		Disabled *bool          `json:"disabled"`
		Data     map[string]any `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Label == nil && body.Disabled == nil && body.Data == nil {
		h.jsonErr(w, http.StatusBadRequest, "nothing to update")
		return
	}
	if err := h.credSvc.UpdateDetails(id, body.Label, body.Disabled, body.Data); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cred, err := h.credSvc.Get(id)
	if err != nil {
		h.jsonErr(w, http.StatusNotFound, "credential not found")
		return
	}
	name := ""
	if p, err := h.providerSvc.Get(cred.ProviderID); err == nil {
		name = p.Name
	}
	h.json(w, http.StatusOK, toCredView(cred, name))
}

// apiCredentialsReorder sets the manual pool order of a provider's credentials
// @Summary      Reorder credentials
// @Description  Sets the routing priority order; ids must cover all credentials of the provider.
// @Tags         Credentials
// @Accept       json
// @Param        body body object{provider_id=string,ids=[]string} true "Ordered credential IDs"
// @Success      204 "No Content"
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/credentials/reorder [put]
func (h *Handler) apiCredentialsReorder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProviderID string   `json:"provider_id"`
		IDs        []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.ProviderID == "" || len(body.IDs) == 0 {
		h.jsonErr(w, http.StatusBadRequest, "provider_id and ids are required")
		return
	}
	if err := h.credSvc.Reorder(body.ProviderID, body.IDs); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// apiCredentialsTest probes a credential with a minimal live request
// @Summary      Test credential
// @Description  Runs a minimal completion pinned to this credential and reports success and latency.
// @Tags         Credentials
// @Accept       json
// @Produce      json
// @Param        id path string true "Credential ID"
// @Param        body body object{model=string} false "Optional model override"
// @Success      200 {object} object{ok=bool,latency_ms=int,error=string,response=string}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/credentials/{id}/test [post]
func (h *Handler) apiCredentialsTest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cred, err := h.credSvc.Get(id)
	if err != nil {
		h.jsonErr(w, http.StatusNotFound, "credential not found")
		return
	}
	var body struct {
		Model string `json:"model"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	res := h.routerSvc.TestCredential(r.Context(), cred.ProviderID, id, body.Model)
	h.json(w, http.StatusOK, res)
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
