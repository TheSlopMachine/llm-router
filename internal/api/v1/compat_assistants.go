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

// ─────────────────────────────────────────────
// Assistants (POST|GET /v1/assistants family)
// ─────────────────────────────────────────────

// createAssistant handles POST /v1/assistants
// @Summary      Create assistant
// @Description  Stores an assistant preset (model, instructions, tools). Runs execute
// @Description  presets through the chat pipeline. First-class despite the upstream
// @Description  legacy status: old SDKs still call it.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        request body models.AssistantRequest true "Assistant request"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.Assistant "Assistant"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      403 {object} models.OpenAIError "Forbidden - model not allowed by token rules"
// @Router       /v1/assistants [post]
// @Security     BearerAuth
func (h *Handler) createAssistant(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	var req models.AssistantRequest
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
	out, err := h.router.CreateAssistant(r.Context(), &req)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, out)
}

// listAssistants handles GET /v1/assistants
// @Summary      List assistants
// @Description  Returns all stored assistant presets, oldest first.
// @Tags         OpenAI API
// @Produce      json
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.OpenAIError "Assistant list"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Router       /v1/assistants [get]
// @Security     BearerAuth
func (h *Handler) listAssistants(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	_ = t
	items, err := h.router.ListAssistants(r.Context())
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, listEnvelope(items))
}

// getAssistant handles GET /v1/assistants/{assistant_id}
// @Summary      Retrieve assistant
// @Description  Returns one stored assistant preset.
// @Tags         OpenAI API
// @Produce      json
// @Param        assistant_id path string true "Assistant ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.Assistant "Assistant"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown assistant"
// @Router       /v1/assistants/{assistant_id} [get]
// @Security     BearerAuth
func (h *Handler) getAssistant(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("assistant_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "assistant id is required", nil)
		return
	}
	out, err := h.router.GetAssistant(r.Context(), id, t)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, out)
}

// updateAssistant handles POST /v1/assistants/{assistant_id}
// @Summary      Modify assistant
// @Description  Patches a stored assistant preset.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        assistant_id path string true "Assistant ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.Assistant "Assistant"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown assistant"
// @Router       /v1/assistants/{assistant_id} [post]
// @Security     BearerAuth
func (h *Handler) updateAssistant(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("assistant_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "assistant id is required", nil)
		return
	}
	var req models.AssistantRequest
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
	out, err := h.router.UpdateAssistant(r.Context(), id, t, &req)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, out)
}

// deleteAssistant handles DELETE /v1/assistants/{assistant_id}
// @Summary      Delete assistant
// @Description  Removes an assistant preset. Runs referencing it stay addressable.
// @Tags         OpenAI API
// @Produce      json
// @Param        assistant_id path string true "Assistant ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.OpenAIError "Deletion receipt"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown assistant"
// @Router       /v1/assistants/{assistant_id} [delete]
// @Security     BearerAuth
func (h *Handler) deleteAssistant(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("assistant_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "assistant id is required", nil)
		return
	}
	_ = t
	if err := h.router.DeleteAssistant(r.Context(), id); err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"id": id, "object": "assistant.deleted", "deleted": true})
}

// ─────────────────────────────────────────────
// Threads (POST|GET|DELETE /v1/threads family)
// ─────────────────────────────────────────────

// createThread handles POST /v1/threads
// @Summary      Create thread
// @Description  Creates a thread handle on the shared responses store.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.Thread "Thread"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Router       /v1/threads [post]
// @Security     BearerAuth
func (h *Handler) createThread(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	var body struct {
		Metadata map[string]string `json:"metadata"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	_ = t
	out, err := h.router.CreateThread(r.Context(), body.Metadata)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, out)
}

// getThread handles GET /v1/threads/{thread_id}
// @Summary      Retrieve thread
// @Description  Returns one stored thread handle.
// @Tags         OpenAI API
// @Produce      json
// @Param        thread_id path string true "Thread ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.Thread "Thread"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown thread"
// @Router       /v1/threads/{thread_id} [get]
// @Security     BearerAuth
func (h *Handler) getThread(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("thread_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "thread id is required", nil)
		return
	}
	out, err := h.router.GetThread(r.Context(), id, t)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, out)
}

// updateThread handles POST /v1/threads/{thread_id}
// @Summary      Modify thread
// @Description  Patches thread metadata.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        thread_id path string true "Thread ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.Thread "Thread"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown thread"
// @Router       /v1/threads/{thread_id} [post]
// @Security     BearerAuth
func (h *Handler) updateThread(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("thread_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "thread id is required", nil)
		return
	}
	var body struct {
		Metadata map[string]string `json:"metadata"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	_ = t
	out, err := h.router.UpdateThread(r.Context(), id, body.Metadata)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, out)
}

