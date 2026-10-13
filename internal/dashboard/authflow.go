package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
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
		Locale     string `json:"locale"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	provider, ok := h.loadVisibleProvider(r.Context(), body.ProviderID)
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}

	// Go backends never offer stepped flows; fall back without touching Lua.
	if _, ok := h.gormProviderService.GoAdapterFor(provider.TypeKey); ok {
		h.jsonErr(w, http.StatusConflict, "provider does not support stepped auth flows")
		return
	}

	flowID, err := util.GenerateID()
	if err != nil {
		h.logError("Failed to generate flow identifier", "provider_id", body.ProviderID, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, "failed to generate flow id")
		return
	}

	locale := luaplugin.NegotiateLocale(firstNonEmpty(r.URL.Query().Get("locale"), body.Locale), r.Header.Get("Accept-Language"))
	result, err := h.luaService.AuthInitiate(r.Context(), provider.TypeKey, flowID, provider.Config, locale)
	if err != nil {
		if isAuthFallback(err) {
			h.jsonErr(w, http.StatusConflict, "provider does not support stepped auth flows")
			return
		}
		h.logError("Failed to initiate authentication", "provider_id", body.ProviderID, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.renderAuthResult(w, r.Context(), result, body.ProviderID, flowID)
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
		Locale     string         `json:"locale"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.FlowID == "" {
		h.jsonErr(w, http.StatusBadRequest, "flow_id is required")
		return
	}

	provider, ok := h.loadVisibleProvider(r.Context(), body.ProviderID)
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}

	if _, ok := h.gormProviderService.GoAdapterFor(provider.TypeKey); ok {
		h.jsonErr(w, http.StatusConflict, "provider does not support stepped auth flows")
		return
	}

	locale := luaplugin.NegotiateLocale(firstNonEmpty(r.URL.Query().Get("locale"), body.Locale), r.Header.Get("Accept-Language"))
	result, err := h.luaService.AuthStep(r.Context(), provider.TypeKey, body.FlowID, body.Action, body.Values, provider.Config, locale)
	if err != nil {
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			h.jsonErr(w, http.StatusBadRequest, "auth flow step is not supported by this provider")
			return
		}
		h.logError("Failed to advance authentication", "provider_id", body.ProviderID, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.renderAuthResult(w, r.Context(), result, body.ProviderID, body.FlowID)
}

func (h *Handler) renderAuthResult(w http.ResponseWriter, ctx context.Context, result *models.AuthFlowResult, providerID, flowID string) {
	if result == nil {
		h.logError("Empty authentication result", "provider_id", providerID)
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
		credential, err := h.gormCredentialService.Add(ctx, credential.AddOptions{
			ProviderID: providerID,
			Label:      buildAutoCredentialLabel(providerID, result.Credentials),
			Data:       result.Credentials,
		})
		if err != nil {
			h.logError("Failed to save credentials", "provider_id", providerID, "error", err)
			h.jsonErr(w, http.StatusInternalServerError, "save credentials: "+err.Error())
			return
		}
		h.logInfo("Authentication completed", "provider_id", providerID, "credential_id", credential.ID)
		h.json(w, http.StatusOK, map[string]any{
			"status":        "complete",
			"message":       "Credentials saved successfully",
			"credential_id": credential.ID,
		})
	default:
		h.logError("Invalid authentication state", "provider_id", providerID)
		h.jsonErr(w, http.StatusInternalServerError, "invalid auth flow state")
	}
}

func isAuthFallback(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, luaplugin.ErrHandlerNotFound)
}

// firstNonEmpty returns the first non-empty value.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
