package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// apiConfigGet returns the instance RouterConfiguration
// @Summary      Get router configuration
// @Description  Returns the deployment-wide router configuration.
// @Tags         Config
// @Produce      json
// @Success      200 {object} models.RouterConfiguration
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/config [get]
func (h *Handler) apiConfigGet(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.configSvc.Get()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, cfg)
}

// apiConfigPut updates the instance RouterConfiguration
// @Summary      Update router configuration
// @Description  Updates the deployment-wide router configuration.
// @Tags         Config
// @Accept       json
// @Produce      json
// @Param        body body models.RouterConfiguration true "Router configuration"
// @Success      200 {object} models.RouterConfiguration
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/config [put]
func (h *Handler) apiConfigPut(w http.ResponseWriter, r *http.Request) {
	var body models.RouterConfiguration
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := body.Validate(); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.configSvc.Put(body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, body)
}
