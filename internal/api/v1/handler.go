// Package v1 implements the OpenAI-compatible /v1/... API endpoints.
//
// Incoming requests are:
//  1. Authenticated via the Internal Token Service
//  2. Validated against the token's rules (allowed models)
//  3. Routed to the appropriate provider by the Router Service
//  4. Translated back to OpenAI-compatible responses
package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/httpkit"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/metrics"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
)

// Handler holds the dependencies for the v1 API.
type Handler struct {
	tokens       *token.Service
	router       *router.Service
	metrics      *metrics.Service
	providerSvc  *provider.Service
	modelInfoSvc *modelinfo.Service
	virtualSvc   *virtual.Service
	logger       *slog.Logger
	noAuth       bool
}

// Register mounts all /v1 routes onto mux. Every route requires a valid
// router token: discovery endpoints included, so anonymous listing can no
// longer leak provider inventory.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/chat/completions", h.auth(h.chatCompletions, false))
	mux.HandleFunc("POST /v1/completions", h.auth(h.legacyCompletions, false))
	mux.HandleFunc("POST /v1/audio/transcriptions", h.auth(h.audioTranscriptions, false))
	mux.HandleFunc("POST /v1/audio/translations", h.auth(h.audioTranslations, false))
	mux.HandleFunc("POST /v1/audio/speech", h.auth(h.audioSpeech, false))
	mux.HandleFunc("POST /v1/images/generations", h.auth(h.imageGenerations, false))
	mux.HandleFunc("POST /v1/images/edits", h.auth(h.imageEdits, false))
	mux.HandleFunc("POST /v1/images/variations", h.auth(h.imageVariations, false))
	mux.HandleFunc("POST /v1/embeddings", h.auth(h.embeddings, false))
	mux.HandleFunc("POST /v1/moderations", h.auth(h.moderations, false))
	mux.HandleFunc("POST /v1/videos", h.auth(h.submitVideo, false))
	mux.HandleFunc("GET /v1/videos/models", h.auth(h.listVideoModels, false))
	mux.HandleFunc("GET /v1/videos/{jobId}", h.auth(h.pollVideo, false))
	mux.HandleFunc("GET /v1/videos/{jobId}/content", h.auth(h.videoContent, false))
	mux.HandleFunc("POST /v1/messages", h.auth(h.anthropicMessages, false))
	mux.HandleFunc("POST /v1/messages/count_tokens", h.auth(h.anthropicCountTokens, false))
	mux.HandleFunc("POST /v1/complete", h.auth(h.anthropicComplete, false))
	mux.HandleFunc("POST /v1/messages/batches", h.auth(h.createBatch, false))
	mux.HandleFunc("GET /v1/messages/batches", h.auth(h.listBatches, false))
	mux.HandleFunc("GET /v1/messages/batches/{batch_id}", h.auth(h.getBatch, false))
	mux.HandleFunc("GET /v1/messages/batches/{batch_id}/results", h.auth(h.getBatchResults, false))
	mux.HandleFunc("POST /v1/messages/batches/{batch_id}/cancel", h.auth(h.cancelBatch, false))
	mux.HandleFunc("DELETE /v1/messages/batches/{batch_id}", h.auth(h.deleteBatch, false))
	mux.HandleFunc("POST /v1/responses", h.auth(h.createResponse, false))
	mux.HandleFunc("GET /v1/responses/{response_id}", h.auth(h.getResponse, false))
	mux.HandleFunc("POST /v1/responses/{response_id}/cancel", h.auth(h.cancelResponse, false))
	mux.HandleFunc("GET /v1/responses/{response_id}/input_items", h.auth(h.listResponseInputItems, false))
	mux.HandleFunc("POST /v1/responses/compact", h.auth(h.compactResponse, false))
	mux.HandleFunc("POST /v1/responses/input_tokens", h.auth(h.countResponseInputTokens, false))
	mux.HandleFunc("POST /v1/conversations", h.auth(h.createConversation, false))
	mux.HandleFunc("GET /v1/conversations/{conversation_id}", h.auth(h.getConversation, false))
	mux.HandleFunc("DELETE /v1/conversations/{conversation_id}", h.auth(h.deleteConversation, false))
	mux.HandleFunc("GET /v1/conversations/{conversation_id}/items", h.auth(h.listConversationItems, false))
	mux.HandleFunc("POST /v1/conversations/{conversation_id}/items", h.auth(h.createConversationItems, false))
	mux.HandleFunc("POST /v1/assistants", h.auth(h.createAssistant, false))
	mux.HandleFunc("GET /v1/assistants", h.auth(h.listAssistants, false))
	mux.HandleFunc("GET /v1/assistants/{assistant_id}", h.auth(h.getAssistant, false))
	mux.HandleFunc("POST /v1/assistants/{assistant_id}", h.auth(h.updateAssistant, false))
	mux.HandleFunc("DELETE /v1/assistants/{assistant_id}", h.auth(h.deleteAssistant, false))
	mux.HandleFunc("POST /v1/threads", h.auth(h.createThread, false))
	mux.HandleFunc("GET /v1/threads/{thread_id}", h.auth(h.getThread, false))
	mux.HandleFunc("POST /v1/threads/{thread_id}", h.auth(h.updateThread, false))
	mux.HandleFunc("DELETE /v1/threads/{thread_id}", h.auth(h.deleteThread, false))
	mux.HandleFunc("POST /v1/threads/{thread_id}/messages", h.auth(h.createThreadMessage, false))
	mux.HandleFunc("GET /v1/threads/{thread_id}/messages", h.auth(h.listThreadMessages, false))
	mux.HandleFunc("GET /v1/threads/{thread_id}/messages/{message_id}", h.auth(h.getThreadMessage, false))
	mux.HandleFunc("POST /v1/threads/{thread_id}/runs", h.auth(h.createRun, false))
	mux.HandleFunc("GET /v1/threads/{thread_id}/runs", h.auth(h.listRuns, false))
	mux.HandleFunc("GET /v1/threads/{thread_id}/runs/{run_id}", h.auth(h.getRun, false))
	mux.HandleFunc("POST /v1/threads/{thread_id}/runs/{run_id}/cancel", h.auth(h.cancelRun, false))
	mux.HandleFunc("POST /v1/threads/{thread_id}/runs/{run_id}/submit_tool_outputs", h.auth(h.submitRunToolOutputs, false))
	mux.HandleFunc("GET /v1/models", h.auth(h.listModels, false))
	mux.HandleFunc("HEAD /v1/models", h.auth(h.listModels, false))
	mux.HandleFunc("OPTIONS /v1/models", h.auth(h.listModels, false))
	mux.HandleFunc("GET /v1/models/{model}", h.auth(h.retrieveModel, false))
	mux.HandleFunc("HEAD /v1/models/{model}", h.auth(h.retrieveModel, false))
	mux.HandleFunc("OPTIONS /v1/models/{model}", h.auth(h.retrieveModel, false))
	// Fallback for unknown paths - always JSON, never SPA/redirect
	// Also handles slashed ModelIds like kiro/claude-haiku-4.5 via notFound delegation
	mux.HandleFunc("/", h.notFoundWithModelFallback)
}

