package v1

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/httpkit"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// ─────────────────────────────────────────────
// Legacy completions (POST /v1/completions)
// ─────────────────────────────────────────────

// legacyCompletions handles POST /v1/completions
// @Summary      Create completion (legacy)
// @Description  Legacy text completions, first-class. The prompt maps to a single chat
// @Description  turn (plus suffix); system/images/options aliases translate like chat.
// @Description  Token-id prompts are refused (no tokenizer upstream of the router).
// @Description  OpenAI SSE terminates with data: [DONE].
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        request body models.CompletionRequest true "Completion request"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.CompletionResponse "Completion"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      403 {object} models.OpenAIError "Forbidden - model not allowed by token rules"
// @Failure      502 {object} models.OpenAIError "Bad Gateway - upstream provider error"
// @Failure      503 {object} models.OpenAIError "Service Unavailable - no credential available"
// @Router       /v1/completions [post]
// @Security     BearerAuth
func (h *Handler) legacyCompletions(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	var request models.CompletionRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	if request.Model == "" {
		h.logValidationFailure(r, "", "invalid_request_error")
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", stringPointer("model"))
		return
	}
	if len(request.Prompt) == 0 {
		h.logValidationFailure(r, request.Model, "invalid_request_error")
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'prompt'", stringPointer("prompt"))
		return
	}
	if deny := authorizeModel(token, request.Model); deny != nil {
		h.logDenyFailure(r, request.Model, deny)
		h.writeError(w, deny.status, deny.code, deny.message, deny.parameter)
		return
	}
	chatRequest := request.ToChat()
	if request.Stream {
		h.handleCompletionStreamWithMetrics(w, r, chatRequest, &request, token, start)
		return
	}
	response, err := h.router.Complete(r.Context(), chatRequest, token)
	duration := time.Since(start)
	var usage *models.ChatCompletionUsage
	if err == nil && response != nil {
		usage = &response.Usage
	}
	h.recordRouteMetric(r.Context(), start, request.Model, token, duration, err, usage)
	if err != nil {
		h.logRouterFailure(r, request.Model, err)
		h.handleRouterError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, completionFromOpenAI(response, &request))
}

// completionFromOpenAI renders a chat completion as a text_completion: first
// prompt plus echo prefix, per-choice text, usage carried over.
func completionFromOpenAI(response *models.ChatCompletionResponse, request *models.CompletionRequest) *models.CompletionResponse {
	output := &models.CompletionResponse{
		ID:      response.ID,
		Object:  "text_completion",
		Created: response.Created,
		Model:   response.Model,
		Usage:   response.Usage,
	}
	if output.ID == "" {
		output.ID = "cmpl_router"
	}
	prefix := ""
	if request.Echo != nil && *request.Echo {
		prefix = strings.Join(request.Prompt, "\n")
		if request.Suffix != "" {
			prefix += request.Suffix
		}
	}
	for i, choice := range response.Choices {
		completionChoice := models.CompletionChoice{
			Index:        i,
			Text:         prefix + choice.Message.Content,
			FinishReason: choice.FinishReason,
			Logprobs:     choice.Logprobs,
		}
		output.Choices = append(output.Choices, completionChoice)
	}
	if output.Choices == nil {
		output.Choices = []models.CompletionChoice{}
	}
	return output
}

// completionStreamWriter translates chat chunk SSE frames into
// text_completion frames. Non-chunk frames ([DONE], error objects) pass
// through untouched.
type completionStreamWriter struct {
	w      io.Writer
	buffer bytes.Buffer
}

func (writer *completionStreamWriter) Write(p []byte) (int, error) {
	writer.buffer.Write(p)
	for {
		raw := writer.buffer.String()
		index := strings.Index(raw, "\n\n")
		if index < 0 {
			break
		}
		frame := raw[:index]
		writer.buffer.Next(index + 2)
		writer.emitFrame(frame)
	}
	return len(p), nil
}