// deleteThread handles DELETE /v1/threads/{thread_id}
// @Summary      Delete thread
// @Description  Removes a thread handle. Messages and runs referencing it stay
// @Description  addressable by id.
// @Tags         OpenAI API
// @Produce      json
// @Param        thread_id path string true "Thread ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.OpenAIError "Deletion receipt"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown thread"
// @Router       /v1/threads/{thread_id} [delete]
// @Security     BearerAuth
func (h *Handler) deleteThread(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("thread_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "thread id is required", nil)
		return
	}
	_ = t
	if err := h.router.DeleteThread(r.Context(), id); err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"id": id, "object": "thread.deleted", "deleted": true})
}

// createThreadMessage handles POST /v1/threads/{thread_id}/messages
// @Summary      Create thread message
// @Description  Appends a user or assistant message to a thread.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        thread_id path string true "Thread ID"
// @Param        request body models.ThreadMessageRequest true "Message request"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ThreadMessage "Thread message"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown thread"
// @Router       /v1/threads/{thread_id}/messages [post]
// @Security     BearerAuth
func (h *Handler) createThreadMessage(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("thread_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "thread id is required", nil)
		return
	}
	var req models.ThreadMessageRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	_ = t
	out, err := h.router.CreateThreadMessage(r.Context(), id, &req)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, out)
}

// listThreadMessages handles GET /v1/threads/{thread_id}/messages
// @Summary      List thread messages
// @Description  Returns all messages of a thread, oldest first.
// @Tags         OpenAI API
// @Produce      json
// @Param        thread_id path string true "Thread ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.OpenAIError "Message list"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown thread"
// @Router       /v1/threads/{thread_id}/messages [get]
// @Security     BearerAuth
func (h *Handler) listThreadMessages(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	id := strings.TrimSpace(r.PathValue("thread_id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "thread id is required", nil)
		return
	}
	_ = t
	items, err := h.router.ListThreadMessages(r.Context(), id)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, listEnvelope(items))
}

// getThreadMessage handles GET /v1/threads/{thread_id}/messages/{message_id}
// @Summary      Retrieve thread message
// @Description  Returns one thread message, asserting thread membership.
// @Tags         OpenAI API
// @Produce      json
// @Param        thread_id path string true "Thread ID"
// @Param        message_id path string true "Message ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ThreadMessage "Thread message"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown message"
// @Router       /v1/threads/{thread_id}/messages/{message_id} [get]
// @Security     BearerAuth
func (h *Handler) getThreadMessage(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	messageID := strings.TrimSpace(r.PathValue("message_id"))
	if threadID == "" || messageID == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "thread and message ids are required", nil)
		return
	}
	_ = t
	out, err := h.router.GetThreadMessage(r.Context(), threadID, messageID)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, out)
}

// ─────────────────────────────────────────────
// Runs (POST /v1/threads/{thread_id}/runs family)
// ─────────────────────────────────────────────

// createRun handles POST /v1/threads/{thread_id}/runs
// @Summary      Create run
// @Description  Executes an assistant over a thread through the chat pipeline. Runs
// @Description  with tool calls park in requires_action; completed runs append the
// @Description  assistant turn to the thread. Streams thread.run.* events when
// @Description  stream is true.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        thread_id path string true "Thread ID"
// @Param        request body models.RunRequest true "Run request"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.Run "Run"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      403 {object} models.OpenAIError "Forbidden - model not allowed by token rules"
// @Failure      404 {object} models.OpenAIError "Unknown thread or assistant"
// @Failure      502 {object} models.OpenAIError "Bad Gateway - upstream provider error"
// @Failure      503 {object} models.OpenAIError "Service Unavailable - no credential available"
// @Router       /v1/threads/{thread_id}/runs [post]
// @Security     BearerAuth
func (h *Handler) createRun(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	if threadID == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "thread id is required", nil)
		return
	}
	var req models.RunRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if req.AssistantID == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'assistant_id'", strPtr("assistant_id"))
		return
	}
	if req.Model != "" {
		if deny := authorizeModel(t, req.Model); deny != nil {
			h.writeError(w, deny.status, deny.code, deny.msg, deny.param)
			return
		}
	}
	run, err := h.router.CreateRun(r.Context(), threadID, &req, t)
	duration := time.Since(start)
	h.recordRouteMetric(r.Context(), start, req.Model, t, duration, err, nil)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	if req.Stream {
		h.streamRun(w, run)
		return
	}
	h.writeJSON(w, http.StatusOK, run)
}