// ─────────────────────────────────────────────
// Endpoints
// ─────────────────────────────────────────────

// chatCompletions handles POST /v1/chat/completions
// @Summary      Create chat completion
// @Description  Creates a completion for the chat message. Supports both streaming and non-streaming responses.
// @Description  Derivative-client aliases translate without new paths: random_seed→seed,
// @Description  reasoning{effort,max_tokens}, options{temperature,top_p,seed,stop,num_predict},
// @Description  format→response_format. prompt_cache_key, guardrails, think, keep_alive and
// @Description  top_k are accepted. OpenAI SSE terminates with data: [DONE].
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        request body models.ChatCompletionRequest true "Chat completion request"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ChatCompletionResponse "Successful response"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      403 {object} models.OpenAIError "Forbidden - model not allowed by token rules"
// @Failure      502 {object} models.OpenAIError "Bad Gateway - upstream provider error"
// @Failure      503 {object} models.OpenAIError "Service Unavailable - no credential available"
// @Security     BearerAuth
// @Router       /v1/chat/completions [post]
func (h *Handler) chatCompletions(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	var req models.ChatCompletionRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	req.NormalizeAliases()
	if req.Model == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", strPtr("model"))
		return
	}
	if len(req.Messages) == 0 {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'messages'", strPtr("messages"))
		return
	}

	if deny := authorizeModel(t, req.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.msg, deny.param)
		return
	}

	if req.Stream {
		h.handleStreamWithMetrics(w, r, &req, t, start)
		return
	}

	resp, err := h.router.Complete(r.Context(), &req, t)
	duration := time.Since(start)

	var usage *models.ChatCompletionUsage
	if err == nil && resp != nil {
		usage = &resp.Usage
	}
	h.recordRouteMetric(r.Context(), start, req.Model, t, duration, err, usage)

	if err != nil {
		h.handleRouterError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, resp)
}

// maxAudioUploadBytes caps one transcription upload (OpenAI's limit is 25 MB;
// keep headroom for form overhead).
const maxAudioUploadBytes = 32 << 20

// audioTranscriptions handles POST /v1/audio/transcriptions
// @Summary      Create audio transcription
// @Description  Transcribes an uploaded audio file. Multipart form: file (required),
// @Description  model (required), language, prompt, response_format (json|text|srt|verbose_json|vtt),
// @Description  temperature, timestamp_granularities[] (word|segment). The OpenRouter
// @Description  JSON variant (input_audio{data,format}, base64 or URL) is accepted.
// @Tags         OpenAI API
// @Accept       mpfd
// @Produce      json
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.TranscriptionResponse "Successful response"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Router       /v1/audio/transcriptions [post]
// @Security     BearerAuth
func (h *Handler) audioTranscriptions(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	req, err := parseTranscriptionRequest(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}

	if deny := authorizeModel(t, req.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.msg, deny.param)
		return
	}

	resp, err := h.router.Transcribe(r.Context(), req, t)
	duration := time.Since(start)

	h.recordRouteMetric(r.Context(), start, req.Model, t, duration, err, nil)

	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeTranscriptionResponse(w, req.ResponseFormat, resp)
}

