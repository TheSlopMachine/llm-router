package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
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
// @Success      200  {array}   models.Agent
// @Failure      401  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/agents [get]
func (h *Handler) apiAgentsList(w http.ResponseWriter, r *http.Request) {
	agents, err := h.agentSvc.List()
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
// @Param        agent  body      models.Agent  true  "Agent configuration"
// @Success      201    {object}  models.Agent
// @Failure      400    {object}  models.ErrorResponse
// @Failure      401    {object}  models.ErrorResponse
// @Failure      500    {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/agents [post]
func (h *Handler) apiAgentsCreate(w http.ResponseWriter, r *http.Request) {
	var agent models.Agent
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.agentSvc.Create(&agent); err != nil {
		h.logger.Error("failed to create agent", "error", err)
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	h.json(w, http.StatusCreated, agent)
}

// apiAgentsGet godoc
// @Summary      Get agent by ID
// @Description  Returns a single agent by ID
// @Tags         agents
// @Security     SessionAuth
// @Produce      json
// @Param        id   path      string  true  "Agent ID"
// @Success      200  {object}  models.Agent
// @Failure      401  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/agents/{id} [get]
func (h *Handler) apiAgentsGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.jsonErr(w, http.StatusBadRequest, "agent ID is required")
		return
	}

	agent, err := h.agentSvc.Get(id)
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
// @Param        agent  body      models.Agent  true  "Agent configuration"
// @Success      200    {object}  models.Agent
// @Failure      400    {object}  models.ErrorResponse
// @Failure      401    {object}  models.ErrorResponse
// @Failure      404    {object}  models.ErrorResponse
// @Failure      500    {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/agents/{id} [put]
func (h *Handler) apiAgentsUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.jsonErr(w, http.StatusBadRequest, "agent ID is required")
		return
	}

	var agent models.Agent
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.agentSvc.Update(id, &agent); err != nil {
		h.logger.Error("failed to update agent", "id", id, "error", err)
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Fetch updated agent
	updated, err := h.agentSvc.Get(id)
	if err != nil {
		h.logger.Error("failed to get updated agent", "id", id, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, "failed to get updated agent")
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
		h.jsonErr(w, http.StatusBadRequest, "agent ID is required")
		return
	}

	if err := h.agentSvc.Delete(id); err != nil {
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
	items, err := h.availableModels(r.Context())
	if err != nil {
		h.logger.Error("failed to list available models", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, "failed to list available models")
		return
	}

	allModels := make([]models.ModelInfo, 0, len(items))
	for _, item := range items {
		allModels = append(allModels, models.ModelInfo{
			Name:          item.FullModelID,
			DisplayName:   item.ProviderName + " - " + item.DisplayName,
			ContextWindow: item.ContextWindow,
			MaxTokens:     item.MaxTokens,
		})
	}

	h.json(w, http.StatusOK, allModels)
}
