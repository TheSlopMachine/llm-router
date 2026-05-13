package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// apiProvidersList lists all providers
// @Summary      List providers
// @Description  Returns all configured providers.
// @Tags         Providers
// @Produce      json
// @Success      200 {array} object{id=string,name=string,type=string,qualifier=string,auth_type=string,base_url=string,icon_url=string}
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers [get]
func (h *Handler) apiProvidersList(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerSvc.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	type pv struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Type      string `json:"type"`
		Qualifier string `json:"qualifier"`
		AuthType  string `json:"auth_type"`
		BaseURL   string `json:"base_url"`
		IconURL   string `json:"icon_url"`
	}
	out := make([]pv, 0, len(providers))
	for _, p := range providers {
		out = append(out, pv{
			ID:        p.ID,
			Name:      p.Name,
			Type:      p.Type,
			Qualifier: p.Qualifier,
			AuthType:  string(p.AuthType),
			BaseURL:   p.BaseURL,
			IconURL:   p.IconURL,
		})
	}
	h.json(w, http.StatusOK, out)
}

// apiAdapterTypes lists available adapter types
// @Summary      List adapter types
// @Description  Returns all registered provider adapter types.
// @Tags         Providers
// @Produce      json
// @Success      200 {array} string
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/adapter-types [get]
func (h *Handler) apiAdapterTypes(w http.ResponseWriter, r *http.Request) {
	h.json(w, http.StatusOK, provider.Registered())
}

// apiProvidersStats returns aggregated statistics for all providers
// @Summary      Get provider statistics
// @Description  Returns aggregated statistics for all providers (model count, credential count, requests in last 24h)
// @Tags         Providers
// @Produce      json
// @Success      200 {object} map[string]models.ProviderStats
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/stats [get]
func (h *Handler) apiProvidersStats(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerSvc.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	stats := make(map[string]*models.ProviderStats)
	ctx := r.Context()

	for _, p := range providers {
		stat := &models.ProviderStats{}

		// 1. Model count
		modelInfos, err := h.modelInfoSvc.GetModelInfos(ctx, p.ID)
		if err == nil {
			stat.ModelCount = len(modelInfos)
		}

		// 2. Credential count
		creds, err := h.credSvc.ListByProvider(p.ID)
		if err == nil {
			stat.CredentialCount = len(creds)
		}

		// 3. Requests in last 24 hours
		filters := models.MetricsFilters{
			ProviderID: p.ID,
			TimeRange:  "1d",
		}
		overview, err := h.metricsSvc.QueryOverview(filters)
		if err == nil && overview != nil {
			stat.RequestsToday = overview.TotalRequests
		}

		stats[p.ID] = stat
	}

	h.json(w, http.StatusOK, stats)
}

// authStart initiates an authentication flow
func (h *Handler) authStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProviderID string `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	p, err := h.providerSvc.Get(body.ProviderID)
	if err != nil {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}

	adapter, err := provider.Lookup(p.Type)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, "adapter not found")
		return
	}

	authFlow := adapter.GetAuthFlow()
	if authFlow == nil {
		h.jsonErr(w, http.StatusBadRequest, "provider does not support authentication flows")
		return
	}

	flowID, err := util.GenerateID()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, "failed to generate flow id")
		return
	}
	ctx := provider.AuthFlowContext{
		ProviderID: body.ProviderID,
		FlowID:     flowID,
		Store:      h.authSvc,
	}

	state, err := authFlow.InitiateFlow(ctx)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.renderAuthState(w, state, body.ProviderID, flowID)
}

// authCallback handles authentication flow callbacks
func (h *Handler) authCallback(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid form data")
		return
	}

	flowID := r.FormValue("flow_id")
	if flowID == "" {
		h.jsonErr(w, http.StatusBadRequest, "missing flow_id")
		return
	}

	providerIDRaw, err := h.authSvc.Get(flowID + ":provider_id")
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid or expired flow")
		return
	}
	providerID := string(providerIDRaw)

	p, err := h.providerSvc.Get(providerID)
	if err != nil {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}

	adapter, err := provider.Lookup(p.Type)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, "adapter not found")
		return
	}

	authFlow := adapter.GetAuthFlow()
	if authFlow == nil {
		h.jsonErr(w, http.StatusBadRequest, "provider does not support authentication flows")
		return
	}

	ctx := provider.AuthFlowContext{
		ProviderID: providerID,
		FlowID:     flowID,
		Store:      h.authSvc,
	}

	input := make(map[string][]string)
	for k, v := range r.Form {
		input[k] = v
	}

	state, err := authFlow.HandleStep(ctx, input)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	if state.Credentials != nil {
		_, err := h.credSvc.Add(credential.AddOptions{
			ProviderID: providerID,
			Label:      "Auto-generated",
			Data:       state.Credentials,
		})
		if err != nil {
			h.jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to save credentials: %s", err))
			return
		}
		h.cleanupAuthFlow(flowID)
		h.json(w, http.StatusOK, map[string]any{
			"status":  "complete",
			"message": "Credentials saved successfully",
		})
		return
	}

	h.renderAuthState(w, state, providerID, flowID)
}

func (h *Handler) renderAuthState(w http.ResponseWriter, state provider.AuthFlowState, providerID, flowID string) {
	if state.ExternalURL != "" {
		h.json(w, http.StatusOK, map[string]any{
			"status":       "redirect",
			"external_url": state.ExternalURL,
		})
		return
	}

	if state.RenderHTML != "" {
		if err := h.authSvc.Set(flowID+":provider_id", providerID); err != nil {
			h.jsonErr(w, http.StatusInternalServerError, "failed to store flow state")
			return
		}
		h.json(w, http.StatusOK, map[string]any{
			"status":  "render",
			"html":    state.RenderHTML,
			"flow_id": flowID,
		})
		return
	}

	h.jsonErr(w, http.StatusInternalServerError, "invalid auth flow state")
}

func (h *Handler) cleanupAuthFlow(flowID string) {
	h.authSvc.Delete(flowID + ":provider_id")
}