// parseTranscriptionRequest reads the multipart body of
// POST /v1/audio/transcriptions into a TranscriptionRequest. The OpenRouter
// JSON variant (model plus input_audio{data,format}, data base64 or URL) is
// accepted by translation: URLs download under the same size cap.
func parseTranscriptionRequest(r *http.Request) (*models.TranscriptionRequest, error) {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data") {
		if strings.HasPrefix(ct, "application/json") {
			return parseJSONTranscriptionRequest(r)
		}
		return nil, fmt.Errorf("Content-Type must be multipart/form-data")
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxAudioUploadBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return nil, fmt.Errorf("malformed multipart body: %s", err)
	}
	model := strings.TrimSpace(r.FormValue("model"))
	if model == "" {
		return nil, fmt.Errorf("missing required field 'model'")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("missing required field 'file'")
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read file: %s", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("file is empty")
	}
	format := strings.TrimSpace(r.FormValue("response_format"))
	switch format {
	case "", "json", "text", "srt", "verbose_json", "vtt":
	default:
		return nil, fmt.Errorf("invalid response_format %q: expected json, text, srt, verbose_json or vtt", format)
	}
	req := &models.TranscriptionRequest{
		Model:          models.ModelId(model),
		File:           data,
		FileName:       header.Filename,
		ContentType:    header.Header.Get("Content-Type"),
		Language:       strings.TrimSpace(r.FormValue("language")),
		Prompt:         r.FormValue("prompt"),
		ResponseFormat: format,
	}
	if req.ContentType == "" {
		req.ContentType = "application/octet-stream"
	}
	if v := strings.TrimSpace(r.FormValue("temperature")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid temperature %q: must be a number", v)
		}
		req.Temperature = &f
	}
	if r.MultipartForm != nil {
		for _, g := range r.MultipartForm.Value["timestamp_granularities[]"] {
			g = strings.TrimSpace(g)
			if g == "word" || g == "segment" {
				req.TimestampGranularities = append(req.TimestampGranularities, g)
			} else if g != "" {
				return nil, fmt.Errorf("invalid timestamp granularity %q: expected word or segment", g)
			}
		}
	}
	return req, nil
}

// writeTranscriptionResponse renders the normalized transcription in the
// client's response_format. srt/vtt need segments; a model that returned
// none is a loud error, not an empty document.
func (h *Handler) writeTranscriptionResponse(w http.ResponseWriter, format string, resp *models.TranscriptionResponse) {
	switch format {
	case "", "json":
		h.writeJSON(w, http.StatusOK, struct {
			Text string `json:"text"`
		}{Text: resp.Text})
	case "verbose_json":
		h.writeJSON(w, http.StatusOK, resp)
	case "text":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, resp.Text)
	case "srt", "vtt":
		if len(resp.Segments) == 0 {
			h.writeError(w, http.StatusBadGateway, "upstream_error",
				fmt.Sprintf("response_format %q requires segment timestamps the model did not return", format), nil)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if format == "srt" {
			_, _ = io.WriteString(w, resp.SRT())
		} else {
			_, _ = io.WriteString(w, resp.VTT())
		}
	}
}

// audioSpeech handles POST /v1/audio/speech
// @Summary      Create speech
// @Description  Generates audio from the input text. JSON body: model (required),
// @Description  input (required), voice, response_format (mp3|opus|aac|flac|wav|pcm),
// @Description  speed, instructions. The response body is raw audio bytes.
// @Tags         OpenAI API
// @Accept       json
// @Produce      audio/mpeg
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 "Audio stream"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Router       /v1/audio/speech [post]
// @Security     BearerAuth
func (h *Handler) audioSpeech(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	var req models.SpeechRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if req.Model == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", strPtr("model"))
		return
	}
	if strings.TrimSpace(req.Input) == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'input'", strPtr("input"))
		return
	}
	switch req.ResponseFormat {
	case "", "mp3", "opus", "aac", "flac", "wav", "pcm":
	default:
		h.writeError(w, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("invalid response_format %q: expected mp3, opus, aac, flac, wav or pcm", req.ResponseFormat), strPtr("response_format"))
		return
	}
	if req.Speed != nil && (*req.Speed < 0.25 || *req.Speed > 4.0) {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "speed must be between 0.25 and 4.0", strPtr("speed"))
		return
	}

	if deny := authorizeModel(t, req.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.msg, deny.param)
		return
	}

	resp, err := h.router.Speech(r.Context(), &req, t)
	duration := time.Since(start)

	h.recordRouteMetric(r.Context(), start, req.Model, t, duration, err, nil)

	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	format := resp.Format
	if format == "" {
		format = req.ResponseFormat
	}
	w.Header().Set("Content-Type", models.SpeechContentType(format))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp.Audio)
}

