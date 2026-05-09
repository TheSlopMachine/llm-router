package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
)

// apiTokensList lists all router tokens
// @Summary      List tokens
// @Description  Returns all issued router tokens (without secret values).
// @Tags         Tokens
// @Produce      json
// @Success      200 {array} models.RouterToken
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/tokens [get]
func (h *Handler) apiTokensList(w http.ResponseWriter, r *http.Request) {
	toks, err := h.tokenSvc.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, toks)
}

// apiTokensCreate creates a new router token
// @Summary      Create token
// @Description  Issues a new router token with optional model restrictions.
// @Tags         Tokens
// @Accept       json
// @Produce      json
// @Param        token body object{name=string,rules=object{allowed_models=[]string}} true "Token configuration"
// @Success      201 {object} models.RouterToken
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/tokens [post]
func (h *Handler) apiTokensCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Rules struct {
			AllowedModels []models.ModelId `json:"allowed_models"`
		} `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	t, err := h.tokenSvc.Create(token.CreateOptions{
		Name:  body.Name,
		Rules: models.TokenRules{AllowedModels: body.Rules.AllowedModels},
	})
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusCreated, t)
}

// apiTokensUpdate updates a router token
// @Summary      Update token
// @Description  Updates token rules (allowed models).
// @Tags         Tokens
// @Accept       json
// @Produce      json
// @Param        id path string true "Token ID"
// @Param        token body object{name=string,rules=object{allowed_models=[]string}} true "Updated token configuration"
// @Success      200 {object} models.RouterToken
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/tokens/{id} [put]
func (h *Handler) apiTokensUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rules struct {
			AllowedModels []models.ModelId `json:"allowed_models"`
		} `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.tokenSvc.UpdateRules(r.PathValue("id"), models.TokenRules{AllowedModels: body.Rules.AllowedModels}); err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// apiTokensDelete revokes a router token
// @Summary      Revoke token
// @Description  Permanently revokes a router token.
// @Tags         Tokens
// @Produce      json
// @Param        id path string true "Token ID"
// @Success      204 "No Content"
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/tokens/{id} [delete]
func (h *Handler) apiTokensDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.tokenSvc.Delete(r.PathValue("id")); err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

