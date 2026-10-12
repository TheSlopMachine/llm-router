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
	tokens       *token.GormService
	router       *router.Service
	metrics      *metrics.Service
	providerSvc  *provider.GormService
	modelInfoSvc *modelinfo.GormService
	virtualSvc   *virtual.GormService
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
func (h *Handler) chatCompletions(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	var request models.ChatCompletionRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	request.NormalizeAliases()
	if request.Model == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", stringPointer("model"))
		return
	}
	if len(request.Messages) == 0 {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'messages'", stringPointer("messages"))
		return
	}

	if deny := authorizeModel(token, request.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.message, deny.parameter)
		return
	}

	if request.Stream {
		h.handleStreamWithMetrics(w, r, &request, token, start)
		return
	}

	response, err := h.router.Complete(r.Context(), &request, token)
	duration := time.Since(start)

	var usage *models.ChatCompletionUsage
	if err == nil && response != nil {
		usage = &response.Usage
	}
	h.recordRouteMetric(r.Context(), start, request.Model, token, duration, err, usage)

	if err != nil {
		h.handleRouterError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, response)
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
func (h *Handler) audioTranscriptions(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	request, err := parseTranscriptionRequest(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}

	if deny := authorizeModel(token, request.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.message, deny.parameter)
		return
	}

	response, err := h.router.Transcribe(r.Context(), request, token)
	duration := time.Since(start)

	h.recordRouteMetric(r.Context(), start, request.Model, token, duration, err, nil)

	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	h.writeTranscriptionResponse(w, request.ResponseFormat, response)
}

// parseTranscriptionRequest reads the multipart body of
// POST /v1/audio/transcriptions into a TranscriptionRequest. The OpenRouter
// JSON variant (model plus input_audio{data,format}, data base64 or URL) is
// accepted by translation: URLs download under the same size cap.
func parseTranscriptionRequest(r *http.Request) (*models.TranscriptionRequest, error) {
	if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "multipart/form-data") {
		if strings.HasPrefix(contentType, "application/json") {
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
	request := &models.TranscriptionRequest{
		Model:          models.ModelId(model),
		File:           data,
		FileName:       header.Filename,
		ContentType:    header.Header.Get("Content-Type"),
		Language:       strings.TrimSpace(r.FormValue("language")),
		Prompt:         r.FormValue("prompt"),
		ResponseFormat: format,
	}
	if request.ContentType == "" {
		request.ContentType = "application/octet-stream"
	}
	if value := strings.TrimSpace(r.FormValue("temperature")); value != "" {
		temperature, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid temperature %q: must be a number", value)
		}
		request.Temperature = &temperature
	}
	if r.MultipartForm != nil {
		for _, granularity := range r.MultipartForm.Value["timestamp_granularities[]"] {
			granularity = strings.TrimSpace(granularity)
			if granularity == "word" || granularity == "segment" {
				request.TimestampGranularities = append(request.TimestampGranularities, granularity)
			} else if granularity != "" {
				return nil, fmt.Errorf("invalid timestamp granularity %q: expected word or segment", granularity)
			}
		}
	}
	return request, nil
}

