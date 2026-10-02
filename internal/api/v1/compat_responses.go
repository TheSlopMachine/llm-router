package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/httpkit"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// listEnvelope wraps compat listings in the OpenAI {object:list,data} shape,
// arrays never null.
func listEnvelope[T any](items []*T) map[string]any {
	if items == nil {
		items = []*T{}
	}
	return map[string]any{"object": "list", "data": items}
}

// ─────────────────────────────────────────────
// Responses (POST /v1/responses family)
// ─────────────────────────────────────────────

// createResponse handles POST /v1/responses
// @Summary      Create response
// @Description  Creates a model response through the chat pipeline. Input accepts a
// @Description  string or response input items; function tools execute inline.
// @Description  Responses persist as resp_* rows unless store is false. Background
// @Description  is accepted and executed inline. Streams response.* SSE events
// @Description  when stream is true.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        request body models.ResponseRequest true "Response request"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ResponseObject "Response"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      403 {object} models.OpenAIError "Forbidden - model not allowed by token rules"
// @Failure      502 {object} models.OpenAIError "Bad Gateway - upstream provider error"
// @Failure      503 {object} models.OpenAIError "Service Unavailable - no credential available"
// @Router       /v1/responses [post]
// @Security     BearerAuth
func (h *Handler) createResponse(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	var req models.ResponseRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if req.Model == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", strPtr("model"))
		return
	}
	if deny := authorizeModel(t, req.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.msg, deny.param)
		return
	}
	resp, err := h.router.CreateResponse(r.Context(), &req, t)
	duration := time.Since(start)
	var usage *models.ChatCompletionUsage
	if err == nil && resp != nil && resp.Usage != nil {
		usage = &models.ChatCompletionUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	h.recordRouteMetric(r.Context(), start, req.Model, t, duration, err, usage)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	if req.Stream {
		h.streamResponseObject(w, r, resp)
		return
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// streamResponseObject emits a completed response as response.* SSE events.
func (h *Handler) streamResponseObject(w http.ResponseWriter, r *http.Request, resp *models.ResponseObject) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "server_error", "streaming is not supported by this server", nil)
		return
	}
	httpkit.WriteSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	emit := func(event string, v any) {
		raw, err := json.Marshal(v)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(raw))
		flusher.Flush()
	}
	creating := *resp
	creating.Status = "in_progress"
	emit("response.created", creating)
	for i, item := range resp.Output {
		if item.Type == "message" && item.Text != "" {
			emit("response.output_text.delta", map[string]any{"output_index": i, "delta": item.Text})
		}
	}
	emit("response.completed", resp)
}

