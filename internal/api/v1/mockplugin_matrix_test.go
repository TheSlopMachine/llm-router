package v1

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/metrics"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	"github.com/TheSlopMachine/llm-router/internal/testutil/mockplugin"
)

// setupLuaHandler wires a v1 Handler backed by one Lua fixture: the plugin
// is installed, a provider instance serves its type key, and one credential
// is stored. Handler methods are called directly with a nil token
// (unrestricted, mirroring the admin listener).
func setupLuaHandler(t *testing.T, fixture mockplugin.Fixture) (*Handler, string, *modelinfo.GormService) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()
	logger := slog.Default()

	luaService, err := luaplugin.New(database, logger)
	if err != nil {
		t.Fatalf("new plugin service: %v", err)
	}
	providerService := provider.NewGorm(database)
	providerService.SetLuaService(luaService)
	if _, err := luaService.InstallSQLite(ctx, []byte(fixture.Source), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install %s: %v", fixture.TypeKey, err)
	}
	instance, err := providerService.Create(ctx, provider.CreateOptions{Name: "MockPlugin", TypeKey: fixture.TypeKey})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	credentialService := credential.NewGorm(database, providerService)
	if _, err := credentialService.Add(ctx, credential.AddOptions{
		ProviderID: instance.ID,
		Label:      "Mock",
		Data:       map[string]any{"api_key": "test-key"},
	}); err != nil {
		t.Fatalf("add credential: %v", err)
	}
	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)
	metricsService := metrics.New(database, logger)
	routerService := router.New(providerService, credentialService, modelInfoService, videojobs.New(database), nil, nil, logger)

	return &Handler{
		router:       routerService,
		metrics:      metricsService,
		providerSvc:  providerService,
		modelInfoSvc: modelInfoService,
		logger:       logger,
	}, instance.ID, modelInfoService
}