// writeTranscriptionResponse renders the normalized transcription in the
// client's response_format. srt/vtt need segments; a model that returned
// none is a loud error, not an empty document.
func (h *Handler) writeTranscriptionResponse(w http.ResponseWriter, format string, response *models.TranscriptionResponse) {
	switch format {
	case "", "json":
		h.writeJSON(w, http.StatusOK, struct {
			Text string `json:"text"`
		}{Text: response.Text})
	case "verbose_json":
		h.writeJSON(w, http.StatusOK, response)
	case "text":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, response.Text)
	case "srt", "vtt":
		if len(response.Segments) == 0 {
			h.writeError(w, http.StatusBadGateway, "upstream_error",
				fmt.Sprintf("response_format %q requires segment timestamps the model did not return", format), nil)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if format == "srt" {
			_, _ = io.WriteString(w, response.SRT())
		} else {
			_, _ = io.WriteString(w, response.VTT())
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
func (h *Handler) audioSpeech(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	var request models.SpeechRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if request.Model == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", stringPointer("model"))
		return
	}
	if strings.TrimSpace(request.Input) == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'input'", stringPointer("input"))
		return
	}
	switch request.ResponseFormat {
	case "", "mp3", "opus", "aac", "flac", "wav", "pcm":
	default:
		h.writeError(w, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("invalid response_format %q: expected mp3, opus, aac, flac, wav or pcm", request.ResponseFormat), stringPointer("response_format"))
		return
	}
	if request.Speed != nil && (*request.Speed < 0.25 || *request.Speed > 4.0) {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "speed must be between 0.25 and 4.0", stringPointer("speed"))
		return
	}

	if deny := authorizeModel(token, request.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.message, deny.parameter)
		return
	}

	response, err := h.router.Speech(r.Context(), &request, token)
	duration := time.Since(start)

	h.recordRouteMetric(r.Context(), start, request.Model, token, duration, err, nil)

	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	format := response.Format
	if format == "" {
		format = request.ResponseFormat
	}
	w.Header().Set("Content-Type", models.SpeechContentType(format))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(response.Audio)
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
func (h *Handler) imageGenerations(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	var request models.ImageGenerationRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if strings.TrimSpace(request.Prompt) == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'prompt'", stringPointer("prompt"))
		return
	}
	if request.N < 0 || request.N > 10 {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "n must be between 1 and 10", stringPointer("n"))
		return
	}
	switch request.ResponseFormat {
	case "", "url", "b64_json":
	default:
		h.writeError(w, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("invalid response_format %q: expected url or b64_json", request.ResponseFormat), stringPointer("response_format"))
		return
	}

	if deny := authorizeModel(token, request.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.message, deny.parameter)
		return
	}

	response, err := h.router.GenerateImage(r.Context(), &request, token)
	duration := time.Since(start)

	h.recordRouteMetric(r.Context(), start, request.Model, token, duration, err, nil)

	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	if response.Created == 0 {
		response.Created = time.Now().Unix()
	}
	h.writeJSON(w, http.StatusOK, response)
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
func (h *Handler) embeddings(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	var request models.EmbeddingsRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if request.Model == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", stringPointer("model"))
		return
	}
	if len(request.Input) == 0 {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'input'", stringPointer("input"))
		return
	}
	for _, text := range request.Input {
		if strings.TrimSpace(text) == "" {
			h.writeError(w, http.StatusBadRequest, "invalid_request_error", "input must not contain empty strings", stringPointer("input"))
			return
		}
	}
	switch request.EncodingFormat {
	case "", "float", "base64":
	default:
		h.writeError(w, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("invalid encoding_format %q: expected float or base64", request.EncodingFormat), stringPointer("encoding_format"))
		return
	}

	if deny := authorizeModel(token, request.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.message, deny.parameter)
		return
	}

	response, err := h.router.Embed(r.Context(), &request, token)
	duration := time.Since(start)

	h.recordRouteMetric(r.Context(), start, request.Model, token, duration, err, nil)

	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	if request.EncodingFormat == "base64" {
		for i := range response.Data {
			response.Data[i].B64Values = models.EmbeddingBase64(response.Data[i].Values)
			response.Data[i].Values = nil
		}
	}
	if response.Model == "" {
		response.Model = request.Model.String()
	}
	h.writeJSON(w, http.StatusOK, struct {
		Object string `json:"object"`
		*models.EmbeddingsResponse
	}{Object: "list", EmbeddingsResponse: response})
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
func (h *Handler) anthropicMessages(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	var request anthropicRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		h.writeCompatDecodeError(w, r, err)
		return
	}
	if request.Model == "" {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", stringPointer("model"))
		return
	}
	if request.MaxTokens <= 0 {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'max_tokens'", stringPointer("max_tokens"))
		return
	}
	if len(request.Messages) == 0 {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "missing required field 'messages'", stringPointer("messages"))
		return
	}

	chatRequest, err := anthropicToOpenAI(&request)
	if err != nil {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}

	if deny := authorizeModel(token, chatRequest.Model); deny != nil {
		h.writeCompatError(w, r, deny.status, deny.code, deny.message, deny.parameter)
		return
	}

	if request.Stream {
		h.anthropicStream(w, r, chatRequest, token, start)
		return
	}

	response, err := h.router.Complete(r.Context(), chatRequest, token)
	duration := time.Since(start)

	var usage *models.ChatCompletionUsage
	if err == nil && response != nil {
		usage = &response.Usage
	}
	h.recordRouteMetric(r.Context(), start, chatRequest.Model, token, duration, err, usage)

	if err != nil {
		h.handleCompatRouterError(w, r, err)
		return
	}
	echoAnthropicVersion(w, r)
	h.writeJSON(w, http.StatusOK, anthropicFromOpenAI(response, request.Model))
}