func (writer *completionStreamWriter) emitFrame(frame string) {
	line := strings.TrimSpace(frame)
	if !strings.HasPrefix(line, "data: ") {
		_, _ = io.WriteString(writer.w, frame+"\n\n")
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
	if payload == "" || payload == "[DONE]" {
		_, _ = io.WriteString(writer.w, frame+"\n\n")
		return
	}
	var chunk models.StreamChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil || len(chunk.Choices) == 0 {
		_, _ = io.WriteString(writer.w, frame+"\n\n")
		return
	}
	type completionDelta struct {
		Text         string `json:"text"`
		Index        int    `json:"index"`
		FinishReason any    `json:"finish_reason"`
	}
	type completionChunk struct {
		ID      string            `json:"id"`
		Object  string            `json:"object"`
		Created int64             `json:"created"`
		Model   string            `json:"model"`
		Choices []completionDelta `json:"choices"`
	}
	output := completionChunk{ID: chunk.ID, Object: "text_completion", Created: chunk.Created, Model: chunk.Model}
	for _, choice := range chunk.Choices {
		var finish any
		if choice.FinishReason != nil {
			finish = *choice.FinishReason
		}
		output.Choices = append(output.Choices, completionDelta{
			Text:         choice.Delta.TextContent(),
			Index:        choice.Index,
			FinishReason: finish,
		})
	}
	raw, err := json.Marshal(output)
	if err != nil {
		_, _ = io.WriteString(writer.w, frame+"\n\n")
		return
	}
	_, _ = io.WriteString(writer.w, "data: "+string(raw)+"\n\n")
}

// handleCompletionStreamWithMetrics wraps completion streaming with metrics
// collection, translating chat chunks into text_completion frames.
func (h *Handler) handleCompletionStreamWithMetrics(w http.ResponseWriter, r *http.Request, chatRequest *models.ChatCompletionRequest, request *models.CompletionRequest, token *models.RouterToken, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		if h.logger != nil {
			h.logger.Error("Streaming not supported",
				"endpoint", endpointFromRequest(r),
				"code", apierrors.CodeServerError,
				"model", string(request.Model))
		}
		h.writeError(w, http.StatusInternalServerError, "server_error", "streaming is not supported by this server", nil)
		return
	}
	httpkit.WriteSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	translatedWriter := &completionStreamWriter{w: w}
	err := h.router.CompleteStream(r.Context(), chatRequest, translatedWriter, token)
	duration := time.Since(start)

	providerID, _ := h.router.GetProviderIDForModel(r.Context(), request.Model)
	tokenID := ""
	if token != nil {
		tokenID = token.ID
	}
	if err != nil {
		if h.logger != nil {
			h.logger.Error("Completion stream error",
				"endpoint", endpointFromRequest(r),
				"code", apierrors.ToAPIError(err).Code,
				"model", string(request.Model),
				"provider_id", providerID,
				"error", err)
		}
		WriteOpenAIStreamError(w, flusher, err)
	}
	h.metrics.RecordRequest(BuildStreamMetricEvent(start, request.Model, providerID, tokenID, duration, err))
}

// ─────────────────────────────────────────────
// Audio translations (POST /v1/audio/translations)
// ─────────────────────────────────────────────

// audioTranslations handles POST /v1/audio/translations
// @Summary      Create audio translation
// @Description  Translates an uploaded audio file to English text. Same multipart
// @Description  pipeline as transcriptions (file and model required); the task is
// @Description  recorded as translate on the response.
// @Tags         OpenAI API
// @Accept       mpfd
// @Produce      json
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.TranscriptionResponse "Translated text"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Router       /v1/audio/translations [post]
// @Security     BearerAuth
func (h *Handler) audioTranslations(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	request, err := parseTranscriptionRequest(r)
	if err != nil {
		h.logValidationFailure(r, "", "invalid_request_error")
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}
	if deny := authorizeModel(token, request.Model); deny != nil {
		h.logDenyFailure(r, request.Model, deny)
		h.writeError(w, deny.status, deny.code, deny.message, deny.parameter)
		return
	}
	response, err := h.router.Transcribe(r.Context(), request, token)
	duration := time.Since(start)
	h.recordRouteMetric(r.Context(), start, request.Model, token, duration, err, nil)
	if err != nil {
		h.logRouterFailure(r, request.Model, err)
		h.handleRouterError(w, err)
		return
	}
	task := "translate"
	response.Task = &task
	h.writeTranscriptionResponse(w, request.ResponseFormat, response)
}

