package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
)

type credentialView struct {
	ID             string     `json:"id"`
	ProviderID     string     `json:"provider_id"`
	ProviderName   string     `json:"provider_name"`
	Label          string     `json:"label"`
	IsExpired      bool       `json:"is_expired"`
	Disabled       bool       `json:"disabled"`
	DisabledBy     string     `json:"disabled_by,omitempty"`
	DisabledReason string     `json:"disabled_reason,omitempty"`
	DisabledAt     *time.Time `json:"disabled_at,omitempty"`
	Parked         bool       `json:"parked,omitempty"`
	ParkedUntil    *time.Time `json:"parked_until,omitempty"`
	ParkReason     string     `json:"park_reason,omitempty"`
	Order          int        `json:"order,omitempty"`
	RequestCount   int64      `json:"request_count"`
	SuccessCount   int64      `json:"success_count"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (h *Handler) toCredentialView(credential *models.Credential, providerName string) credentialView {
	view := credentialView{
		ID:             credential.ID,
		ProviderID:     credential.ProviderID,
		ProviderName:   providerName,
		Label:          credential.Label,
		IsExpired:      credential.IsExpired(),
		Disabled:       credential.Disabled,
		DisabledBy:     credential.DisabledBy,
		DisabledReason: credential.DisabledReason,
		DisabledAt:     credential.DisabledAt,
		Order:          credential.Order,
		RequestCount:   credential.RequestCount,
		SuccessCount:   credential.SuccessCount,
		ExpiresAt:      credential.ExpiresAt,
		UpdatedAt:      credential.UpdatedAt,
	}
	if park, err := h.credentialService.Parked(credential.ID); err == nil && park != nil {
		view.Parked = true
		view.ParkedUntil = &park.Until
		view.ParkReason = park.Reason
	}
	return view
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
	providers, _ := h.providerService.List()
	providerMap := make(map[string]string, len(providers))
	for _, provider := range providers {
		providerMap[provider.ID] = provider.Name
	}
	credentials, err := h.credentialService.ListAll()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]credentialView, len(credentials))
	for i, credential := range credentials {
		out[i] = h.toCredentialView(credential, providerMap[credential.ProviderID])
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
	provider, ok := h.loadVisibleProvider(body.ProviderID)
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	label := body.Label
	if label == "" {
		label = buildAutoCredentialLabel(provider.Name, body.Data)
	}
	credential, err := h.credentialService.Add(credential.AddOptions{
		ProviderID: body.ProviderID,
		Label:      label,
		Data:       body.Data,
	})
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, h.toCredentialView(credential, provider.Name))
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
	if err := h.credentialService.UpdateDetails(id, body.Label, body.Disabled, body.Data); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	credential, err := h.credentialService.Get(id)
	if err != nil {
		h.jsonErr(w, http.StatusNotFound, "credential not found")
		return
	}
	if body.Disabled != nil {
		h.logger.Info("credential admin state change", "credential_id", id, "disabled", *body.Disabled)
	}
	name := ""
	if provider, err := h.providerService.Get(credential.ProviderID); err == nil {
		name = provider.Name
	}
	h.json(w, http.StatusOK, h.toCredentialView(credential, name))
}

// apiCredentialsTest verifies a credential through the provider's check_health
// handler. Explicit unhealthy disables the credential at once; types without
// the handler report unsupported and run nothing.
// @Summary      Test credential
// @Description  Runs the provider check_health handler for this credential and reports the verdict.
// @Tags         Credentials
// @Accept       json
// @Produce      json
// @Param        id path string true "Credential ID"
// @Param        body body object{model=string} false "Optional model override"
// @Success      200 {object} object{ok=bool,latency_ms=int,error=string,response=string,quota_exceeded=bool,code=string,summary=string,proxy=string}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/credentials/{id}/test [post]
func (h *Handler) apiCredentialsTest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	credential, err := h.credentialService.Get(id)
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
	result := h.routerService.TestCredential(r.Context(), credential.ProviderID, id, body.Model)
	if !result.OK {
		arguments := []any{"credential", id, "provider", credential.ProviderID, "err", result.Error}
		if result.Proxy != "" {
			arguments = append(arguments, "proxy", result.Proxy)
		}
		h.logger.Warn("credential probe failed", arguments...)
	}
	h.json(w, http.StatusOK, result)
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
	if err := h.credentialService.Delete(r.PathValue("id")); err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// apiCredentialsUnpark clears a unified cooldown park so the credential
// serves immediately. Missing parks succeed: unparking is idempotent.
// Unknown credential IDs fail loudly.
// @Summary      Unpark credential
// @Description  Clears a unified cooldown park, returning the credential to rotation at once.
// @Tags         Credentials
// @Produce      json
// @Param        id path string true "Credential ID"
// @Success      200 {object} object{ok=bool}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/credentials/{id}/unpark [post]
func (h *Handler) apiCredentialsUnpark(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.credentialService.Get(id); err != nil {
		h.jsonErr(w, http.StatusNotFound, "credential not found")
		return
	}
	if err := h.credentialService.Unpark(id); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]any{"ok": true})
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

	type result struct{ providerModels ProviderModels }
	channel := make(chan result, len(providerIDs))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	for _, providerID := range providerIDs {
		go func(currentProviderID string) {
			provider, err := h.providerService.Get(currentProviderID)
			if err != nil {
				channel <- result{providerModels: ProviderModels{ProviderID: currentProviderID, Error: "provider not found"}}
				return
			}

			providerModels := ProviderModels{
				ProviderID:   provider.ID,
				ProviderName: provider.Name,
				ProviderType: provider.TypeKey,
			}

			modelInfos, err := h.modelInfoService.GetModelInfos(ctx, currentProviderID)
			if err != nil {
				h.logger.Warn("dashboard apiModels: model discovery failed", "provider_id", currentProviderID, "err", err)
				providerModels.Error = err.Error()
			} else {
				providerModels.ModelInfo = modelInfos

				providerModels.Models = make([]string, len(modelInfos))
				for i, model := range modelInfos {
					providerModels.Models[i] = model.Name
				}
			}

			channel <- result{providerModels: providerModels}
		}(providerID)
	}

	results := make([]ProviderModels, 0, len(providerIDs))
	for range providerIDs {
		results = append(results, (<-channel).providerModels)
	}
	h.json(w, http.StatusOK, map[string]any{"providers": results})
}