func serveJSON(t *testing.T, handler func(w *httptest.ResponseRecorder, r *http.Request), path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest("POST", path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func TestMockPluginV1Chat_Completions(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Chat())

	recorder := serveJSON(t, func(w *httptest.ResponseRecorder, r *http.Request) {
		handler.chatCompletions(w, r, nil)
	}, "/v1/chat/completions", `{"model":"`+providerID+`/chat","messages":[{"role":"user","content":"hi"}]}`)
	if recorder.Code != 200 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	var response models.ChatCompletionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(response.Choices) != 1 || response.Choices[0].Message.Content != "mockplugin ok" {
		t.Fatalf("content: %+v", response.Choices)
	}
}

func TestMockPluginV1Chat_CompletionsStream(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Stream())

	recorder := serveJSON(t, func(w *httptest.ResponseRecorder, r *http.Request) {
		handler.chatCompletions(w, r, nil)
	}, "/v1/chat/completions", `{"model":"`+providerID+`/stream","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if recorder.Code != 200 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "data: [DONE]") {
		t.Fatalf("stream without [DONE]: %q", recorder.Body.String())
	}
}

func TestMockPluginV1Chat_QuotaError(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Limited())

	recorder := serveJSON(t, func(w *httptest.ResponseRecorder, r *http.Request) {
		handler.chatCompletions(w, r, nil)
	}, "/v1/chat/completions", `{"model":"`+providerID+`/limited","messages":[{"role":"user","content":"hi"}]}`)
	if recorder.Code != 429 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	var response models.OpenAIError
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.Error.Code != "insufficient_quota" {
		t.Fatalf("code: %+v", response.Error)
	}
}

func TestMockPluginV1Messages_Unary(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Chat())

	recorder := serveJSON(t, func(w *httptest.ResponseRecorder, r *http.Request) {
		handler.anthropicMessages(w, r, nil)
	}, "/v1/messages", `{"model":"`+providerID+`/chat","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`)
	if recorder.Code != 200 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "mockplugin ok") {
		t.Fatalf("content missing: %q", recorder.Body.String())
	}
}

func TestMockPluginV1Messages_Stream(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Stream())

	recorder := serveJSON(t, func(w *httptest.ResponseRecorder, r *http.Request) {
		handler.anthropicMessages(w, r, nil)
	}, "/v1/messages", `{"model":"`+providerID+`/stream","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if recorder.Code != 200 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "message_stop") {
		t.Fatalf("stream without message_stop: %q", recorder.Body.String())
	}
}

func TestMockPluginV1Messages_Tools(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Tools())

	recorder := serveJSON(t, func(w *httptest.ResponseRecorder, r *http.Request) {
		handler.anthropicMessages(w, r, nil)
	}, "/v1/messages", `{"model":"`+providerID+`/tools","max_tokens":64,`+
		`"messages":[{"role":"user","content":"hi"}],`+
		`"tools":[{"name":"get_weather","description":"Get weather","input_schema":{"type":"object"}}],`+
		`"tool_choice":{"type":"any"}}`)
	if recorder.Code != 200 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "tool_use") {
		t.Fatalf("no tool_use block: %q", recorder.Body.String())
	}
}

func TestMockPluginV1Transcribe_JSON(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Transcribe())

	contentType, buffer := multipartBody(t, map[string]string{
		"model": providerID + "/transcribe",
	}, true)
	request := httptest.NewRequest("POST", "/v1/audio/transcriptions", buffer)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	handler.audioTranscriptions(recorder, request, nil)
	if recorder.Code != 200 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.Text != "mockplugin transcript" {
		t.Fatalf("text: %q", response.Text)
	}
}

func TestMockPluginV1Transcribe_SRT(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Transcribe())

	contentType, buffer := multipartBody(t, map[string]string{
		"model":           providerID + "/transcribe",
		"response_format": "srt",
	}, true)
	request := httptest.NewRequest("POST", "/v1/audio/transcriptions", buffer)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	handler.audioTranscriptions(recorder, request, nil)
	if recorder.Code != 200 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "-->") {
		t.Fatalf("srt without cue timing: %q", recorder.Body.String())
	}
}

func TestMockPluginV1Speech_Success(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Speech())

	recorder := serveJSON(t, func(w *httptest.ResponseRecorder, r *http.Request) {
		handler.audioSpeech(w, r, nil)
	}, "/v1/audio/speech", `{"model":"`+providerID+`/speech","input":"hi"}`)
	if recorder.Code != 200 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.Len() == 0 {
		t.Fatal("empty audio")
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "audio/") {
		t.Fatalf("content type: %q", contentType)
	}
}

func TestMockPluginV1Image_URL(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Image())

	recorder := serveJSON(t, func(w *httptest.ResponseRecorder, r *http.Request) {
		handler.imageGenerations(w, r, nil)
	}, "/v1/images/generations", `{"model":"`+providerID+`/image","prompt":"a cat","response_format":"url"}`)
	if recorder.Code != 200 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	var response models.ImageGenerationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(response.Data) != 1 || response.Data[0].URL != "http://example.com/mockplugin.png" {
		t.Fatalf("data: %+v", response.Data)
	}
}

func TestMockPluginV1Image_Base64(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Image())

	recorder := serveJSON(t, func(w *httptest.ResponseRecorder, r *http.Request) {
		handler.imageGenerations(w, r, nil)
	}, "/v1/images/generations", `{"model":"`+providerID+`/image","prompt":"a cat","response_format":"b64_json"}`)
	if recorder.Code != 200 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	var response models.ImageGenerationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(response.Data) != 1 || response.Data[0].B64JSON != "aGk=" {
		t.Fatalf("data: %+v", response.Data)
	}
}

func TestMockPluginV1Embeddings_Float(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Embeddings())

	recorder := serveJSON(t, func(w *httptest.ResponseRecorder, r *http.Request) {
		handler.embeddings(w, r, nil)
	}, "/v1/embeddings", `{"model":"`+providerID+`/embeddings","input":["hi"]}`)
	if recorder.Code != 200 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	var response models.EmbeddingsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(response.Data) != 1 || len(response.Data[0].Values) != 3 {
		t.Fatalf("data: %+v", response.Data)
	}
}

func TestMockPluginV1Embeddings_Base64(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Embeddings())

	recorder := serveJSON(t, func(w *httptest.ResponseRecorder, r *http.Request) {
		handler.embeddings(w, r, nil)
	}, "/v1/embeddings", `{"model":"`+providerID+`/embeddings","input":["hi"],"encoding_format":"base64"}`)
	if recorder.Code != 200 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"embedding":"`) {
		t.Fatalf("base64 embedding must render as a string: %q", recorder.Body.String())
	}
}