// imageGenerations handles POST /v1/images/generations
// @Summary      Create image
// @Description  Generates images from a prompt. JSON body: prompt (required), model,
// @Description  n, size, quality, style, response_format (url|b64_json).
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ImageGenerationResponse "Successful response"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Router       /v1/images/generations [post]
// @Security     BearerAuth
func (h *Handler) imageGenerations(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	var req models.ImageGenerationRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'prompt'", strPtr("prompt"))
		return
	}
	if req.N < 0 || req.N > 10 {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "n must be between 1 and 10", strPtr("n"))
		return
	}
	switch req.ResponseFormat {
	case "", "url", "b64_json":
	default:
		h.writeError(w, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("invalid response_format %q: expected url or b64_json", req.ResponseFormat), strPtr("response_format"))
		return
	}

	if deny := authorizeModel(t, req.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.msg, deny.param)
		return
	}

	resp, err := h.router.GenerateImage(r.Context(), &req, t)
	duration := time.Since(start)

	h.recordRouteMetric(r.Context(), start, req.Model, t, duration, err, nil)

	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	if resp.Created == 0 {
		resp.Created = time.Now().Unix()
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// embeddings handles POST /v1/embeddings
// @Summary      Create embeddings
// @Description  Generates embedding vectors for the input. JSON body: model (required),
// @Description  input (string or array of strings, required), encoding_format (float|base64),
// @Description  dimensions (output_dimension alias), output_dtype and truncate accepted.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.EmbeddingsResponse "Successful response"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Router       /v1/embeddings [post]
// @Security     BearerAuth
func (h *Handler) embeddings(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	var req models.EmbeddingsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if req.Model == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", strPtr("model"))
		return
	}
	if len(req.Input) == 0 {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'input'", strPtr("input"))
		return
	}
	for _, s := range req.Input {
		if strings.TrimSpace(s) == "" {
			h.writeError(w, http.StatusBadRequest, "invalid_request_error", "input must not contain empty strings", strPtr("input"))
			return
		}
	}
	switch req.EncodingFormat {
	case "", "float", "base64":
	default:
		h.writeError(w, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("invalid encoding_format %q: expected float or base64", req.EncodingFormat), strPtr("encoding_format"))
		return
	}

	if deny := authorizeModel(t, req.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.msg, deny.param)
		return
	}

	resp, err := h.router.Embed(r.Context(), &req, t)
	duration := time.Since(start)

	h.recordRouteMetric(r.Context(), start, req.Model, t, duration, err, nil)

	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	if req.EncodingFormat == "base64" {
		for i := range resp.Data {
			resp.Data[i].B64Values = models.EmbeddingBase64(resp.Data[i].Values)
			resp.Data[i].Values = nil
		}
	}
	if resp.Model == "" {
		resp.Model = req.Model.String()
	}
	h.writeJSON(w, http.StatusOK, struct {
		Object string `json:"object"`
		*models.EmbeddingsResponse
	}{Object: "list", EmbeddingsResponse: resp})
}

// anthropicMessages handles POST /v1/messages
// @Summary      Create message (Anthropic Messages API)
// @Description  Anthropic-compatible endpoint. The request is converted to the OpenAI
// @Description  chat shape, routed normally, and the answer converted back. Supports
// @Description  streaming (SSE), tools, system prompts, images and tool results.
// @Description  Document blocks are refused; cache_control, top_k and thinking budgets
// @Description  are accepted. Errors serialize as {type:error,error:{type,message}} when
// @Description  anthropic-version is present, OpenAIError otherwise. SSE uses
// @Description  event: message_start|content_block_delta|message_stop.
// @Tags         Anthropic API
// @Accept       json
// @Produce      json
// @Param        request body models.AnthropicMessageRequest true "Message request"
// @Param        anthropic-version header string false "Anthropic API version (accepted, never pinned)"
// @Param        anthropic-beta header string false "Anthropic beta flags (passthrough)"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.AnthropicMessage "Successful response"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      403 {object} models.OpenAIError "Forbidden - model not allowed by token rules"
// @Failure      502 {object} models.OpenAIError "Bad Gateway - upstream provider error"
// @Failure      503 {object} models.OpenAIError "Service Unavailable - no credential available"
// @Router       /v1/messages [post]
// @Security     BearerAuth
func (h *Handler) anthropicMessages(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	var req anthropicRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeCompatDecodeError(w, r, err)
		return
	}
	if req.Model == "" {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", strPtr("model"))
		return
	}
	if req.MaxTokens <= 0 {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'max_tokens'", strPtr("max_tokens"))
		return
	}
	if len(req.Messages) == 0 {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'messages'", strPtr("messages"))
		return
	}

	chatReq, err := anthropicToOpenAI(&req)
	if err != nil {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}

	if deny := authorizeModel(t, chatReq.Model); deny != nil {
		h.writeCompatError(w, r, deny.status, deny.code, deny.msg, deny.param)
		return
	}

	if req.Stream {
		h.anthropicStream(w, r, chatReq, t, start)
		return
	}

	resp, err := h.router.Complete(r.Context(), chatReq, t)
	duration := time.Since(start)

	var usage *models.ChatCompletionUsage
	if err == nil && resp != nil {
		usage = &resp.Usage
	}
	h.recordRouteMetric(r.Context(), start, chatReq.Model, t, duration, err, usage)

	if err != nil {
		h.handleCompatRouterError(w, r, err)
		return
	}
	echoAnthropicVersion(w, r)
	h.writeJSON(w, http.StatusOK, anthropicFromOpenAI(resp, req.Model))
}

// anthropicStream serves POST /v1/messages with stream=true: the OpenAI
// chunk stream is translated into Anthropic events.
func (h *Handler) anthropicStream(w http.ResponseWriter, r *http.Request, chatReq *models.ChatCompletionRequest, t *models.RouterToken, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "server_error", "streaming is not supported by this server", nil)
		return
	}

	httpkit.WriteSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	tw := newAnthropicStreamWriter(w, flusher, chatReq.Model.String())
	err := h.router.CompleteStream(r.Context(), chatReq, tw, t)
	duration := time.Since(start)

	providerType, _, _ := chatReq.Model.Parse()
	providerID, _ := h.router.GetProviderIDForModel(r.Context(), chatReq.Model)
	tokenID := ""
	if t != nil {
		tokenID = t.ID
	}
	event := models.MetricEvent{
		Timestamp:    start,
		ProviderID:   providerID,
		ProviderType: providerType,
		Model:        chatReq.Model,
		TokenID:      tokenID,
		Duration:     duration,
		StatusCode:   http.StatusOK,
	}

	if err != nil {
		event.StatusCode = http.StatusInternalServerError
		re := h.classifyError(err)
		event.ErrorType = re.code
		h.logger.Error("anthropic stream error", "err", err)
		_ = tw.emit("error", map[string]any{
			"type":  "error",
			"error": map[string]any{"type": "api_error", "message": err.Error()},
		})
	}
	h.metrics.RecordRequest(event)
}