// anthropicStream serves POST /v1/messages with stream=true: the OpenAI
// chunk stream is translated into Anthropic events.
func (h *Handler) anthropicStream(w http.ResponseWriter, r *http.Request, chatRequest *models.ChatCompletionRequest, token *models.RouterToken, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "server_error", "streaming is not supported by this server", nil)
		return
	}

	httpkit.WriteSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	translatedWriter := newAnthropicStreamWriter(w, flusher, chatRequest.Model.String())
	err := h.router.CompleteStream(r.Context(), chatRequest, translatedWriter, token)
	duration := time.Since(start)

	providerID, _ := h.router.GetProviderIDForModel(r.Context(), chatRequest.Model)
	tokenID := ""
	if token != nil {
		tokenID = token.ID
	}

	if err != nil {
		h.logger.Error("anthropic stream error", "err", err)
		_ = translatedWriter.emit("error", map[string]any{
			"type":  "error",
			"error": map[string]any{"type": "api_error", "message": err.Error()},
		})
	}
	h.metrics.RecordRequest(BuildStreamMetricEvent(start, chatRequest.Model, providerID, tokenID, duration, err))
}

// handleStreamWithMetrics wraps streaming with metrics collection.
func (h *Handler) handleStreamWithMetrics(w http.ResponseWriter, r *http.Request, request *models.ChatCompletionRequest, token *models.RouterToken, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "server_error", "streaming is not supported by this server", nil)
		return
	}

	httpkit.WriteSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	err := h.router.CompleteStream(r.Context(), request, w, token)
	duration := time.Since(start)

	// Extract provider info
	providerID, _ := h.router.GetProviderIDForModel(r.Context(), request.Model)

	// Build metric event
	tokenID := ""
	if token != nil {
		tokenID = token.ID
	}

	if err != nil {
		h.logger.Error("stream error", "err", err)

		// Send error in OpenAI-compatible format as SSE event
		WriteOpenAIStreamError(w, flusher, err)
	}

	// Record metrics
	h.metrics.RecordRequest(BuildStreamMetricEvent(start, request.Model, providerID, tokenID, duration, err))
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
func (h *Handler) listModels(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	entryFor := func(provider *models.ProviderInstance, modelView modelinfo.ModelView) models.ModelEntry {
		entry := models.ModelEntry{
			ID:                  provider.ID + "/" + modelView.Name,
			Object:              "model",
			Created:             provider.CreatedAt.Unix(),
			OwnedBy:             provider.TypeKey,
			Name:                modelView.DisplayName,
			Description:         modelView.Description,
			ContextLength:       modelView.ContextWindow,
			MaxCompletionTokens: modelView.MaxTokens,
			Reasoning:           modelView.Reasoning,
			SupportedParameters: modelView.SupportedParameters,
		}
		if len(modelView.InputModalities) > 0 || len(modelView.OutputModalities) > 0 {
			architecture := &models.ModelArchitecture{
				InputModalities:  modelView.InputModalities,
				OutputModalities: modelView.OutputModalities,
			}
			architecture.Modality = models.JoinModalities(modelView.InputModalities) + "->" + models.JoinModalities(modelView.OutputModalities)
			entry.Architecture = architecture
		}
		return entry
	}

	var entries []models.ModelEntry
	// byFullID indexes the already-fetched member views for virtual-model
	// resolution below: no second lookup pass.
	byFullID := map[string]modelinfo.ModelView{}
	// Global listing like dashboard Available Models, not per-token.
	// Never hide a provider on discovery error — log and optionally emit synthetic entry.
	if h.providerSvc != nil && h.modelInfoSvc != nil {
		if providers, err := h.providerSvc.List(r.Context()); err == nil {
			for _, providerInstance := range providers {
				if providerInstance.TypeKey == provider.TypeVirtual || providerInstance.Disabled {
					continue
				}
				modelViews, err := h.modelInfoSvc.MergedView(r.Context(), providerInstance.ID)
				if err != nil {
					h.logger.Warn("v1 listModels: model discovery failed", "provider_id", providerInstance.ID, "err", err)
					if providerInstance.TypeKey == "custom" {
						// Compat servers may not implement GET /models — still advertise provider as routable.
						entries = append(entries, models.ModelEntry{
							ID:      providerInstance.ID + "/*",
							Object:  "model",
							Created: providerInstance.CreatedAt.Unix(),
							OwnedBy: providerInstance.TypeKey,
						})
					}
					continue
				}
				if len(modelViews) == 0 {
					if providerInstance.TypeKey == "custom" {
						entries = append(entries, models.ModelEntry{
							ID:      providerInstance.ID + "/*",
							Object:  "model",
							Created: providerInstance.CreatedAt.Unix(),
							OwnedBy: providerInstance.TypeKey,
						})
					}
					continue
				}
				for _, memberView := range modelViews {
					if memberView.Disabled {
						continue
					}
					byFullID[providerInstance.ID+"/"+memberView.Name] = memberView
					entries = append(entries, entryFor(providerInstance, memberView))
				}
			}
		}
	}
	if h.virtualSvc != nil {
		if agents, err := h.virtualSvc.List(r.Context()); err == nil {
			// Virtual metadata is folded serve-time from the member views
			// above: unknown members are skipped, a virtual model with no
			// available members stays hidden. Pure in-memory work, fanned out.
			type resolvedVirtual struct {
				agent    *models.VirtualModel
				metadata virtual.Metadata
			}
			var mutex sync.Mutex
			var resolvedVirtuals []resolvedVirtual
			var waitGroup sync.WaitGroup
			for _, agent := range agents {
				if agent.Disabled {
					continue
				}
				waitGroup.Add(1)
				go func(agent *models.VirtualModel) {
					defer waitGroup.Done()
					members := make([]modelinfo.ModelView, 0, len(agent.Models))
					for _, entry := range agent.Models {
						memberView, ok := byFullID[string(entry.ModelID)]
						if !ok {
							h.logger.Warn("v1 listModels: virtual member not found, skipping",
								"virtual_id", agent.ID, "member", string(entry.ModelID))
							continue
						}
						members = append(members, memberView)
					}
					if len(members) == 0 {
						h.logger.Warn("v1 listModels: virtual model has no available members, hiding",
							"virtual_id", agent.ID)
						return
					}
					mutex.Lock()
					memberInfos := make([]models.ModelInfo, 0, len(members))
					for _, memberView := range members {
						memberInfos = append(memberInfos, memberView.ModelInfo)
					}
					resolvedVirtuals = append(resolvedVirtuals, resolvedVirtual{agent: agent, metadata: virtual.FoldMembers(memberInfos)})
					mutex.Unlock()
				}(agent)
			}
			waitGroup.Wait()
			for _, resolved := range resolvedVirtuals {
				agent, metadata := resolved.agent, resolved.metadata
				entry := models.ModelEntry{
					ID:                  provider.TypeVirtual + "/" + agent.ID,
					Object:              "model",
					Created:             agent.CreatedAt.Unix(),
					OwnedBy:             provider.TypeVirtual,
					Name:                agent.Name,
					Description:         agent.Description,
					ContextLength:       metadata.ContextLength,
					MaxCompletionTokens: metadata.MaxCompletionTokens,
					Reasoning:           metadata.Reasoning,
					SupportedParameters: metadata.SupportedParameters,
					Capabilities:        metadata.Capabilities,
				}
				if len(metadata.InputModalities) > 0 || len(metadata.OutputModalities) > 0 {
					entry.Architecture = &models.ModelArchitecture{
						InputModalities:  metadata.InputModalities,
						OutputModalities: metadata.OutputModalities,
						Modality:         models.JoinModalities(metadata.InputModalities) + "->" + models.JoinModalities(metadata.OutputModalities),
					}
				}
				entries = append(entries, entry)
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	if entries == nil {
		entries = []models.ModelEntry{}
	}
	if token != nil {
		entries = filterModelsByToken(token, entries)
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
func filterModelsByToken(token *models.RouterToken, entries []models.ModelEntry) []models.ModelEntry {
	kept := entries[:0]
	for _, entry := range entries {
		if token.Rules.Allows(models.ModelId(entry.ID)) {
			kept = append(kept, entry)
		}
	}
	if kept == nil {
		kept = []models.ModelEntry{}
	}
	return kept
}

// anthropicModelList renders model entries in the Anthropic models shape.
func anthropicModelList(entries []models.ModelEntry) models.AnthropicModelListResponse {
	output := models.AnthropicModelListResponse{Data: make([]models.AnthropicModelEntry, 0, len(entries))}
	for _, entry := range entries {
		anthropicEntry := models.AnthropicModelEntry{Type: "model", ID: entry.ID, DisplayName: entry.Name}
		if entry.Created > 0 {
			anthropicEntry.CreatedAt = time.Unix(entry.Created, 0).UTC().Format(time.RFC3339)
		}
		output.Data = append(output.Data, anthropicEntry)
	}
	if count := len(output.Data); count > 0 {
		first, last := output.Data[0].ID, output.Data[count-1].ID
		output.FirstID, output.LastID = &first, &last
	}
	return output
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
func (h *Handler) retrieveModel(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	modelPath := strings.TrimPrefix(r.URL.Path, "/v1/models/")
	if modelPath == "" || modelPath == r.URL.Path {
		if pathValue := r.PathValue("model"); pathValue != "" {
			modelPath = pathValue
		}
	}
	modelPath = strings.TrimSuffix(modelPath, "/")
	if modelPath == "" {
		h.writeCompatError(w, r, http.StatusBadRequest, "invalid_request_error", "model id is required", stringPointer("model"))
		return
	}
	modelID := models.ModelId(modelPath)
	providerID, modelName, err := modelID.Parse()
	if err != nil || providerID == "" || modelName == "" {
		h.writeCompatError(w, r, http.StatusNotFound, "not_found", fmt.Sprintf("The model '%s' does not exist", modelPath), nil)
		return
	}
	if token != nil && !token.Rules.Allows(modelID) {
		h.writeCompatError(w, r, http.StatusNotFound, "not_found", fmt.Sprintf("The model '%s' does not exist", modelPath), nil)
		return
	}
	// Check global existence via modelInfo or virtual models
	exists := false
	var ownedBy string
	var created int64
	var modelView *modelinfo.ModelView
	var virtualAgent *models.VirtualModel
	var virtualMetadata virtual.Metadata
	if providerID == provider.TypeVirtual && h.virtualSvc != nil {
		if agent, err := h.virtualSvc.Get(r.Context(), modelName); err == nil && !agent.Disabled {
			// Members resolve cache-only: a single lookup must never ping
			// upstreams. Unknown members are skipped; no available members
			// means the virtual model does not exist for this endpoint.
			if h.modelInfoSvc != nil {
				need := map[string][]string{}
				for _, entry := range agent.Models {
					memberProviderID, memberName, parseError := entry.ModelID.Parse()
					if parseError != nil {
						h.logger.Warn("v1 retrieveModel: virtual member id invalid, skipping",
							"virtual_id", agent.ID, "member", string(entry.ModelID))
						continue
					}
					need[memberProviderID] = append(need[memberProviderID], memberName)
				}
				var members []modelinfo.ModelView
				for memberProviderID, memberNames := range need {
					memberViews, viewError := h.modelInfoSvc.PeekMergedView(r.Context(), memberProviderID)
					if viewError != nil {
						h.logger.Warn("v1 retrieveModel: provider view unavailable, skipping members",
							"virtual_id", agent.ID, "provider_id", memberProviderID)
						continue
					}
					byName := make(map[string]modelinfo.ModelView, len(memberViews))
					for _, view := range memberViews {
						if !view.Disabled {
							byName[view.Name] = view
						}
					}
					for _, memberName := range memberNames {
						memberView, ok := byName[memberName]
						if !ok {
							h.logger.Warn("v1 retrieveModel: virtual member not found, skipping",
								"virtual_id", agent.ID, "member", memberProviderID+"/"+memberName)
							continue
						}
						members = append(members, memberView)
					}
				}
				if len(members) > 0 {
					exists = true
					ownedBy = provider.TypeVirtual
					created = agent.CreatedAt.Unix()
					virtualAgent = agent
					memberInfos := make([]models.ModelInfo, 0, len(members))
					for _, memberView := range members {
						memberInfos = append(memberInfos, memberView.ModelInfo)
					}
					virtualMetadata = virtual.FoldMembers(memberInfos)
				} else {
					h.logger.Warn("v1 retrieveModel: virtual model has no available members, hiding",
						"virtual_id", agent.ID)
				}
			}
		}
	} else if h.providerSvc != nil && h.modelInfoSvc != nil {
		if provider, err := h.providerSvc.Get(r.Context(), providerID); err == nil {
			ownedBy = provider.TypeKey
			created = provider.CreatedAt.Unix()
			if views, err := h.modelInfoSvc.MergedView(r.Context(), providerID); err == nil {
				for i := range views {
					if views[i].Name == modelName && !views[i].Disabled {
						exists = true
						modelView = &views[i]
						break
					}
				}
			}
		}
	}
	if !exists {
		h.writeCompatError(w, r, http.StatusNotFound, "not_found", fmt.Sprintf("The model '%s' does not exist", modelPath), nil)
		return
	}
	if isAnthropicStyle(r) {
		echoAnthropicVersion(w, r)
		name := ""
		if virtualAgent != nil {
			name = virtualAgent.Name
		}
		if modelView != nil && modelView.DisplayName != "" {
			name = modelView.DisplayName
		}
		entry := models.AnthropicModelEntry{Type: "model", ID: modelPath, DisplayName: name}
		if created > 0 {
			entry.CreatedAt = time.Unix(created, 0).UTC().Format(time.RFC3339)
		}
		h.writeJSON(w, http.StatusOK, entry)
		return
	}
	output := map[string]any{
		"id":       modelPath,
		"object":   "model",
		"created":  created,
		"owned_by": ownedBy,
	}
	if virtualAgent != nil {
		output["name"] = virtualAgent.Name
		if virtualAgent.Description != "" {
			output["description"] = virtualAgent.Description
		}
		if virtualMetadata.ContextLength > 0 {
			output["context_length"] = virtualMetadata.ContextLength
		}
		if virtualMetadata.MaxCompletionTokens > 0 {
			output["max_completion_tokens"] = virtualMetadata.MaxCompletionTokens
		}
		if len(virtualMetadata.Capabilities) > 0 {
			output["capabilities"] = virtualMetadata.Capabilities
		}
		if len(virtualMetadata.InputModalities) > 0 || len(virtualMetadata.OutputModalities) > 0 {
			output["architecture"] = map[string]any{
				"input_modalities":  virtualMetadata.InputModalities,
				"output_modalities": virtualMetadata.OutputModalities,
				"modality":          models.JoinModalities(virtualMetadata.InputModalities) + "->" + models.JoinModalities(virtualMetadata.OutputModalities),
			}
		}
		if virtualMetadata.Reasoning != nil {
			output["reasoning"] = virtualMetadata.Reasoning
		}
		if len(virtualMetadata.SupportedParameters) > 0 {
			output["supported_parameters"] = virtualMetadata.SupportedParameters
		}
	}
	if modelView != nil {
		if modelView.DisplayName != "" {
			output["name"] = modelView.DisplayName
		}
		if modelView.Description != "" {
			output["description"] = modelView.Description
		}
		if modelView.ContextWindow > 0 {
			output["context_length"] = modelView.ContextWindow
		}
		if modelView.MaxTokens > 0 {
			output["max_completion_tokens"] = modelView.MaxTokens
		}
		if len(modelView.InputModalities) > 0 || len(modelView.OutputModalities) > 0 {
			output["architecture"] = map[string]any{
				"input_modalities":  modelView.InputModalities,
				"output_modalities": modelView.OutputModalities,
				"modality":          models.JoinModalities(modelView.InputModalities) + "->" + models.JoinModalities(modelView.OutputModalities),
			}
		}
		if modelView.Reasoning != nil {
			output["reasoning"] = modelView.Reasoning
		}
		if len(modelView.SupportedParameters) > 0 {
			output["supported_parameters"] = modelView.SupportedParameters
		}
	}
	h.writeJSON(w, http.StatusOK, output)
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

type authenticatedHandler func(w http.ResponseWriter, r *http.Request, token *models.RouterToken)

// auth extracts and validates the router token. Authorization: Bearer
// llmr_* and the x-api-key: llmr_* alias (Anthropic SDK style) both work on
// every /v1 route. allowAnonymous stays for the NoAuth-adjacent probes only;
// production routes pass false.
func (h *Handler) auth(next authenticatedHandler, allowAnonymous bool) http.HandlerFunc {
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
			h.writeError(w, http.StatusUnauthorized, "invalid_request_error", "You didn't provide an API key. Provide it in an Authorization: Bearer header or an x-api-key header.", stringPointer("Authorization"))
			return
		}

		token, err := h.tokens.Validate(r.Context(), raw)
		if err != nil {
			if errors.Is(err, apierrors.ErrUnauthorized) {
				h.writeError(w, http.StatusUnauthorized, "invalid_request_error", "Incorrect API key provided. You can find your API key at https://platform.openai.com/account/api-keys.", nil)
				return
			}
			h.writeError(w, http.StatusInternalServerError, "server_error", "token validation failed", nil)
			return
		}

		next(w, r, token)
	}
}

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

// extractToken reads the router token from Authorization: Bearer or the
// x-api-key alias. Bearer wins when both are present.
func extractToken(r *http.Request) string {
	if value := r.Header.Get("Authorization"); value != "" {
		if after, ok := strings.CutPrefix(value, "Bearer "); ok {
			if key := strings.TrimSpace(after); key != "" {
				return key
			}
		}
	}
	if value := strings.TrimSpace(r.Header.Get("x-api-key")); value != "" {
		if after, ok := strings.CutPrefix(value, "Bearer "); ok {
			return strings.TrimSpace(after)
		}
		return value
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
	routerError := h.classifyError(err)
	apierrors.WriteResponseHeaders(w, pluginResponseHeaders(err))
	h.writeError(w, routerError.status, routerError.code, wireMessage(err), nil)
}

// pluginResponseHeaders returns the sanitized response headers a plugin
// attached to its terminal error, or nil. Only unary paths call it: once
// streaming starts, headers no longer reach the client.
func pluginResponseHeaders(err error) map[string]string {
	var providerError *models.ProviderError
	if errors.As(err, &providerError) {
		return providerError.Headers
	}
	return nil
}

// wireMessage renders the client-facing message: the raw plugin message
// for terminal provider errors, the Go error otherwise.
func wireMessage(err error) string {
	var providerError *models.ProviderError
	if errors.As(err, &providerError) && providerError.Message != "" {
		return providerError.Message
	}
	return err.Error()
}

type routerError struct {
	status int
	code   string
}

func (h *Handler) classifyError(err error) routerError {
	apiError := apierrors.ToAPIError(err)
	return routerError{apiError.Status, apiError.Code}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string, parameter *string) {
	// Map internal code to OpenAI error type
	errType := errorTypeForCode(code)
	h.writeJSON(w, status, models.OpenAIError{
		Error: models.OpenAIErrorBody{
			Message: message,
			Type:    errType,
			Param:   parameter,
			Code:    code,
		},
	})
}

func stringPointer(text string) *string { return &text }

func errorTypeForCode(code string) string {
	return apierrors.ErrorTypeForCode(code)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		h.logger.Error("json encode error", "err", err)
	}
}
