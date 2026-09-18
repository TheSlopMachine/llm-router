package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

// ─────────────────────────────────────────────
// Agent API Handlers
// ─────────────────────────────────────────────

// apiAgentsList godoc
// @Summary      List all agents
// @Description  Returns a list of all configured agents
// @Tags         agents
// @Security     SessionAuth
// @Produce      json
// @Success      200  {array}   models.VirtualModel
// @Failure      401  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/agents [get]
func (h *Handler) apiAgentsList(w http.ResponseWriter, r *http.Request) {
	agents, err := h.virtualSvc.List()
	if err != nil {
		h.logger.Error("failed to list agents", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	h.json(w, http.StatusOK, agents)
}

// apiAgentsCreate godoc
// @Summary      Create a new agent
// @Description  Creates a new agent with the provided configuration
// @Tags         agents
// @Security     SessionAuth
// @Accept       json
// @Produce      json
// @Param        agent  body      models.VirtualModel  true  "Agent configuration"
// @Success      201    {object}  models.VirtualModel
// @Failure      400    {object}  models.ErrorResponse
// @Failure      401    {object}  models.ErrorResponse
// @Failure      500    {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/agents [post]
func (h *Handler) apiAgentsCreate(w http.ResponseWriter, r *http.Request) {
	var vm models.VirtualModel
	if err := json.NewDecoder(r.Body).Decode(&vm); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.virtualSvc.Create(&vm); err != nil {
		h.logger.Error("failed to create virtual model", "error", err)
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	h.json(w, http.StatusCreated, vm)
}

// apiAgentsGet godoc
// @Summary      Get agent by ID
// @Description  Returns a single agent by ID
// @Tags         agents
// @Security     SessionAuth
// @Produce      json
// @Param        id   path      string  true  "Agent ID"
// @Success      200  {object}  models.VirtualModel
// @Failure      401  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/agents/{id} [get]
func (h *Handler) apiAgentsGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.jsonErr(w, http.StatusBadRequest, "virtual model ID is required")
		return
	}

	agent, err := h.virtualSvc.Get(id)
	if err != nil {
		h.logger.Error("failed to get agent", "id", id, "error", err)
		h.jsonErr(w, http.StatusNotFound, "agent not found")
		return
	}

	h.json(w, http.StatusOK, agent)
}

// apiAgentsUpdate godoc
// @Summary      Update an agent
// @Description  Updates an existing agent with the provided configuration
// @Tags         agents
// @Security     SessionAuth
// @Accept       json
// @Produce      json
// @Param        id     path      string        true  "Agent ID"
// @Param        agent  body      models.VirtualModel  true  "Agent configuration"
// @Success      200    {object}  models.VirtualModel
// @Failure      400    {object}  models.ErrorResponse
// @Failure      401    {object}  models.ErrorResponse
// @Failure      404    {object}  models.ErrorResponse
// @Failure      500    {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/agents/{id} [put]
func (h *Handler) apiAgentsUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.jsonErr(w, http.StatusBadRequest, "virtual model ID is required")
		return
	}

	var vm models.VirtualModel
	if err := json.NewDecoder(r.Body).Decode(&vm); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.virtualSvc.Update(id, &vm); err != nil {
		h.logger.Error("failed to update virtual model", "id", id, "error", err)
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Fetch updated agent
	updated, err := h.virtualSvc.Get(id)
	if err != nil {
		h.logger.Error("failed to get updated virtual model", "id", id, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, "failed to get updated virtual model")
		return
	}

	h.json(w, http.StatusOK, updated)
}

// apiAgentsDelete godoc
// @Summary      Delete an agent
// @Description  Deletes an agent by ID
// @Tags         agents
// @Security     SessionAuth
// @Produce      json
// @Param        id   path      string  true  "Agent ID"
// @Success      204  "No Content"
// @Failure      401  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/agents/{id} [delete]
func (h *Handler) apiAgentsDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.jsonErr(w, http.StatusBadRequest, "virtual model ID is required")
		return
	}

	if err := h.virtualSvc.Delete(id); err != nil {
		h.logger.Error("failed to delete agent", "id", id, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, "failed to delete agent")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// apiAgentsAvailableModels godoc
// @Summary      List available models for agents
// @Description  Returns a list of all available models that can be used in agents
// @Tags         agents
// @Security     SessionAuth
// @Produce      json
// @Success      200  {array}   models.ModelInfo
// @Failure      401  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/agents/available-models [get]
func (h *Handler) apiAgentsAvailableModels(w http.ResponseWriter, r *http.Request) {
	items, err := h.availableModels()
	if err != nil {
		h.logger.Error("failed to list available models", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, "failed to list available models")
		return
	}

	// Virtual models are not valid fall-through targets (circular dependency).
	allModels := make([]models.ModelInfo, 0, len(items))
	for _, item := range items {
		if item.ProviderType == provider.TypeVirtual {
			continue
		}
		allModels = append(allModels, models.ModelInfo{
			Name:          item.FullModelID,
			DisplayName:   item.ProviderName + " - " + item.DisplayName,
			ContextWindow: item.ContextWindow,
			MaxTokens:     item.MaxTokens,
			Capabilities:  item.Capabilities,
		})
	}

	h.json(w, http.StatusOK, allModels)
}