// handleStreamWithMetrics wraps streaming with metrics collection.
func (h *Handler) handleStreamWithMetrics(w http.ResponseWriter, r *http.Request, req *models.ChatCompletionRequest, t *models.RouterToken, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "server_error", "streaming is not supported by this server", nil)
		return
	}

	httpkit.WriteSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	err := h.router.CompleteStream(r.Context(), req, w, t)
	duration := time.Since(start)

	// Extract provider info
	providerType, _, _ := req.Model.Parse()
	providerID, _ := h.router.GetProviderIDForModel(r.Context(), req.Model)

	// Build metric event
	tokenID := ""
	if t != nil {
		tokenID = t.ID
	}
	event := models.MetricEvent{
		Timestamp:    start,
		ProviderID:   providerID,
		ProviderType: providerType,
		Model:        req.Model,
		TokenID:      tokenID,
		Duration:     duration,
		StatusCode:   http.StatusOK,
	}

	if err != nil {
		event.StatusCode = http.StatusInternalServerError
		re := h.classifyError(err)
		event.ErrorType = re.code
		h.logger.Error("stream error", "err", err)

		// Send error in OpenAI-compatible format as SSE event
		errorObj := models.OpenAIError{
			Error: models.OpenAIErrorBody{
				Message: err.Error(),
				Type:    errorTypeForCode(re.code),
				Code:    re.code,
			},
		}
		errorJSON, _ := json.Marshal(errorObj)
		fmt.Fprintf(w, "data: %s\n\n", errorJSON)
		flusher.Flush()
	}

	// Record metrics
	h.metrics.RecordRequest(event)
}

