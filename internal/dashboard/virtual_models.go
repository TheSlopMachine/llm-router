package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// ─────────────────────────────────────────────
// Virtual Model API Handlers
// ─────────────────────────────────────────────

// apiVirtualModelsList godoc
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
	vms, err := h.virtualSvc.List()
	if err != nil {
		h.logger.Error("failed to list virtual models", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, "failed to list virtual models")
		return
	}
	h.json(w, http.StatusOK, vms)
}

// apiVirtualModelsCreate godoc
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

// apiVirtualModelsGet godoc
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

	vm, err := h.virtualSvc.Get(id)
	if err != nil {
		h.logger.Error("failed to get virtual model", "id", id, "error", err)
		h.jsonErr(w, http.StatusNotFound, "virtual model not found")
		return
	}

	h.json(w, http.StatusOK, vm)
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

// apiVirtualModelsUpdate godoc
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

	var vm models.VirtualModel
	if err := json.NewDecoder(r.Body).Decode(&vm); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Managed virtual models are system-owned: members refresh from the
	// provider list, so only the disabled toggle is user-editable.
	if existing, err := h.virtualSvc.Get(id); err == nil && existing.ManagedBy != "" {
		if !managedUpdateIsToggleOnly(existing, &vm) {
			h.jsonErr(w, http.StatusBadRequest, "managed virtual model is read-only except for the disabled toggle")
			return
		}
		vm.ManagedBy = existing.ManagedBy
	}

	if err := h.virtualSvc.Update(id, &vm); err != nil {
		h.logger.Error("failed to update virtual model", "id", id, "error", err)
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Fetch updated virtual model
	updated, err := h.virtualSvc.Get(id)
	if err != nil {
		h.logger.Error("failed to get updated virtual model", "id", id, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, "failed to get updated virtual model")
		return
	}

	h.json(w, http.StatusOK, updated)
}

// apiVirtualModelsDelete godoc
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

	if err := h.virtualSvc.Delete(id); err != nil {
		h.logger.Error("failed to delete virtual model", "id", id, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, "failed to delete virtual model")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
