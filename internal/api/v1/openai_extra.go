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
func (h *Handler) legacyCompletions(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	var req models.CompletionRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if req.Model == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'model'", strPtr("model"))
		return
	}
	if len(req.Prompt) == 0 {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field 'prompt'", strPtr("prompt"))
		return
	}
	if deny := authorizeModel(t, req.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.msg, deny.param)
		return
	}
	chatReq := req.ToChat()
	if req.Stream {
		h.handleCompletionStreamWithMetrics(w, r, chatReq, &req, t, start)
		return
	}
	resp, err := h.router.Complete(r.Context(), chatReq, t)
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
	h.writeJSON(w, http.StatusOK, completionFromOpenAI(resp, &req))
}

// completionFromOpenAI renders a chat completion as a text_completion: first
// prompt plus echo prefix, per-choice text, usage carried over.
func completionFromOpenAI(resp *models.ChatCompletionResponse, req *models.CompletionRequest) *models.CompletionResponse {
	out := &models.CompletionResponse{
		ID:      resp.ID,
		Object:  "text_completion",
		Created: resp.Created,
		Model:   resp.Model,
		Usage:   resp.Usage,
	}
	if out.ID == "" {
		out.ID = "cmpl_router"
	}
	prefix := ""
	if req.Echo != nil && *req.Echo {
		prefix = strings.Join(req.Prompt, "\n")
		if req.Suffix != "" {
			prefix += req.Suffix
		}
	}
	for i, c := range resp.Choices {
		choice := models.CompletionChoice{
			Index:        i,
			Text:         prefix + c.Message.Content,
			FinishReason: c.FinishReason,
			Logprobs:     c.Logprobs,
		}
		out.Choices = append(out.Choices, choice)
	}
	if out.Choices == nil {
		out.Choices = []models.CompletionChoice{}
	}
	return out
}

// completionStreamWriter translates chat chunk SSE frames into
// text_completion frames. Non-chunk frames ([DONE], error objects) pass
// through untouched.
type completionStreamWriter struct {
	w   io.Writer
	buf bytes.Buffer
}

func (c *completionStreamWriter) Write(p []byte) (int, error) {
	c.buf.Write(p)
	for {
		raw := c.buf.String()
		idx := strings.Index(raw, "\n\n")
		if idx < 0 {
			break
		}
		frame := raw[:idx]
		c.buf.Next(idx + 2)
		c.emitFrame(frame)
	}
	return len(p), nil
}