func TestMockPluginV1Video_Pipeline(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Video())

	submit := serveJSON(t, func(w *httptest.ResponseRecorder, r *http.Request) {
		handler.submitVideo(w, r, nil)
	}, "/v1/videos", `{"model":"`+providerID+`/video","prompt":"a cat"}`)
	if submit.Code != 202 {
		t.Fatalf("submit status: got %d, body %q", submit.Code, submit.Body.String())
	}
	var submission struct {
		ID         string `json:"id"`
		PollingURL string `json:"polling_url"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(submit.Body.Bytes(), &submission); err != nil {
		t.Fatalf("decode submit: %v", err)
	}
	if submission.ID == "" || submission.PollingURL == "" {
		t.Fatalf("submit without job id: %+v", submission)
	}

	pollRequest := httptest.NewRequest("GET", "/v1/videos/"+submission.ID, nil)
	pollRequest.SetPathValue("jobId", submission.ID)
	poll := httptest.NewRecorder()
	handler.pollVideo(poll, pollRequest, nil)
	if poll.Code != 200 {
		t.Fatalf("poll status: got %d, body %q", poll.Code, poll.Body.String())
	}
	var job struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(poll.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode poll: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("job status: got %q, want completed", job.Status)
	}

	contentRequest := httptest.NewRequest("GET", "/v1/videos/"+submission.ID+"/content?index=0", nil)
	contentRequest.SetPathValue("jobId", submission.ID)
	content := httptest.NewRecorder()
	handler.videoContent(content, contentRequest, nil)
	if content.Code != 200 {
		t.Fatalf("content status: got %d, body %q", content.Code, content.Body.String())
	}
	if mediaType := content.Header().Get("Content-Type"); !strings.Contains(mediaType, "video/") {
		t.Fatalf("content type: %q", mediaType)
	}
	if body := content.Body.Bytes(); len(body) < 8 || string(body[4:8]) != "ftyp" {
		t.Fatalf("video bytes without ftyp magic")
	}
}

func TestMockPluginV1EndpointGate_TranscribeOnChat(t *testing.T) {
	handler, providerID, modelInfoService := setupLuaHandler(t, mockplugin.ChatOnly())
	if _, err := modelInfoService.GetModelInfos(context.Background(), providerID); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	contentType, buffer := multipartBody(t, map[string]string{
		"model": providerID + "/chat",
	}, true)
	request := httptest.NewRequest("POST", "/v1/audio/transcriptions", buffer)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	handler.audioTranscriptions(recorder, request, nil)
	if recorder.Code < 400 {
		t.Fatalf("transcribe on chat-only model must not succeed: %d %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "endpoint_not_supported") {
		t.Fatalf("gate must report endpoint_not_supported: %q", recorder.Body.String())
	}
}

func TestMockPluginV1Models_List(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Chat())

	request := httptest.NewRequest("GET", "/v1/models", nil)
	recorder := httptest.NewRecorder()
	handler.listModels(recorder, request, nil)
	if recorder.Code != 200 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	var response models.ModelListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, entry := range response.Data {
		if entry.ID == providerID+"/chat" {
			return
		}
	}
	t.Fatalf("model %s/chat missing from listing: %+v", providerID, response.Data)
}

func TestMockPluginV1VideoModels_List(t *testing.T) {
	handler, providerID, _ := setupLuaHandler(t, mockplugin.Video())

	request := httptest.NewRequest("GET", "/v1/videos/models", nil)
	recorder := httptest.NewRecorder()
	handler.listVideoModels(recorder, request, nil)
	if recorder.Code != 200 {
		t.Fatalf("status: got %d, body %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), providerID+"/video") {
		t.Fatalf("video model missing from listing: %q", recorder.Body.String())
	}
}