// ─────────────────────────────────────────────
// Moderations (POST /v1/moderations)
// ─────────────────────────────────────────────

// moderations handles POST /v1/moderations
// @Summary      Create moderation
// @Description  Scores inputs against the provider's safety categories. Input accepts
// @Description  a string, a list of strings, or text parts (Mistral chat/moderations
// @Description  shape joins to text). Providers without a moderate handler report
// @Description  endpoint_not_supported.
// @Tags         OpenAI API
// @Accept       json
// @Produce      json
// @Param        request body models.ModerationRequest true "Moderation request"
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ModerationResponse "Moderation verdicts"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      403 {object} models.OpenAIError "Forbidden - model not allowed by token rules"
// @Failure      502 {object} models.OpenAIError "Bad Gateway - upstream provider error"
// @Failure      503 {object} models.OpenAIError "Service Unavailable - no credential available"
// @Router       /v1/moderations [post]
// @Security     BearerAuth
func (h *Handler) moderations(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	var request models.ModerationRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	if request.Model == "" {
		h.logValidationFailure(r, "", "invalid_request_error")
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", stringPointer("model"))
		return
	}
	if len(request.Input) == 0 {
		h.logValidationFailure(r, request.Model, "invalid_request_error")
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'input'", stringPointer("input"))
		return
	}
	if deny := authorizeModel(token, request.Model); deny != nil {
		h.logDenyFailure(r, request.Model, deny)
		h.writeError(w, deny.status, deny.code, deny.message, deny.parameter)
		return
	}
	response, err := h.router.Moderate(r.Context(), &request, token)
	duration := time.Since(start)
	h.recordRouteMetric(r.Context(), start, request.Model, token, duration, err, nil)
	if err != nil {
		h.logRouterFailure(r, request.Model, err)
		h.handleRouterError(w, err)
		return
	}
	if response.ID == "" {
		response.ID = models.NewModerationID()
	}
	if response.Model == "" {
		response.Model = request.Model.String()
	}
	h.writeJSON(w, http.StatusOK, response)
}