func (c *completionStreamWriter) emitFrame(frame string) {
	line := strings.TrimSpace(frame)
	if !strings.HasPrefix(line, "data: ") {
		_, _ = io.WriteString(c.w, frame+"\n\n")
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
	if payload == "" || payload == "[DONE]" {
		_, _ = io.WriteString(c.w, frame+"\n\n")
		return
	}
	var chunk models.StreamChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil || len(chunk.Choices) == 0 {
		_, _ = io.WriteString(c.w, frame+"\n\n")
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
	out := completionChunk{ID: chunk.ID, Object: "text_completion", Created: chunk.Created, Model: chunk.Model}
	for _, ch := range chunk.Choices {
		var finish any
		if ch.FinishReason != nil {
			finish = *ch.FinishReason
		}
		out.Choices = append(out.Choices, completionDelta{
			Text:         ch.Delta.TextContent(),
			Index:        ch.Index,
			FinishReason: finish,
		})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		_, _ = io.WriteString(c.w, frame+"\n\n")
		return
	}
	_, _ = io.WriteString(c.w, "data: "+string(raw)+"\n\n")
}

// handleCompletionStreamWithMetrics wraps completion streaming with metrics
// collection, translating chat chunks into text_completion frames.
func (h *Handler) handleCompletionStreamWithMetrics(w http.ResponseWriter, r *http.Request, chatReq *models.ChatCompletionRequest, req *models.CompletionRequest, t *models.RouterToken, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "server_error", "streaming is not supported by this server", nil)
		return
	}
	httpkit.WriteSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	tw := &completionStreamWriter{w: w}
	err := h.router.CompleteStream(r.Context(), chatReq, tw, t)
	duration := time.Since(start)

	providerType, _, _ := req.Model.Parse()
	providerID, _ := h.router.GetProviderIDForModel(r.Context(), req.Model)
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
		h.logger.Error("completion stream error", "err", err)
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
	h.metrics.RecordRequest(event)
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
func (h *Handler) audioTranslations(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
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
	task := "translate"
	resp.Task = &task
	h.writeTranscriptionResponse(w, req.ResponseFormat, resp)
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
func (h *Handler) moderations(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	var req models.ModerationRequest
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
	if deny := authorizeModel(t, req.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.msg, deny.param)
		return
	}
	resp, err := h.router.Moderate(r.Context(), &req, t)
	duration := time.Since(start)
	h.recordRouteMetric(r.Context(), start, req.Model, t, duration, err, nil)
	if err != nil {
		h.handleRouterError(w, err)
		return
	}
	if resp.ID == "" {
		resp.ID = models.NewModerationID()
	}
	if resp.Model == "" {
		resp.Model = req.Model.String()
	}
	h.writeJSON(w, http.StatusOK, resp)
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
		resp, derr := client.Get(data)
		if derr != nil {
			return nil, "", "", fmt.Errorf("download input_audio: %s", derr)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, "", "", fmt.Errorf("download input_audio: unexpected status %s", resp.Status)
		}
		raw, derr := io.ReadAll(io.LimitReader(resp.Body, maxAudioUploadBytes+1))
		if derr != nil {
			return nil, "", "", fmt.Errorf("download input_audio: %s", derr)
		}
		if len(raw) == 0 {
			return nil, "", "", fmt.Errorf("downloaded audio is empty")
		}
		if len(raw) > maxAudioUploadBytes {
			return nil, "", "", fmt.Errorf("downloaded audio exceeds the size limit")
		}
		return raw, fileName, contentType, nil
	}
	raw, derr := base64.StdEncoding.DecodeString(data)
	if derr != nil {
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
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "multipart/form-data") {
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
	req := &models.ImageGenerationRequest{
		Model:     models.ModelId(model),
		Prompt:    prompt,
		ImageB64:  base64.StdEncoding.EncodeToString(data),
		ImageName: header.Filename,
	}
	if n := strings.TrimSpace(r.FormValue("n")); n != "" {
		v, err := strconv.Atoi(n)
		if err != nil || v < 1 || v > 10 {
			return nil, fmt.Errorf("invalid n %q: expected 1..10", n)
		}
		req.N = v
	}
	if v := strings.TrimSpace(r.FormValue("size")); v != "" {
		req.Size = v
	}
	if v := strings.TrimSpace(r.FormValue("response_format")); v != "" {
		if v != "url" && v != "b64_json" {
			return nil, fmt.Errorf("invalid response_format %q: expected url or b64_json", v)
		}
		req.ResponseFormat = v
	}
	if mask, maskHeader, err := r.FormFile("mask"); err == nil {
		defer mask.Close()
		maskData, err := io.ReadAll(mask)
		if err != nil {
			return nil, fmt.Errorf("read mask: %s", err)
		}
		if len(maskData) > 0 {
			req.MaskB64 = base64.StdEncoding.EncodeToString(maskData)
		}
		_ = maskHeader
	}
	return req, nil
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
func (h *Handler) imageEdits(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	req, err := parseImageEditRequest(r, true)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}
	if deny := authorizeModel(t, req.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.msg, deny.param)
		return
	}
	resp, err := h.router.GenerateImage(r.Context(), req, t)
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
func (h *Handler) imageVariations(w http.ResponseWriter, r *http.Request, t *models.RouterToken) {
	start := time.Now()
	req, err := parseImageEditRequest(r, false)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error(), nil)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		req.Prompt = "Generate a variation of the input image."
	}
	if deny := authorizeModel(t, req.Model); deny != nil {
		h.writeError(w, deny.status, deny.code, deny.msg, deny.param)
		return
	}
	resp, err := h.router.GenerateImage(r.Context(), req, t)
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
