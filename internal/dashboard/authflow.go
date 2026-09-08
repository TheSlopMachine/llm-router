package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// authInitiate starts a multi-step auth wizard for a provider.
// Falls back with 409 when the type only supports single-step schema forms.
// @Summary      Initiate auth wizard
// @Description  Starts a multi-step auth wizard for a provider.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        body body object{provider_id=string} true "Provider reference"
// @Success      200 {object} object{status=string,nodes=[]models.UINode,redirect_url=string,flow_id=string,provider_id=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/auth/initiate [post]
func (h *Handler) authInitiate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProviderID string `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	p, ok := h.loadVisibleProvider(body.ProviderID)
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}

	flowID, err := util.GenerateID()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, "failed to generate flow id")
		return
	}

	result, err := h.luaSvc.AuthInitiate(r.Context(), p.TypeKey, flowID)
	if err != nil {
		if isAuthFallback(err) {
			h.jsonErr(w, http.StatusConflict, "provider does not support stepped auth flows")
			return
		}
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.renderAuthResult(w, result, body.ProviderID, flowID)
}

// authStep submits one wizard step and returns the next state.
// @Summary      Submit auth step
// @Description  Submits one auth wizard step and returns the next state.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        body body object{provider_id=string,flow_id=string,action=string,values=object} true "Step input"
// @Success      200 {object} object{status=string,nodes=[]models.UINode,redirect_url=string,flow_id=string,message=string,credential_id=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/auth/step [post]
func (h *Handler) authStep(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProviderID string         `json:"provider_id"`
		FlowID     string         `json:"flow_id"`
		Action     string         `json:"action"`
		Values     map[string]any `json:"values"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.FlowID == "" {
		h.jsonErr(w, http.StatusBadRequest, "flow_id is required")
		return
	}

	p, ok := h.loadVisibleProvider(body.ProviderID)
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}

	result, err := h.luaSvc.AuthStep(r.Context(), p.TypeKey, body.FlowID, body.Action, body.Values)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.renderAuthResult(w, result, body.ProviderID, body.FlowID)
}

func (h *Handler) renderAuthResult(w http.ResponseWriter, result *models.AuthFlowResult, providerID, flowID string) {
	if result == nil {
		h.jsonErr(w, http.StatusInternalServerError, "empty auth result")
		return
	}
	switch {
	case len(result.Render) > 0:
		h.json(w, http.StatusOK, map[string]any{
			"status":      "render",
			"nodes":       result.Render,
			"flow_id":     flowID,
			"provider_id": providerID,
		})
	case result.RedirectURL != "":
		h.json(w, http.StatusOK, map[string]any{
			"status":       "redirect",
			"redirect_url": result.RedirectURL,
			"flow_id":      flowID,
			"provider_id":  providerID,
		})
	case result.Credentials != nil:
		cred, err := h.credSvc.Add(credential.AddOptions{
			ProviderID: providerID,
			Label:      buildAutoCredentialLabel(providerID, result.Credentials),
			Data:       result.Credentials,
		})
		if err != nil {
			h.jsonErr(w, http.StatusInternalServerError, "save credentials: "+err.Error())
			return
		}
		h.json(w, http.StatusOK, map[string]any{
			"status":        "complete",
			"message":       "Credentials saved successfully",
			"credential_id": cred.ID,
		})
	default:
		h.jsonErr(w, http.StatusInternalServerError, "invalid auth flow state")
	}
}

func isAuthFallback(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, sub := range []string{"does not declare handler", "handler not declared", "not registered", "disabled"} {
		if len(msg) >= len(sub) && containsFold(msg, sub) {
			return true
		}
	}
	return false
}

func containsFold(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
