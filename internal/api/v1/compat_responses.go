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
func (h *Handler) createResponse(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	var request models.ResponseRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	if request.Model == "" {
		h.logValidationFailure(r, "", "invalid_request_error")
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", stringPointer("model"))
		return
	}
	if deny := authorizeModel(token, request.Model); deny != nil {
		h.logDenyFailure(r, request.Model, deny)
		h.writeError(w, deny.status, deny.code, deny.message, deny.parameter)
		return
	}
	response, err := h.router.CreateResponse(r.Context(), &request, token)
	duration := time.Since(start)
	var usage *models.ChatCompletionUsage
	if err == nil && response != nil && response.Usage != nil {
		usage = &models.ChatCompletionUsage{
			PromptTokens:     response.Usage.InputTokens,
			CompletionTokens: response.Usage.OutputTokens,
			TotalTokens:      response.Usage.TotalTokens,
		}
	}
	h.recordRouteMetric(r.Context(), start, request.Model, token, duration, err, usage)
	if err != nil {
		h.logRouterFailure(r, request.Model, err)
		h.handleRouterError(w, err)
		return
	}
	if request.Stream {
		h.streamResponseObject(w, r, response)
		return
	}
	h.writeJSON(w, http.StatusOK, response)
}

// streamResponseObject emits a completed response as response.* SSE events.
func (h *Handler) streamResponseObject(w http.ResponseWriter, r *http.Request, response *models.ResponseObject) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		if h.logger != nil {
			h.logger.Error("Streaming not supported",
				"endpoint", endpointFromRequest(r),
				"code", "server_error")
		}
		h.writeError(w, http.StatusInternalServerError, "server_error", "streaming is not supported by this server", nil)
		return
	}
	httpkit.WriteSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	for _, event := range models.ResponseStreamEvents(response) {
		raw, err := json.Marshal(event.Data)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Name, string(raw))
		flusher.Flush()
	}
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
func (h *Handler) getResponse(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	id, ok := h.requireOpenAIPathID(w, r, "response_id", "response")
	if !ok {
		return
	}
	response, err := h.router.GetResponse(r.Context(), id, token)
	if err != nil {
		h.logRouterFailure(r, "", err)
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, response)
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
func (h *Handler) cancelResponse(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	id, ok := h.requireOpenAIPathID(w, r, "response_id", "response")
	if !ok {
		return
	}
	response, err := h.router.CancelResponse(r.Context(), id, token)
	if err != nil {
		h.logRouterFailure(r, "", err)
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, response)
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
func (h *Handler) listResponseInputItems(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	id, ok := h.requireOpenAIPathID(w, r, "response_id", "response")
	if !ok {
		return
	}
	items, err := h.router.ListResponseInputItems(r.Context(), id, token)
	if err != nil {
		h.logRouterFailure(r, "", err)
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
func (h *Handler) compactResponse(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	var body struct {
		ResponseID string `json:"response_id"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	if strings.TrimSpace(body.ResponseID) == "" {
		h.logValidationFailure(r, "", "invalid_request_error")
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'response_id'", stringPointer("response_id"))
		return
	}
	response, err := h.router.CompactResponse(r.Context(), strings.TrimSpace(body.ResponseID), token)
	if err != nil {
		h.logRouterFailure(r, "", err)
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, response)
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
func (h *Handler) countResponseInputTokens(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	var request models.ResponseRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	if request.Model != "" {
		if deny := authorizeModel(token, request.Model); deny != nil {
			h.logDenyFailure(r, request.Model, deny)
			h.writeError(w, deny.status, deny.code, deny.message, deny.parameter)
			return
		}
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"input_tokens": h.router.CountResponseInputTokens(&request)})
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
func (h *Handler) createConversation(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	var body struct {
		Metadata map[string]string          `json:"metadata"`
		Items    []models.ResponseInputItem `json:"items"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	conversation, err := h.router.CreateConversation(r.Context(), "", body.Metadata, body.Items)
	if err != nil {
		h.logRouterFailure(r, "", err)
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, conversation)
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
func (h *Handler) getConversation(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	id, ok := h.requireOpenAIPathID(w, r, "conversation_id", "conversation")
	if !ok {
		return
	}
	conversation, err := h.router.GetConversation(r.Context(), id, token)
	if err != nil {
		h.logRouterFailure(r, "", err)
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, conversation)
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
func (h *Handler) deleteConversation(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	id, ok := h.requireOpenAIPathID(w, r, "conversation_id", "conversation")
	if !ok {
		return
	}
	if err := h.router.DeleteConversation(r.Context(), id); err != nil {
		h.logRouterFailure(r, "", err)
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
func (h *Handler) listConversationItems(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	id, ok := h.requireOpenAIPathID(w, r, "conversation_id", "conversation")
	if !ok {
		return
	}
	items, err := h.router.ListConversationItems(r.Context(), id, token)
	if err != nil {
		h.logRouterFailure(r, "", err)
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
func (h *Handler) createConversationItems(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	id, ok := h.requireOpenAIPathID(w, r, "conversation_id", "conversation")
	if !ok {
		return
	}
	var body struct {
		Items []models.ResponseInputItem `json:"items"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	items, err := h.router.AppendConversationItems(r.Context(), id, token, body.Items)
	if err != nil {
		h.logRouterFailure(r, "", err)
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, listEnvelopeItems(items))
}
