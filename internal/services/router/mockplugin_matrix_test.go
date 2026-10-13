package router

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	"github.com/TheSlopMachine/llm-router/internal/testutil/mockplugin"
)

// setupLuaRouter wires a router backed by one Lua fixture: the plugin is
// installed, a provider instance serves its type key, and one credential is
// stored. It returns the router, the credential and model info services for
// probes and cache warming, and the provider ID for model id building.
func setupLuaRouter(t *testing.T, fixture mockplugin.Fixture) (*Service, *credential.GormService, *modelinfo.GormService, string) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()

	luaService, err := luaplugin.New(database, nil)
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
	routerService := New(providerService, credentialService, modelInfoService, videojobs.New(database), nil, nil, slog.Default())

	return routerService, credentialService, modelInfoService, instance.ID
}

func chatRequest(model string) *models.ChatCompletionRequest {
	return &models.ChatCompletionRequest{
		Model:    models.ModelId(model),
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}
}

func TestMockPluginChat_Complete(t *testing.T) {
	service, _, _, providerID := setupLuaRouter(t, mockplugin.Chat())

	response, err := service.Complete(context.Background(), chatRequest(providerID+"/chat"), nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if len(response.Choices) != 1 || response.Choices[0].Message.Content != "mockplugin ok" {
		t.Fatalf("content: %+v", response.Choices)
	}
}

func TestMockPluginTools_Complete(t *testing.T) {
	service, _, _, providerID := setupLuaRouter(t, mockplugin.Tools())

	request := chatRequest(providerID + "/tools")
	request.Tools = []models.ChatTool{{Type: "function", Function: &models.ChatToolFunction{Name: "get_weather"}}}
	response, err := service.Complete(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if len(response.Choices) != 1 {
		t.Fatalf("choices: %+v", response.Choices)
	}
	choice := response.Choices[0]
	if choice.FinishReason != "tool_calls" {
		t.Fatalf("finish reason: got %q, want tool_calls", choice.FinishReason)
	}
	if len(choice.Message.ToolCalls) != 1 || choice.Message.ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("tool calls: %+v", choice.Message.ToolCalls)
	}
}

func TestMockPluginLimited_QuotaSurfaces(t *testing.T) {
	service, _, _, providerID := setupLuaRouter(t, mockplugin.Limited())

	_, err := service.Complete(context.Background(), chatRequest(providerID+"/limited"), nil)
	var providerError *models.ProviderError
	if !errors.As(err, &providerError) {
		t.Fatalf("expected ProviderError, got %T (%v)", err, err)
	}
	if providerError.Code != "insufficient_quota" || providerError.StatusCode != 429 {
		t.Fatalf("quota error: %+v", providerError)
	}
}

func TestMockPluginDenied_AuthSurfaces(t *testing.T) {
	service, _, _, providerID := setupLuaRouter(t, mockplugin.Denied())

	_, err := service.Complete(context.Background(), chatRequest(providerID+"/denied"), nil)
	var providerError *models.ProviderError
	if !errors.As(err, &providerError) {
		t.Fatalf("expected ProviderError, got %T (%v)", err, err)
	}
	if providerError.Code != "authentication_error" || providerError.StatusCode != 401 {
		t.Fatalf("auth error: %+v", providerError)
	}
}

func TestMockPluginStream_CompleteStream(t *testing.T) {
	service, _, _, providerID := setupLuaRouter(t, mockplugin.Stream())

	var output strings.Builder
	if err := service.CompleteStream(context.Background(), chatRequest(providerID+"/stream"), &output, nil); err != nil {
		t.Fatalf("stream failed: %v", err)
	}
	body := output.String()
	if !strings.Contains(body, "mockplugin") {
		t.Fatalf("stream must carry chunk text: %q", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("stream must terminate with [DONE]: %q", body)
	}
}

func TestMockPluginTranscribe_Success(t *testing.T) {
	service, _, _, providerID := setupLuaRouter(t, mockplugin.Transcribe())

	response, err := service.Transcribe(context.Background(), &models.TranscriptionRequest{
		Model:       models.ModelId(providerID + "/transcribe"),
		File:        []byte("audio-bytes"),
		FileName:    "clip.wav",
		ContentType: "audio/wav",
	}, nil)
	if err != nil {
		t.Fatalf("transcribe failed: %v", err)
	}
	if response.Text != "mockplugin transcript" {
		t.Fatalf("text: got %q", response.Text)
	}
	if len(response.Segments) != 1 {
		t.Fatalf("segments: %+v", response.Segments)
	}
}

func TestMockPluginSpeech_Success(t *testing.T) {
	service, _, _, providerID := setupLuaRouter(t, mockplugin.Speech())

	response, err := service.Speech(context.Background(), &models.SpeechRequest{
		Model: models.ModelId(providerID + "/speech"),
		Input: "hi",
	}, nil)
	if err != nil {
		t.Fatalf("speech failed: %v", err)
	}
	if string(response.Audio) != "FAKEWAV" {
		t.Fatalf("audio: got %q", response.Audio)
	}
	if response.Format != "wav" {
		t.Fatalf("format: got %q", response.Format)
	}
}

func TestMockPluginImage_URL(t *testing.T) {
	service, _, _, providerID := setupLuaRouter(t, mockplugin.Image())

	response, err := service.GenerateImage(context.Background(), &models.ImageGenerationRequest{
		Model:          models.ModelId(providerID + "/image"),
		Prompt:         "a cat",
		ResponseFormat: "url",
	}, nil)
	if err != nil {
		t.Fatalf("generate image failed: %v", err)
	}
	if len(response.Data) != 1 || response.Data[0].URL != "http://example.com/mockplugin.png" {
		t.Fatalf("data: %+v", response.Data)
	}
}

func TestMockPluginImage_Base64(t *testing.T) {
	service, _, _, providerID := setupLuaRouter(t, mockplugin.Image())

	response, err := service.GenerateImage(context.Background(), &models.ImageGenerationRequest{
		Model:          models.ModelId(providerID + "/image"),
		Prompt:         "a cat",
		ResponseFormat: "b64_json",
	}, nil)
	if err != nil {
		t.Fatalf("generate image failed: %v", err)
	}
	if len(response.Data) != 1 || response.Data[0].B64JSON != "aGk=" {
		t.Fatalf("data: %+v", response.Data)
	}
}

func TestMockPluginEmbeddings_Vectors(t *testing.T) {
	service, _, _, providerID := setupLuaRouter(t, mockplugin.Embeddings())

	response, err := service.Embed(context.Background(), &models.EmbeddingsRequest{
		Model: models.ModelId(providerID + "/embeddings"),
		Input: []string{"hi", "there"},
	}, nil)
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	if len(response.Data) != 2 {
		t.Fatalf("data len: %d", len(response.Data))
	}
	for i, entry := range response.Data {
		if entry.Index != i || len(entry.Values) != 3 || entry.Values[0] != 0.1 {
			t.Fatalf("entry %d: %+v", i, entry)
		}
	}
}

func TestMockPluginVideo_CompletedSubmit(t *testing.T) {
	service, _, _, providerID := setupLuaRouter(t, mockplugin.Video())

	submission, err := service.SubmitVideo(context.Background(), &models.VideoGenerationRequest{
		Model:  models.ModelId(providerID + "/video"),
		Prompt: "a cat",
	}, nil)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if !strings.HasPrefix(submission.ID, "gen-vid-") {
		t.Fatalf("submit must mint a local id, got %q", submission.ID)
	}
	if submission.PollingURL != "/v1/videos/"+submission.ID {
		t.Fatalf("polling url: %q", submission.PollingURL)
	}

	polled, err := service.PollVideo(context.Background(), submission.ID, nil)
	if err != nil {
		t.Fatalf("poll failed: %v", err)
	}
	if polled.ID != submission.ID || polled.Status != models.VideoStatusCompleted {
		t.Fatalf("poll: %+v", polled)
	}
	if len(polled.UnsignedURLs) != 1 {
		t.Fatalf("unsigned urls: %+v", polled.UnsignedURLs)
	}

	content, err := service.VideoContent(context.Background(), submission.ID, 0, nil)
	if err != nil {
		t.Fatalf("content failed: %v", err)
	}
	if string(content.Video[4:8]) != "ftyp" || content.ContentType != "video/mp4" {
		t.Fatalf("content: type=%q bytes=%q", content.ContentType, content.Video)
	}
}

func TestMockPluginVideo_PendingSubmit(t *testing.T) {
	service, _, _, providerID := setupLuaRouter(t, mockplugin.VideoPending())

	submission, err := service.SubmitVideo(context.Background(), &models.VideoGenerationRequest{
		Model:  models.ModelId(providerID + "/video"),
		Prompt: "a cat",
	}, nil)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if submission.Status != models.VideoStatusPending {
		t.Fatalf("submit status: got %q, want pending", submission.Status)
	}

	polled, err := service.PollVideo(context.Background(), submission.ID, nil)
	if err != nil {
		t.Fatalf("poll failed: %v", err)
	}
	if polled.Status != models.VideoStatusCompleted {
		t.Fatalf("poll status: got %q, want completed", polled.Status)
	}

	content, err := service.VideoContent(context.Background(), submission.ID, 0, nil)
	if err != nil {
		t.Fatalf("content failed: %v", err)
	}
	if string(content.Video[4:8]) != "ftyp" || content.ContentType != "video/mp4" {
		t.Fatalf("content: type=%q bytes=%q", content.ContentType, content.Video)
	}
}

func TestMockPluginEndpointGate_TranscribeOnChat(t *testing.T) {
	service, _, modelInfoService, providerID := setupLuaRouter(t, mockplugin.ChatOnly())
	if _, err := modelInfoService.GetModelInfos(context.Background(), providerID); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	_, err := service.Transcribe(context.Background(), &models.TranscriptionRequest{
		Model:       models.ModelId(providerID + "/chat"),
		File:        []byte("audio-bytes"),
		FileName:    "clip.wav",
		ContentType: "audio/wav",
	}, nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestMockPluginEndpointGate_VideoOnChat(t *testing.T) {
	service, _, modelInfoService, providerID := setupLuaRouter(t, mockplugin.ChatOnly())
	if _, err := modelInfoService.GetModelInfos(context.Background(), providerID); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	_, err := service.SubmitVideo(context.Background(), &models.VideoGenerationRequest{
		Model:  models.ModelId(providerID + "/chat"),
		Prompt: "a cat",
	}, nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestMockPluginProbe_TestModel(t *testing.T) {
	service, _, _, providerID := setupLuaRouter(t, mockplugin.Chat())

	result := service.TestModel(context.Background(), models.ModelId(providerID+"/chat"))
	if !result.OK {
		t.Fatalf("probe failed: %v", result.Error)
	}
}
