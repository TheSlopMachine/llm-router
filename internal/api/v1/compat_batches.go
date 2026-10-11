package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// decodeBatchParams parses one batch entry's params for edge authorization.
// Full entry validation stays in the router; the edge needs only the model.
func decodeBatchParams(raw json.RawMessage, message *models.AnthropicMessageRequest) error {
	if len(raw) == 0 {
		return errMissingBatchParams()
	}
	if err := json.Unmarshal(raw, message); err != nil {
		return errMissingBatchParams()
	}
	if message.Model == "" {
		return errMissingBatchParams()
	}
	return nil
}

func errMissingBatchParams() error {
	return fmt.Errorf("batch entry params must be a message request with model")
}

// batchModel reports the first entry model for metrics.
func batchModel(request *models.BatchCreateRequest) models.ModelId {
	for _, entry := range request.Requests {
		var message models.AnthropicMessageRequest
		if err := json.Unmarshal(entry.Params, &message); err == nil && message.Model != "" {
			return models.ModelId(message.Model)
		}
	}
	return ""
}

// ─────────────────────────────────────────────
// Anthropic message batches (POST|GET /v1/messages/batches family)
// ─────────────────────────────────────────────

// createBatch handles POST /v1/messages/batches: entries validate, execute
// through the chat pipeline in order, and persist as an ended batch.
// Custom IDs stay unique per batch; per-entry failures land as errored
// result lines, never as create failures.
// @Summary      Create message batch (Anthropic)
// @Description  Creates and synchronously executes a message batch. Entries run
// @Description  through the chat pipeline in order; the batch returns ended with
// @Description  per-entry succeeded/errored result lines. Errors use the
// @Description  negotiated envelope.
// @Tags         Anthropic API
// @Accept       json
// @Produce      json
// @Param        request body models.BatchCreateRequest true "Batch request"
// @Param        anthropic-version header string false "Anthropic API version (accepted, never pinned)"
// @Param        anthropic-beta header string false "Anthropic beta flags (passthrough)"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.BatchInfo "Batch"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      403 {object} models.OpenAIError "Forbidden - model not allowed by token rules"
// @Failure      502 {object} models.OpenAIError "Bad Gateway - upstream provider error"
// @Failure      503 {object} models.OpenAIError "Service Unavailable - no credential available"
// @Router       /v1/messages/batches [post]
// @Security     BearerAuth
func (h *Handler) createBatch(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	var request models.BatchCreateRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		h.writeCompatDecodeError(w, r, err)
		return
	}
	if len(request.Requests) == 0 {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'requests'", stringPointer("requests"))
		return
	}
	for _, entry := range request.Requests {
		var message models.AnthropicMessageRequest
		if err := decodeBatchParams(entry.Params, &message); err != nil {
			h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
			return
		}
		if deny := authorizeModel(token, models.ModelId(message.Model)); deny != nil {
			h.writeCompatError(w, r, deny.status, deny.code, deny.message, deny.parameter)
			return
		}
	}
	batch, err := h.router.CreateBatch(r.Context(), &request, token)
	duration := time.Since(start)
	h.recordRouteMetric(r.Context(), start, batchModel(&request), token, duration, err, nil)
	if err != nil {
		h.handleCompatRouterError(w, r, err)
		return
	}
	echoAnthropicVersion(w, r)
	h.writeJSON(w, http.StatusOK, batch)
}

// listBatches handles GET /v1/messages/batches
// @Summary      List message batches (Anthropic)
// @Description  Returns all stored message batches, oldest first.
// @Tags         Anthropic API
// @Produce      json
// @Param        anthropic-version header string false "Anthropic API version (accepted, never pinned)"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.OpenAIError "Batch list"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Router       /v1/messages/batches [get]
// @Security     BearerAuth
func (h *Handler) listBatches(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	_ = token
	items, err := h.router.ListBatches(r.Context())
	if err != nil {
		h.handleCompatRouterError(w, r, err)
		return
	}
	if items == nil {
		items = []*models.BatchInfo{}
	}
	echoAnthropicVersion(w, r)
	h.writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": items, "has_more": false})
}