// streamRun emits a completed run as thread.run.* SSE events.
func (h *Handler) streamRun(w http.ResponseWriter, run *models.Run) {
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
	creating := *run
	creating.Status = "in_progress"
	emit("thread.run.created", creating)
	emit("thread.run.in_progress", creating)
	emit("thread.run.completed", run)
}

// listRuns handles GET /v1/threads/{thread_id}/runs
// @Summary      List runs
// @Description  Returns all runs of a thread, oldest first.
// @Tags         OpenAI API
// @Produce      json
// @Param        thread_id path string true "Thread ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.OpenAIError "Run list"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown thread"
// @Router       /v1/threads/{thread_id}/runs [get]
// @Security     BearerAuth
func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	if threadID == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "thread id is required", nil)
		return
	}
	_ = t
	items, err := h.router.ListRuns(r.Context(), threadID)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, listEnvelope(items))
}

// getRun handles GET /v1/threads/{thread_id}/runs/{run_id}
// @Summary      Retrieve run
// @Description  Returns one run, asserting thread membership.
// @Tags         OpenAI API
// @Produce      json
// @Param        thread_id path string true "Thread ID"
// @Param        run_id path string true "Run ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.Run "Run"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown run"
// @Router       /v1/threads/{thread_id}/runs/{run_id} [get]
// @Security     BearerAuth
func (h *Handler) getRun(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	runID := strings.TrimSpace(r.PathValue("run_id"))
	if threadID == "" || runID == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "thread and run ids are required", nil)
		return
	}
	out, err := h.router.GetRun(r.Context(), threadID, runID, t)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, out)
}

// cancelRun handles POST /v1/threads/{thread_id}/runs/{run_id}/cancel
// @Summary      Cancel run
// @Description  Marks a non-terminal run canceled.
// @Tags         OpenAI API
// @Produce      json
// @Param        thread_id path string true "Thread ID"
// @Param        run_id path string true "Run ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.Run "Canceled run"
// @Failure      400 {object} models.OpenAIError "Run already terminal"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown run"
// @Router       /v1/threads/{thread_id}/runs/{run_id}/cancel [post]
// @Security     BearerAuth
func (h *Handler) cancelRun(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	runID := strings.TrimSpace(r.PathValue("run_id"))
	if threadID == "" || runID == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "thread and run ids are required", nil)
		return
	}
	out, err := h.router.CancelRun(r.Context(), threadID, runID, t)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, out)
}

// submitRunToolOutputs handles POST
// /v1/threads/{thread_id}/runs/{run_id}/submit_tool_outputs
// @Summary      Submit run tool outputs
// @Description  Continues a requires_action run: tool outputs join the stashed turns
// @Description  and the pipeline runs again.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        thread_id path string true "Thread ID"
// @Param        run_id path string true "Run ID"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.Run "Run"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown run"
// @Router       /v1/threads/{thread_id}/runs/{run_id}/submit_tool_outputs [post]
// @Security     BearerAuth
func (h *Handler) submitRunToolOutputs(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	runID := strings.TrimSpace(r.PathValue("run_id"))
	if threadID == "" || runID == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "thread and run ids are required", nil)
		return
	}
	var body struct {
		ToolOutputs []models.ToolOutput `json:"tool_outputs"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if len(body.ToolOutputs) == 0 {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'tool_outputs'", strPtr("tool_outputs"))
		return
	}
	out, err := h.router.SubmitToolOutputs(r.Context(), threadID, runID, t, body.ToolOutputs)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, out)
}