// listModels handles GET /v1/models - token-filtered discovery listing.
// Entries the token rules deny are omitted silently. With anthropic-version
// present the same entries serialize in the Anthropic models shape.
// @Summary      List models
// @Description  Lists routable models as provider/model ids. TokenRules filter the
// @Description  listing: denied models are omitted, never error. Dual-serves the
// @Description  Anthropic models shape when anthropic-version is present.
// @Tags         OpenAI API
// @Tags         Anthropic API
// @Produce      json
// @Param        anthropic-version header string false "Anthropic API version (accepted, never pinned)"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ModelListResponse "Model list (Anthropic shape when anthropic-version is present)"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Router       /v1/models [get]
// @Security     BearerAuth
func (h *Handler) listModels(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	entryFor := func(p *models.ProviderInstance, mi modelinfo.ModelView) models.ModelEntry {
		e := models.ModelEntry{
			ID:                  p.ID + "/" + mi.Name,
			Object:              "model",
			Created:             p.CreatedAt.Unix(),
			OwnedBy:             p.TypeKey,
			Name:                mi.DisplayName,
			Description:         mi.Description,
			ContextLength:       mi.ContextWindow,
			MaxCompletionTokens: mi.MaxTokens,
			Reasoning:           mi.Reasoning,
			SupportedParameters: mi.SupportedParameters,
		}
		if len(mi.InputModalities) > 0 || len(mi.OutputModalities) > 0 {
			arch := &models.ModelArchitecture{
				InputModalities:  mi.InputModalities,
				OutputModalities: mi.OutputModalities,
			}
			arch.Modality = models.JoinModalities(mi.InputModalities) + "->" + models.JoinModalities(mi.OutputModalities)
			e.Architecture = arch
		}
		return e
	}

	var entries []models.ModelEntry
	// byFullID indexes the already-fetched member views for virtual-model
	// resolution below: no second lookup pass.
	byFullID := map[string]modelinfo.ModelView{}
	// Global listing like dashboard Available Models, not per-token.
	// Never hide a provider on discovery error — log and optionally emit synthetic entry.
	if h.providerSvc != nil && h.modelInfoSvc != nil {
		if providers, err := h.providerSvc.List(); err == nil {
			for _, p := range providers {
				if p.TypeKey == provider.TypeVirtual || p.Disabled {
					continue
				}
				infos, err := h.modelInfoSvc.MergedView(r.Context(), p.ID)
				if err != nil {
					h.logger.Warn("v1 listModels: model discovery failed", "provider_id", p.ID, "err", err)
					if p.TypeKey == "custom" {
						// Compat servers may not implement GET /models — still advertise provider as routable.
						entries = append(entries, models.ModelEntry{
							ID:      p.ID + "/*",
							Object:  "model",
							Created: p.CreatedAt.Unix(),
							OwnedBy: p.TypeKey,
						})
					}
					continue
				}
				if len(infos) == 0 {
					if p.TypeKey == "custom" {
						entries = append(entries, models.ModelEntry{
							ID:      p.ID + "/*",
							Object:  "model",
							Created: p.CreatedAt.Unix(),
							OwnedBy: p.TypeKey,
						})
					}
					continue
				}
				for _, mi := range infos {
					if mi.Disabled {
						continue
					}
					byFullID[p.ID+"/"+mi.Name] = mi
					entries = append(entries, entryFor(p, mi))
				}
			}
		}
	}
	if h.virtualSvc != nil {
		if agents, err := h.virtualSvc.List(); err == nil {
			// Virtual metadata is folded serve-time from the member views
			// above: unknown members are skipped, a virtual model with no
			// available members stays hidden. Pure in-memory work, fanned out.
			type resolvedVirtual struct {
				agent *models.VirtualModel
				agg   virtual.Metadata
			}
			var mu sync.Mutex
			var resolved []resolvedVirtual
			var wg sync.WaitGroup
			for _, a := range agents {
				if a.Disabled {
					continue
				}
				wg.Add(1)
				go func(a *models.VirtualModel) {
					defer wg.Done()
					members := make([]modelinfo.ModelView, 0, len(a.Models))
					for _, e := range a.Models {
						mv, ok := byFullID[string(e.ModelID)]
						if !ok {
							h.logger.Warn("v1 listModels: virtual member not found, skipping",
								"virtual_id", a.ID, "member", string(e.ModelID))
							continue
						}
						members = append(members, mv)
					}
					if len(members) == 0 {
						h.logger.Warn("v1 listModels: virtual model has no available members, hiding",
							"virtual_id", a.ID)
						return
					}
					mu.Lock()
					infos := make([]models.ModelInfo, 0, len(members))
					for _, mv := range members {
						infos = append(infos, mv.ModelInfo)
					}
					resolved = append(resolved, resolvedVirtual{agent: a, agg: virtual.FoldMembers(infos)})
					mu.Unlock()
				}(a)
			}
			wg.Wait()
			for _, rv := range resolved {
				a, agg := rv.agent, rv.agg
				e := models.ModelEntry{
					ID:                  provider.TypeVirtual + "/" + a.ID,
					Object:              "model",
					Created:             a.CreatedAt.Unix(),
					OwnedBy:             provider.TypeVirtual,
					Name:                a.Name,
					Description:         a.Description,
					ContextLength:       agg.ContextLength,
					MaxCompletionTokens: agg.MaxCompletionTokens,
					Reasoning:           agg.Reasoning,
					SupportedParameters: agg.SupportedParameters,
					Capabilities:        agg.Capabilities,
				}
				if len(agg.InputModalities) > 0 || len(agg.OutputModalities) > 0 {
					e.Architecture = &models.ModelArchitecture{
						InputModalities:  agg.InputModalities,
						OutputModalities: agg.OutputModalities,
						Modality:         models.JoinModalities(agg.InputModalities) + "->" + models.JoinModalities(agg.OutputModalities),
					}
				}
				entries = append(entries, e)
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	if entries == nil {
		entries = []models.ModelEntry{}
	}
	if t != nil {
		entries = filterModelsByToken(t, entries)
	}

	if isAnthropicStyle(r) {
		echoAnthropicVersion(w, r)
		h.writeJSON(w, http.StatusOK, anthropicModelList(entries))
		return
	}
	h.writeJSON(w, http.StatusOK, models.ModelListResponse{Object: "list", Data: entries})
}

// filterModelsByToken omits entries the token rules deny. Synthetic custom
// "provider/*" cards survive only under AllowAllModels: a literal wildcard
// never equals a listed model.
func filterModelsByToken(t *models.RouterToken, entries []models.ModelEntry) []models.ModelEntry {
	kept := entries[:0]
	for _, e := range entries {
		if t.Rules.Allows(models.ModelId(e.ID)) {
			kept = append(kept, e)
		}
	}
	if kept == nil {
		kept = []models.ModelEntry{}
	}
	return kept
}

// anthropicModelList renders model entries in the Anthropic models shape.
func anthropicModelList(entries []models.ModelEntry) models.AnthropicModelListResponse {
	out := models.AnthropicModelListResponse{Data: make([]models.AnthropicModelEntry, 0, len(entries))}
	for _, e := range entries {
		entry := models.AnthropicModelEntry{Type: "model", ID: e.ID, DisplayName: e.Name}
		if e.Created > 0 {
			entry.CreatedAt = time.Unix(e.Created, 0).UTC().Format(time.RFC3339)
		}
		out.Data = append(out.Data, entry)
	}
	if n := len(out.Data); n > 0 {
		first, last := out.Data[0].ID, out.Data[n-1].ID
		out.FirstID, out.LastID = &first, &last
	}
	return out
}

// retrieveModel handles GET /v1/models/{model} - token-filtered lookup.
// Unknown and token-denied ids both return 404 with the same message, so
// denied models are indistinguishable from missing ones. With
// anthropic-version present the card serializes in the Anthropic shape.
// @Summary      Retrieve model
// @Description  Retrieves one model card by provider/model id. Unknown or token-denied
// @Description  ids return 404. Dual-serves the Anthropic model shape when
// @Description  anthropic-version is present.
// @Tags         OpenAI API
// @Tags         Anthropic API
// @Produce      json
// @Param        model path string true "Model id (provider/model)"
// @Param        anthropic-version header string false "Anthropic API version (accepted, never pinned)"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ModelEntry "Model card (Anthropic shape when anthropic-version is present)"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      404 {object} models.OpenAIError "Unknown or denied model"
// @Router       /v1/models/{model} [get]
// @Security     BearerAuth
func (h *Handler) retrieveModel(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	modelID := strings.TrimPrefix(r.URL.Path, "/v1/models/")
	if modelID == "" || modelID == r.URL.Path {
		if v := r.PathValue("model"); v != "" {
			modelID = v
		}
	}
	modelID = strings.TrimSuffix(modelID, "/")
	if modelID == "" {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "model id is required", strPtr("model"))
		return
	}
	mid := models.ModelId(modelID)
	providerID, modelName, err := mid.Parse()
	if err != nil || providerID == "" || modelName == "" {
		h.writeCompatError(w, r, http.StatusNotFound, "not_found", fmt.Sprintf("The model '%s' does not exist", modelID), nil)
		return
	}
	if t != nil && !t.Rules.Allows(mid) {
		h.writeCompatError(w, r, http.StatusNotFound, "not_found", fmt.Sprintf("The model '%s' does not exist", modelID), nil)
		return
	}
	// Check global existence via modelInfo or virtual models
	exists := false
	var ownedBy string
	var created int64
	var info *modelinfo.ModelView
	var virtualAgent *models.VirtualModel
	var virtualAgg virtual.Metadata
	if providerID == provider.TypeVirtual && h.virtualSvc != nil {
		if a, err := h.virtualSvc.Get(modelName); err == nil && !a.Disabled {
			// Members resolve cache-only: a single lookup must never ping
			// upstreams. Unknown members are skipped; no available members
			// means the virtual model does not exist for this endpoint.
			if h.modelInfoSvc != nil {
				need := map[string][]string{}
				for _, e := range a.Models {
					pid, name, perr := e.ModelID.Parse()
					if perr != nil {
						h.logger.Warn("v1 retrieveModel: virtual member id invalid, skipping",
							"virtual_id", a.ID, "member", string(e.ModelID))
						continue
					}
					need[pid] = append(need[pid], name)
				}
				var members []modelinfo.ModelView
				for pid, names := range need {
					views, verr := h.modelInfoSvc.PeekMergedView(pid)
					if verr != nil {
						h.logger.Warn("v1 retrieveModel: provider view unavailable, skipping members",
							"virtual_id", a.ID, "provider_id", pid)
						continue
					}
					byName := make(map[string]modelinfo.ModelView, len(views))
					for _, v := range views {
						if !v.Disabled {
							byName[v.Name] = v
						}
					}
					for _, name := range names {
						mv, ok := byName[name]
						if !ok {
							h.logger.Warn("v1 retrieveModel: virtual member not found, skipping",
								"virtual_id", a.ID, "member", pid+"/"+name)
							continue
						}
						members = append(members, mv)
					}
				}
				if len(members) > 0 {
					exists = true
					ownedBy = provider.TypeVirtual
					created = a.CreatedAt.Unix()
					virtualAgent = a
					infos := make([]models.ModelInfo, 0, len(members))
					for _, mv := range members {
						infos = append(infos, mv.ModelInfo)
					}
					virtualAgg = virtual.FoldMembers(infos)
				} else {
					h.logger.Warn("v1 retrieveModel: virtual model has no available members, hiding",
						"virtual_id", a.ID)
				}
			}
		}
	} else if h.providerSvc != nil && h.modelInfoSvc != nil {
		if p, err := h.providerSvc.Get(providerID); err == nil {
			ownedBy = p.TypeKey
			created = p.CreatedAt.Unix()
			if views, err := h.modelInfoSvc.MergedView(r.Context(), providerID); err == nil {
				for i := range views {
					if views[i].Name == modelName && !views[i].Disabled {
						exists = true
						info = &views[i]
						break
					}
				}
			}
		}
	}
	if !exists {
		h.writeCompatError(w, r, http.StatusNotFound, "not_found", fmt.Sprintf("The model '%s' does not exist", modelID), nil)
		return
	}
	if isAnthropicStyle(r) {
		echoAnthropicVersion(w, r)
		name := ""
		if virtualAgent != nil {
			name = virtualAgent.Name
		}
		if info != nil && info.DisplayName != "" {
			name = info.DisplayName
		}
		entry := models.AnthropicModelEntry{Type: "model", ID: modelID, DisplayName: name}
		if created > 0 {
			entry.CreatedAt = time.Unix(created, 0).UTC().Format(time.RFC3339)
		}
		h.writeJSON(w, http.StatusOK, entry)
		return
	}
	out := map[string]any{
		"id":       modelID,
		"object":   "model",
		"created":  created,
		"owned_by": ownedBy,
	}
	if virtualAgent != nil {
		out["name"] = virtualAgent.Name
		if virtualAgent.Description != "" {
			out["description"] = virtualAgent.Description
		}
		if virtualAgg.ContextLength > 0 {
			out["context_length"] = virtualAgg.ContextLength
		}
		if virtualAgg.MaxCompletionTokens > 0 {
			out["max_completion_tokens"] = virtualAgg.MaxCompletionTokens
		}
		if len(virtualAgg.Capabilities) > 0 {
			out["capabilities"] = virtualAgg.Capabilities
		}
		if len(virtualAgg.InputModalities) > 0 || len(virtualAgg.OutputModalities) > 0 {
			out["architecture"] = map[string]any{
				"input_modalities":  virtualAgg.InputModalities,
				"output_modalities": virtualAgg.OutputModalities,
				"modality":          models.JoinModalities(virtualAgg.InputModalities) + "->" + models.JoinModalities(virtualAgg.OutputModalities),
			}
		}
		if virtualAgg.Reasoning != nil {
			out["reasoning"] = virtualAgg.Reasoning
		}
		if len(virtualAgg.SupportedParameters) > 0 {
			out["supported_parameters"] = virtualAgg.SupportedParameters
		}
	}
	if info != nil {
		if info.DisplayName != "" {
			out["name"] = info.DisplayName
		}
		if info.Description != "" {
			out["description"] = info.Description
		}
		if info.ContextWindow > 0 {
			out["context_length"] = info.ContextWindow
		}
		if info.MaxTokens > 0 {
			out["max_completion_tokens"] = info.MaxTokens
		}
		if len(info.InputModalities) > 0 || len(info.OutputModalities) > 0 {
			out["architecture"] = map[string]any{
				"input_modalities":  info.InputModalities,
				"output_modalities": info.OutputModalities,
				"modality":          models.JoinModalities(info.InputModalities) + "->" + models.JoinModalities(info.OutputModalities),
			}
		}
		if info.Reasoning != nil {
			out["reasoning"] = info.Reasoning
		}
		if len(info.SupportedParameters) > 0 {
			out["supported_parameters"] = info.SupportedParameters
		}
	}
	h.writeJSON(w, http.StatusOK, out)
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.writeError(w, http.StatusNotFound, "invalid_request_error", fmt.Sprintf("Invalid URL (%s %s)", r.Method, r.URL.Path), nil)
}

func (h *Handler) notFoundWithModelFallback(w http.ResponseWriter, r *http.Request) {
	// Handle slashed ModelIds like kiro/claude-haiku-4.5 which don't match {model} pattern
	if strings.HasPrefix(r.URL.Path, "/v1/models/") && r.URL.Path != "/v1/models/" {
		h.auth(h.retrieveModel, false)(w, r)
		return
	}
	h.notFound(w, r)
}

// ─────────────────────────────────────────────
// Middleware
// ─────────────────────────────────────────────

type authedHandler func(w http.ResponseWriter, r *http.Request, t *models.RouterToken)

// auth extracts and validates the router token. Authorization: Bearer
// llmr_* and the x-api-key: llmr_* alias (Anthropic SDK style) both work on
// every /v1 route. allowAnonymous stays for the NoAuth-adjacent probes only;
// production routes pass false.
func (h *Handler) auth(next authedHandler, allowAnonymous bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.noAuth {
			next(w, r, nil)
			return
		}
		raw := extractToken(r)

		// Whitelisted safe paths allow missing token
		if raw == "" {
			if allowAnonymous {
				// Anonymous access - pass nil token (handlers must handle nil)
				next(w, r, nil)
				return
			}
			h.writeError(w, http.StatusUnauthorized, "invalid_request_error", "You didn't provide an API key. Provide it in an Authorization: Bearer header or an x-api-key header.", strPtr("Authorization"))
			return
		}

		t, err := h.tokens.Validate(raw)
		if err != nil {
			if errors.Is(err, apierrors.ErrUnauthorized) {
				h.writeError(w, http.StatusUnauthorized, "invalid_request_error", "Incorrect API key provided. You can find your API key at https://platform.openai.com/account/api-keys.", nil)
				return
			}
			h.writeError(w, http.StatusInternalServerError, "server_error", "token validation failed", nil)
			return
		}

		next(w, r, t)
	}
}

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

// extractToken reads the router token from Authorization: Bearer or the
// x-api-key alias. Bearer wins when both are present.
func extractToken(r *http.Request) string {
	if v := r.Header.Get("Authorization"); v != "" {
		if after, ok := strings.CutPrefix(v, "Bearer "); ok {
			if key := strings.TrimSpace(after); key != "" {
				return key
			}
		}
	}
	if v := strings.TrimSpace(r.Header.Get("x-api-key")); v != "" {
		if after, ok := strings.CutPrefix(v, "Bearer "); ok {
			return strings.TrimSpace(after)
		}
		return v
	}
	return ""
}

// isAnthropicStyle reports whether the request wants Anthropic shapes: the
// anthropic-version header opts a /v1 route into Anthropic serialization
// (success and error envelopes, SSE event names). The header value itself
// is accepted and echoed, never pinned.
func isAnthropicStyle(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("anthropic-version")) != ""
}

func (h *Handler) handleRouterError(w http.ResponseWriter, err error) {
	re := h.classifyError(err)
	if secs, ok := apierrors.RetryAfterDelay(err); ok {
		w.Header().Set("Retry-After", strconv.FormatInt(secs, 10))
	}
	h.writeError(w, re.status, re.code, err.Error(), nil)
}

type routerError struct {
	status int
	code   string
}

func (h *Handler) classifyError(err error) routerError {
	re := apierrors.ToAPIError(err)
	return routerError{re.Status, re.Code}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, msg string, param *string) {
	// Map internal code to OpenAI error type
	errType := errorTypeForCode(code)
	h.writeJSON(w, status, models.OpenAIError{
		Error: models.OpenAIErrorBody{
			Message: msg,
			Type:    errType,
			Param:   param,
			Code:    code,
		},
	})
}

func strPtr(s string) *string { return &s }

func errorTypeForCode(code string) string {
	return apierrors.ErrorTypeForCode(code)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.logger.Error("json encode error", "err", err)
	}
}