// getBatch handles GET /v1/messages/batches/{batch_id}
// @Summary      Retrieve message batch (Anthropic)
// @Description  Returns one stored message batch.
// @Tags         Anthropic API
// @Produce      json
// @Param        batch_id path string true "Batch ID"
// @Param        anthropic-version header string false "Anthropic API version (accepted, never pinned)"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.BatchInfo "Batch"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown batch"
// @Router       /v1/messages/batches/{batch_id} [get]
// @Security     BearerAuth
func (h *Handler) getBatch(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	id, ok := h.requirePathID(w, r, "batch_id", "batch")
	if !ok {
		return
	}
	batch, err := h.router.GetBatch(r.Context(), id, token)
	if err != nil {
		h.handleCompatRouterError(w, r, err)
		return
	}
	echoAnthropicVersion(w, r)
	h.writeJSON(w, http.StatusOK, batch)
}

// getBatchResults handles GET /v1/messages/batches/{batch_id}/results
// @Summary      Fetch batch results (Anthropic)
// @Description  Returns stored batch results as JSONL lines {custom_id,result}.
// @Tags         Anthropic API
// @Produce      json
// @Param        batch_id path string true "Batch ID"
// @Param        anthropic-version header string false "Anthropic API version (accepted, never pinned)"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {string} string "JSONL results"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown batch"
// @Router       /v1/messages/batches/{batch_id}/results [get]
// @Security     BearerAuth
func (h *Handler) getBatchResults(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	id, ok := h.requirePathID(w, r, "batch_id", "batch")
	if !ok {
		return
	}
	lines, err := h.router.GetBatchResults(r.Context(), id, token)
	if err != nil {
		h.handleCompatRouterError(w, r, err)
		return
	}
	echoAnthropicVersion(w, r)
	w.Header().Set("Content-Type", "application/jsonl")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(lines))
}

// cancelBatch handles POST /v1/messages/batches/{batch_id}/cancel
// @Summary      Cancel message batch (Anthropic)
// @Description  Marks a non-terminal batch canceled. Synchronously executed
// @Description  batches are terminal and report 400.
// @Tags         Anthropic API
// @Produce      json
// @Param        batch_id path string true "Batch ID"
// @Param        anthropic-version header string false "Anthropic API version (accepted, never pinned)"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.BatchInfo "Batch"
// @Failure      400 {object} models.OpenAIError "Batch already terminal"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown batch"
// @Router       /v1/messages/batches/{batch_id}/cancel [post]
// @Security     BearerAuth
func (h *Handler) cancelBatch(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	id, ok := h.requirePathID(w, r, "batch_id", "batch")
	if !ok {
		return
	}
	batch, err := h.router.CancelBatch(r.Context(), id, token)
	if err != nil {
		h.handleCompatRouterError(w, r, err)
		return
	}
	echoAnthropicVersion(w, r)
	h.writeJSON(w, http.StatusOK, batch)
}

// deleteBatch handles DELETE /v1/messages/batches/{batch_id}
// @Summary      Delete message batch (Anthropic)
// @Description  Removes a stored message batch.
// @Tags         Anthropic API
// @Produce      json
// @Param        batch_id path string true "Batch ID"
// @Param        anthropic-version header string false "Anthropic API version (accepted, never pinned)"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.OpenAIError "Deletion receipt"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown batch"
// @Router       /v1/messages/batches/{batch_id} [delete]
// @Security     BearerAuth
func (h *Handler) deleteBatch(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	id, ok := h.requirePathID(w, r, "batch_id", "batch")
	if !ok {
		return
	}
	_ = token
	if err := h.router.DeleteBatch(r.Context(), id); err != nil {
		h.handleCompatRouterError(w, r, err)
		return
	}
	echoAnthropicVersion(w, r)
	h.writeJSON(w, http.StatusOK, map[string]any{"id": id, "type": "message_batch_deleted", "deleted": true})
}