// parseJSONTranscriptionRequest reads the OpenRouter JSON STT variant:
// model plus input_audio{data,format} where data is base64 audio or an
// http(s) URL downloaded under the audio size cap.
func parseJSONTranscriptionRequest(r *http.Request) (*models.TranscriptionRequest, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxAudioUploadBytes)
	var body struct {
		Model          string   `json:"model"`
		Language       string   `json:"language"`
		Prompt         string   `json:"prompt"`
		ResponseFormat string   `json:"response_format"`
		Temperature    *float64 `json:"temperature"`
		InputAudio     *struct {
			Data   string `json:"data"`
			Format string `json:"format"`
		} `json:"input_audio"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("malformed request body: %s", err)
	}
	if strings.TrimSpace(body.Model) == "" {
		return nil, fmt.Errorf("missing required field 'model'")
	}
	if body.InputAudio == nil || strings.TrimSpace(body.InputAudio.Data) == "" {
		return nil, fmt.Errorf("missing required field 'input_audio.data'")
	}
	format := strings.ToLower(strings.TrimSpace(body.InputAudio.Format))
	audio, fileName, contentType, err := fetchSTTAudio(strings.TrimSpace(body.InputAudio.Data), format)
	if err != nil {
		return nil, err
	}
	switch strings.TrimSpace(body.ResponseFormat) {
	case "", "json", "text", "srt", "verbose_json", "vtt":
	default:
		return nil, fmt.Errorf("invalid response_format %q: expected json, text, srt, verbose_json or vtt", body.ResponseFormat)
	}
	return &models.TranscriptionRequest{
		Model:          models.ModelId(strings.TrimSpace(body.Model)),
		File:           audio,
		FileName:       fileName,
		ContentType:    contentType,
		Language:       strings.TrimSpace(body.Language),
		Prompt:         body.Prompt,
		ResponseFormat: strings.TrimSpace(body.ResponseFormat),
		Temperature:    body.Temperature,
	}, nil
}

// fetchSTTAudio resolves JSON-variant audio: base64 bytes inline, or an
// http(s) URL downloaded with a 60s timeout under the audio size cap.
func fetchSTTAudio(data, format string) (audio []byte, fileName, contentType string, err error) {
	fileName, contentType = sttFileShape(format)
	if strings.HasPrefix(data, "http://") || strings.HasPrefix(data, "https://") {
		client := &http.Client{Timeout: 60 * time.Second}
		fetchResponse, downloadError := client.Get(data)
		if downloadError != nil {
			return nil, "", "", fmt.Errorf("download input_audio: %s", downloadError)
		}
		defer fetchResponse.Body.Close()
		if fetchResponse.StatusCode != http.StatusOK {
			return nil, "", "", fmt.Errorf("download input_audio: unexpected status %s", fetchResponse.Status)
		}
		raw, downloadError := io.ReadAll(io.LimitReader(fetchResponse.Body, maxAudioUploadBytes+1))
		if downloadError != nil {
			return nil, "", "", fmt.Errorf("download input_audio: %s", downloadError)
		}
		if len(raw) == 0 {
			return nil, "", "", fmt.Errorf("downloaded audio is empty")
		}
		if len(raw) > maxAudioUploadBytes {
			return nil, "", "", fmt.Errorf("downloaded audio exceeds the size limit")
		}
		return raw, fileName, contentType, nil
	}
	raw, downloadError := base64.StdEncoding.DecodeString(data)
	if downloadError != nil {
		return nil, "", "", fmt.Errorf("input_audio.data is not base64 audio or an http(s) URL")
	}
	if len(raw) == 0 {
		return nil, "", "", fmt.Errorf("input_audio.data is empty")
	}
	return raw, fileName, contentType, nil
}

// sttFileShape maps an audio format hint to file name and content type.
func sttFileShape(format string) (string, string) {
	switch format {
	case "wav":
		return "clip.wav", "audio/wav"
	case "mp3":
		return "clip.mp3", "audio/mpeg"
	case "ogg", "opus":
		return "clip.ogg", "audio/ogg"
	case "flac":
		return "clip.flac", "audio/flac"
	case "m4a":
		return "clip.m4a", "audio/mp4"
	case "webm":
		return "clip.webm", "audio/webm"
	default:
		return "clip.bin", "application/octet-stream"
	}
}

// ─────────────────────────────────────────────
// Image edits + variations (multipart, same response as generations)
// ─────────────────────────────────────────────

// maxImageUploadBytes caps one image upload (32 MB headroom for form overhead).
const maxImageUploadBytes = 32 << 20

// parseImageEditRequest reads the multipart body of POST /v1/images/edits
// and POST /v1/images/variations. Edits require prompt; variations accept
// an empty prompt (the router substitutes a variation instruction).
func parseImageEditRequest(r *http.Request, needPrompt bool) (*models.ImageGenerationRequest, error) {
	contentType := r.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		return nil, fmt.Errorf("Content-Type must be multipart/form-data")
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxImageUploadBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return nil, fmt.Errorf("malformed multipart body: %s", err)
	}
	model := strings.TrimSpace(r.FormValue("model"))
	if model == "" {
		return nil, fmt.Errorf("missing required field 'model'")
	}
	prompt := r.FormValue("prompt")
	if needPrompt && strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("missing required field 'prompt'")
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		return nil, fmt.Errorf("missing required field 'image'")
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read image: %s", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("image is empty")
	}
	request := &models.ImageGenerationRequest{
		Model:     models.ModelId(model),
		Prompt:    prompt,
		ImageB64:  base64.StdEncoding.EncodeToString(data),
		ImageName: header.Filename,
	}
	if countText := strings.TrimSpace(r.FormValue("n")); countText != "" {
		count, err := strconv.Atoi(countText)
		if err != nil || count < 1 || count > 10 {
			return nil, fmt.Errorf("invalid n %q: expected 1..10", countText)
		}
		request.N = count
	}
	if value := strings.TrimSpace(r.FormValue("size")); value != "" {
		request.Size = value
	}
	if value := strings.TrimSpace(r.FormValue("response_format")); value != "" {
		if value != "url" && value != "b64_json" {
			return nil, fmt.Errorf("invalid response_format %q: expected url or b64_json", value)
		}
		request.ResponseFormat = value
	}
	if mask, maskHeader, err := r.FormFile("mask"); err == nil {
		defer mask.Close()
		maskData, err := io.ReadAll(mask)
		if err != nil {
			return nil, fmt.Errorf("read mask: %s", err)
		}
		if len(maskData) > 0 {
			request.MaskB64 = base64.StdEncoding.EncodeToString(maskData)
		}
		_ = maskHeader
	}
	return request, nil
}

// imageEdits handles POST /v1/images/edits
// @Summary      Create image edit
// @Description  Edits an uploaded image by prompt. Multipart form: image (required),
// @Description  mask, prompt (required), model (required), n, size,
// @Description  response_format (url|b64_json). Reuses the generations response.
// @Tags         OpenAI API
// @Accept       mpfd
// @Produce      json
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ImageGenerationResponse "Edited images"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      403 {object} models.OpenAIError "Forbidden - model not allowed by token rules"
// @Failure      502 {object} models.OpenAIError "Bad Gateway - upstream provider error"
// @Failure      503 {object} models.OpenAIError "Service Unavailable - no credential available"
// @Router       /v1/images/edits [post]
// @Security     BearerAuth
func (h *Handler) imageEdits(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	request, err := parseImageEditRequest(r, true)
	if err != nil {
		h.logValidationFailure(r, "", "invalid_request_error")
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}
	if deny := authorizeModel(token, request.Model); deny != nil {
		h.logDenyFailure(r, request.Model, deny)
		h.writeError(w, deny.status, deny.code, deny.message, deny.parameter)
		return
	}
	response, err := h.router.GenerateImage(r.Context(), request, token)
	duration := time.Since(start)
	h.recordRouteMetric(r.Context(), start, request.Model, token, duration, err, nil)
	if err != nil {
		h.logRouterFailure(r, request.Model, err)
		h.handleRouterError(w, err)
		return
	}
	if response.Created == 0 {
		response.Created = time.Now().Unix()
	}
	h.writeJSON(w, http.StatusOK, response)
}

// imageVariations handles POST /v1/images/variations
// @Summary      Create image variation
// @Description  Varies an uploaded image. Multipart form: image (required), model
// @Description  (required), n, size, response_format (url|b64_json). First-class
// @Description  despite the upstream deprecated label: old SDKs still call it.
// @Tags         OpenAI API
// @Accept       mpfd
// @Produce      json
// @Param        x-api-key header string false "Router token alias for Authorization: Bearer"
// @Success      200 {object} models.ImageGenerationResponse "Varied images"
// @Failure      400 {object} models.OpenAIError "Invalid request"
// @Failure      401 {object} models.OpenAIError "Unauthorized - invalid or missing token"
// @Failure      403 {object} models.OpenAIError "Forbidden - model not allowed by token rules"
// @Failure      502 {object} models.OpenAIError "Bad Gateway - upstream provider error"
// @Failure      503 {object} models.OpenAIError "Service Unavailable - no credential available"
// @Router       /v1/images/variations [post]
// @Security     BearerAuth
func (h *Handler) imageVariations(w http.ResponseWriter, r *http.Request, token *models.RouterToken) {
	start := time.Now()
	request, err := parseImageEditRequest(r, false)
	if err != nil {
		h.logValidationFailure(r, "", "invalid_request_error")
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}
	if strings.TrimSpace(request.Prompt) == "" {
		request.Prompt = "Generate a variation of the input image."
	}
	if deny := authorizeModel(token, request.Model); deny != nil {
		h.logDenyFailure(r, request.Model, deny)
		h.writeError(w, deny.status, deny.code, deny.message, deny.parameter)
		return
	}
	response, err := h.router.GenerateImage(r.Context(), request, token)
	duration := time.Since(start)
	h.recordRouteMetric(r.Context(), start, request.Model, token, duration, err, nil)
	if err != nil {
		h.logRouterFailure(r, request.Model, err)
		h.handleRouterError(w, err)
		return
	}
	if response.Created == 0 {
		response.Created = time.Now().Unix()
	}
	h.writeJSON(w, http.StatusOK, response)
}