// getResponse handles GET /v1/responses/{response_id}
// @Summary      Retrieve response
// @Description  Returns one stored resp_* response. Token rules apply to the stored model.
// @Tags         OpenAI API
// @Produce      json
// @Param        response_id path string true "Response ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ResponseObject "Response"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      403 {object} models.OpenAIError "Forbidden - model not allowed by token rules"
// @Failure      404 {object} models.OpenAIError "Unknown response"
// @Router       /v1/responses/{response_id} [get]
// @Security     BearerAuth
func (h *Handler) getResponse(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("response_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "response id is required", nil)
		return
	}
	resp, err := h.router.GetResponse(r.Context(), id, t)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// cancelResponse handles POST /v1/responses/{response_id}/cancel
// @Summary      Cancel response
// @Description  Marks a non-terminal response canceled. Synchronously executed
// @Description  responses are terminal and report 400.
// @Tags         OpenAI API
// @Produce      json
// @Param        response_id path string true "Response ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ResponseObject "Canceled response"
// @Failure      400 {object} models.OpenAIError "Response already terminal"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown response"
// @Router       /v1/responses/{response_id}/cancel [post]
// @Security     BearerAuth
func (h *Handler) cancelResponse(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("response_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "response id is required", nil)
		return
	}
	resp, err := h.router.CancelResponse(r.Context(), id, t)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// listResponseInputItems handles GET /v1/responses/{response_id}/input_items
// @Summary      List response input items
// @Description  Returns the stored input of a response as items.
// @Tags         OpenAI API
// @Produce      json
// @Param        response_id path string true "Response ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.OpenAIError "Input item list"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown response"
// @Router       /v1/responses/{response_id}/input_items [get]
// @Security     BearerAuth
func (h *Handler) listResponseInputItems(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("response_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "response id is required", nil)
		return
	}
	items, err := h.router.ListResponseInputItems(r.Context(), id, t)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, listEnvelopeItems(items))
}

// compactResponse handles POST /v1/responses/compact
// @Summary      Compact response
// @Description  Collapses a response input chain. The router stores flat inputs,
// @Description  so compaction returns the stored object unchanged.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ResponseObject "Compacted response"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown response"
// @Router       /v1/responses/compact [post]
// @Security     BearerAuth
func (h *Handler) compactResponse(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	var body struct {
		ResponseID string `json:"response_id"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if strings.TrimSpace(body.ResponseID) == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'response_id'", strPtr("response_id"))
		return
	}
	resp, err := h.router.CompactResponse(r.Context(), strings.TrimSpace(body.ResponseID), t)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// countResponseInputTokens handles POST /v1/responses/input_tokens
// @Summary      Count response input tokens
// @Description  Estimates input tokens for a response request with the documented
// @Description  4-chars-per-token heuristic. No upstream call runs.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        request body models.ResponseRequest true "Response request to count"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.OpenAIError "Token count"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Router       /v1/responses/input_tokens [post]
// @Security     BearerAuth
func (h *Handler) countResponseInputTokens(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	var req models.ResponseRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if req.Model != "" {
		if deny := authorizeModel(t, req.Model); deny != nil {
			h.writeError(w, deny.status, deny.code, deny.msg, deny.param)
			return
		}
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"input_tokens": h.router.CountResponseInputTokens(&req)})
}

// listEnvelopeItems wraps input items in the list envelope.
func listEnvelopeItems(items []models.ResponseInputItem) map[string]any {
	if items == nil {
		items = []models.ResponseInputItem{}
	}
	return map[string]any{"object": "list", "data": items}
}

// ─────────────────────────────────────────────
// Conversations (POST /v1/conversations family)
// ─────────────────────────────────────────────

// createConversation handles POST /v1/conversations
// @Summary      Create conversation
// @Description  Creates a conversation container sharing the responses store.
// @Description  Containers carry no model until model-bound items arrive.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ConversationObject "Conversation"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Router       /v1/conversations [post]
// @Security     BearerAuth
func (h *Handler) createConversation(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	var body struct {
		Metadata map[string]string          `json:"metadata"`
		Items    []models.ResponseInputItem `json:"items"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	conv, err := h.router.CreateConversation(r.Context(), "", body.Metadata, body.Items)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, conv)
}

// getConversation handles GET /v1/conversations/{conversation_id}
// @Summary      Retrieve conversation
// @Description  Returns one stored conversation container.
// @Tags         OpenAI API
// @Produce      json
// @Param        conversation_id path string true "Conversation ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ConversationObject "Conversation"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown conversation"
// @Router       /v1/conversations/{conversation_id} [get]
// @Security     BearerAuth
func (h *Handler) getConversation(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("conversation_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "conversation id is required", nil)
		return
	}
	conv, err := h.router.GetConversation(r.Context(), id, t)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, conv)
}

// deleteConversation handles DELETE /v1/conversations/{conversation_id}
// @Summary      Delete conversation
// @Description  Removes a conversation container. Responses filed under it stay
// @Description  addressable by response id.
// @Tags         OpenAI API
// @Produce      json
// @Param        conversation_id path string true "Conversation ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.OpenAIError "Deletion receipt"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown conversation"
// @Router       /v1/conversations/{conversation_id} [delete]
// @Security     BearerAuth
func (h *Handler) deleteConversation(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("conversation_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "conversation id is required", nil)
		return
	}
	if err := h.router.DeleteConversation(r.Context(), id); err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"id": id, "object": "conversation.deleted", "deleted": true})
}

// listConversationItems handles GET /v1/conversations/{conversation_id}/items
// @Summary      List conversation items
// @Description  Returns the items appended to a conversation.
// @Tags         OpenAI API
// @Produce      json
// @Param        conversation_id path string true "Conversation ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.OpenAIError "Item list"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown conversation"
// @Router       /v1/conversations/{conversation_id}/items [get]
// @Security     BearerAuth
func (h *Handler) listConversationItems(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("conversation_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "conversation id is required", nil)
		return
	}
	items, err := h.router.ListConversationItems(r.Context(), id, t)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, listEnvelopeItems(items))
}

// createConversationItems handles POST /v1/conversations/{conversation_id}/items
// @Summary      Append conversation items
// @Description  Appends items to a conversation and returns the full item list.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        conversation_id path string true "Conversation ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.OpenAIError "Item list"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown conversation"
// @Router       /v1/conversations/{conversation_id}/items [post]
// @Security     BearerAuth
func (h *Handler) createConversationItems(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("conversation_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "conversation id is required", nil)
		return
	}
	var body struct {
		Items []models.ResponseInputItem `json:"items"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	items, err := h.router.AppendConversationItems(r.Context(), id, t, body.Items)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, listEnvelopeItems(items))
}
