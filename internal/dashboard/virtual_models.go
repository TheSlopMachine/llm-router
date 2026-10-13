package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// ─────────────────────────────────────────────
// Virtual Model API Handlers
// ─────────────────────────────────────────────

// apiVirtualModelsList documentation
// @Summary      List all virtual models
// @Description  Returns a list of all configured virtual models
// @Tags         virtual-models
// @Security     SessionAuth
// @Produce      json
// @Success      200  {array}   models.VirtualModel
// @Failure      401  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/virtual-models [get]
func (h *Handler) apiVirtualModelsList(w http.ResponseWriter, r *http.Request) {
	virtualModels, err := h.gormVirtualService.List(r.Context())
	if err != nil {
		h.logError("Failed to list virtual models", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, "failed to list virtual models")
		return
	}
	h.json(w, http.StatusOK, virtualModels)
}

// apiVirtualModelsCreate documentation
// @Summary      Create a new virtual model
// @Description  Creates a new virtual model with the provided configuration
// @Tags         virtual-models
// @Security     SessionAuth
// @Accept       json
// @Produce      json
// @Param        model  body      models.VirtualModel  true  "Virtual model configuration"
// @Success      201    {object}  models.VirtualModel
// @Failure      400    {object}  models.ErrorResponse
// @Failure      401    {object}  models.ErrorResponse
// @Failure      500    {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/virtual-models [post]
func (h *Handler) apiVirtualModelsCreate(w http.ResponseWriter, r *http.Request) {
	var virtualModel models.VirtualModel
	if err := json.NewDecoder(r.Body).Decode(&virtualModel); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.gormVirtualService.Create(r.Context(), &virtualModel); err != nil {
		h.logError("Failed to create virtual model", "error", err)
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	h.logInfo("Virtual model created", "model", virtualModel.ID)
	h.json(w, http.StatusCreated, virtualModel)
}

// apiVirtualModelsGet documentation
// @Summary      Get virtual model by ID
// @Description  Returns a single virtual model by ID
// @Tags         virtual-models
// @Security     SessionAuth
// @Produce      json
// @Param        id   path      string  true  "Virtual model ID"
// @Success      200  {object}  models.VirtualModel
// @Failure      401  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/virtual-models/{id} [get]
func (h *Handler) apiVirtualModelsGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.jsonErr(w, http.StatusBadRequest, "virtual model ID is required")
		return
	}

	virtualModel, err := h.gormVirtualService.Get(r.Context(), id)
	if err != nil {
		h.logError("Failed to read virtual model", "model", id, "error", err)
		h.jsonErr(w, http.StatusNotFound, "virtual model not found")
		return
	}

	h.json(w, http.StatusOK, virtualModel)
}

// managedUpdateIsToggleOnly reports whether an update to a managed virtual
// model touches nothing but the disabled toggle. Stored aggregates and ids
// are recomputed server-side and ignored here; an omitted marker counts as
// unchanged.
func managedUpdateIsToggleOnly(existing *models.VirtualModel, incoming *models.VirtualModel) bool {
	if existing.Name != incoming.Name ||
		existing.Description != incoming.Description ||
		existing.Instruction != incoming.Instruction ||
		(incoming.ManagedBy != "" && incoming.ManagedBy != existing.ManagedBy) {
		return false
	}
	if len(existing.Models) != len(incoming.Models) {
		return false
	}
	for i := range existing.Models {
		if existing.Models[i].ModelID != incoming.Models[i].ModelID {
			return false
		}
	}
	return true
}

// apiVirtualModelsUpdate documentation
// @Summary      Update a virtual model
// @Description  Updates an existing virtual model with the provided configuration
// @Tags         virtual-models
// @Security     SessionAuth
// @Accept       json
// @Produce      json
// @Param        id     path      string        true  "Virtual model ID"
// @Param        model  body      models.VirtualModel  true  "Virtual model configuration"
// @Success      200    {object}  models.VirtualModel
// @Failure      400    {object}  models.ErrorResponse
// @Failure      401    {object}  models.ErrorResponse
// @Failure      404    {object}  models.ErrorResponse
// @Failure      500    {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/virtual-models/{id} [put]
func (h *Handler) apiVirtualModelsUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.jsonErr(w, http.StatusBadRequest, "virtual model ID is required")
		return
	}

	var virtualModel models.VirtualModel
	if err := json.NewDecoder(r.Body).Decode(&virtualModel); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Managed virtual models are system-owned: members refresh from the
	// provider list, so only the disabled toggle is user-editable.
	if existing, err := h.gormVirtualService.Get(r.Context(), id); err == nil && existing.ManagedBy != "" {
		if !managedUpdateIsToggleOnly(existing, &virtualModel) {
			h.jsonErr(w, http.StatusBadRequest, "managed virtual model is read-only except for the disabled toggle")
			return
		}
		virtualModel.ManagedBy = existing.ManagedBy
	}

	if err := h.gormVirtualService.Update(r.Context(), id, &virtualModel); err != nil {
		h.logError("Failed to update virtual model", "model", id, "error", err)
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Fetch updated virtual model
	updated, err := h.gormVirtualService.Get(r.Context(), id)
	if err != nil {
		h.logError("Failed to read updated virtual model", "model", id, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, "failed to get updated virtual model")
		return
	}

	h.logInfo("Virtual model updated", "model", id)
	h.json(w, http.StatusOK, updated)
}

// apiVirtualModelsDelete documentation
// @Summary      Delete a virtual model
// @Description  Deletes a virtual model by ID
// @Tags         virtual-models
// @Security     SessionAuth
// @Produce      json
// @Param        id   path      string  true  "Virtual model ID"
// @Success      204  "No Content"
// @Failure      401  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /api/llm-router/dashboard/virtual-models/{id} [delete]
func (h *Handler) apiVirtualModelsDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.jsonErr(w, http.StatusBadRequest, "virtual model ID is required")
		return
	}

	if err := h.gormVirtualService.Delete(r.Context(), id); err != nil {
		h.logError("Failed to delete virtual model", "model", id, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, "failed to delete virtual model")
		return
	}

	h.logInfo("Virtual model deleted", "model", id)
	w.WriteHeader(http.StatusNoContent)
}
