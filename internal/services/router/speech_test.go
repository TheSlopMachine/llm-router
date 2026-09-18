package router

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// speechMockAdapter extends the router test mock with Speech and GenerateImage.
type speechMockAdapter struct {
	mockAdapter
	infos      []models.ModelInfo
	speechFunc func(context.Context, *models.Credential, *models.SpeechRequest) (*models.SpeechResponse, error)
	imageFunc  func(context.Context, *models.Credential, *models.ImageGenerationRequest) (*models.ImageGenerationResponse, error)
}

func (m *speechMockAdapter) GetModelInfos(ctx context.Context, cred *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	return m.infos, nil
}

func (m *speechMockAdapter) Speech(ctx context.Context, cred *models.Credential, req *models.SpeechRequest, _ map[string]any) (*models.SpeechResponse, error) {
	if m.speechFunc != nil {
		return m.speechFunc(ctx, cred, req)
	}
	return &models.SpeechResponse{Audio: []byte("AUDIO"), Format: "mp3"}, nil
}

func (m *speechMockAdapter) GenerateImage(ctx context.Context, cred *models.Credential, req *models.ImageGenerationRequest, _ map[string]any) (*models.ImageGenerationResponse, error) {
	if m.imageFunc != nil {
		return m.imageFunc(ctx, cred, req)
	}
	return &models.ImageGenerationResponse{
		Created: 1700000001,
		Data:    []models.ImageData{{B64JSON: "aW1n"}},
	}, nil
}

func setupSpeechRouter(t *testing.T, maxRetries int) (*Service, *credential.Service, *modelinfo.Service, *speechMockAdapter) {
	t.Helper()
	database := testutil.SetupTestDB(t)

	providerSvc := provider.NewService(database)
	mock := &speechMockAdapter{mockAdapter: mockAdapter{callCount: new(int)}}
	providerSvc.RegisterGoAdapter(mock)
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)

	return New(providerSvc, credSvc, modelInfoSvc, maxRetries, slog.Default()), credSvc, modelInfoSvc, mock
}

func speechReq(model string) *models.SpeechRequest {
	return &models.SpeechRequest{Model: models.ModelId(model), Input: "hello", Voice: "Kore"}
}

func imageReq(model string) *models.ImageGenerationRequest {
	return &models.ImageGenerationRequest{Model: models.ModelId(model), Prompt: "a cat", N: 1}
}

func TestRouterService_Speech_Success(t *testing.T) {
	svc, credSvc, _, _ := setupSpeechRouter(t, 3)
	addTranscribeCred(t, credSvc, "Cred 1")

	resp, err := svc.Speech(context.Background(), speechReq("mock/gemini-tts"), nil)
	if err != nil {
		t.Fatalf("speech failed: %v", err)
	}
	if string(resp.Audio) != "AUDIO" || resp.Format != "mp3" {
		t.Fatalf("response: %+v", resp)
	}
}

func TestRouterService_Speech_UnsupportedAdapter(t *testing.T) {
	svc, credSvc, _ := setupRouterService(t, 3)
	addTranscribeCred(t, credSvc, "Cred 1")

	_, err := svc.Speech(context.Background(), speechReq("mock/test-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Speech_ModelGateRejects(t *testing.T) {
	svc, credSvc, modelInfoSvc, mock := setupSpeechRouter(t, 3)
	addTranscribeCred(t, credSvc, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "chat-model", DisplayName: "Chat", Endpoints: []string{models.EndpointChatCompletions}},
	}
	if _, err := modelInfoSvc.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	_, err := svc.Speech(context.Background(), speechReq("mock/chat-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Speech_ModelGateAllowsAndBlocksChat(t *testing.T) {
	svc, credSvc, modelInfoSvc, mock := setupSpeechRouter(t, 3)
	addTranscribeCred(t, credSvc, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "tts-model", DisplayName: "TTS", Endpoints: []string{models.EndpointAudioSpeech}},
	}
	if _, err := modelInfoSvc.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	if _, err := svc.Speech(context.Background(), speechReq("mock/tts-model"), nil); err != nil {
		t.Fatalf("speech failed: %v", err)
	}

	_, err := svc.Complete(context.Background(), &models.ChatCompletionRequest{
		Model:    "mock/tts-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported for chat on tts model, got %v", err)
	}
}

func TestRouterService_Speech_RateLimitRotates(t *testing.T) {
	svc, credSvc, _, mock := setupSpeechRouter(t, 3)
	addTranscribeCred(t, credSvc, "Cred 1")
	addTranscribeCred(t, credSvc, "Cred 2")

	firstCall := true
	mock.speechFunc = func(ctx context.Context, cred *models.Credential, req *models.SpeechRequest) (*models.SpeechResponse, error) {
		if firstCall {
			firstCall = false
			resetAt := time.Now().Add(60 * time.Second)
			return nil, &models.ProviderError{
				StatusCode: 429,
				Message:    "rate limit exceeded",
				Type:       models.ErrorTypeRateLimit,
				RetryAfter: &resetAt,
			}
		}
		return &models.SpeechResponse{Audio: []byte("SECOND"), Format: "mp3"}, nil
	}

	resp, err := svc.Speech(context.Background(), speechReq("mock/gemini-tts"), nil)
	if err != nil {
		t.Fatalf("speech failed: %v", err)
	}
	if string(resp.Audio) != "SECOND" {
		t.Fatalf("audio: got %q", resp.Audio)
	}
}

func TestRouterService_GenerateImage_Success(t *testing.T) {
	svc, credSvc, _, _ := setupSpeechRouter(t, 3)
	addTranscribeCred(t, credSvc, "Cred 1")

	resp, err := svc.GenerateImage(context.Background(), imageReq("mock/imagen-3"), nil)
	if err != nil {
		t.Fatalf("generate image failed: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].B64JSON != "aW1n" {
		t.Fatalf("response: %+v", resp)
	}
}

func TestRouterService_GenerateImage_UnsupportedAdapter(t *testing.T) {
	svc, credSvc, _ := setupRouterService(t, 3)
	addTranscribeCred(t, credSvc, "Cred 1")

	_, err := svc.GenerateImage(context.Background(), imageReq("mock/test-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_GenerateImage_ModelGateRejects(t *testing.T) {
	svc, credSvc, modelInfoSvc, mock := setupSpeechRouter(t, 3)
	addTranscribeCred(t, credSvc, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "chat-model", DisplayName: "Chat", Endpoints: []string{models.EndpointChatCompletions}},
	}
	if _, err := modelInfoSvc.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	_, err := svc.GenerateImage(context.Background(), imageReq("mock/chat-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_GenerateImage_ModelGateAllows(t *testing.T) {
	svc, credSvc, modelInfoSvc, mock := setupSpeechRouter(t, 3)
	addTranscribeCred(t, credSvc, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "image-model", DisplayName: "Image", Endpoints: []string{models.EndpointImagesGenerations}},
	}
	if _, err := modelInfoSvc.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	resp, err := svc.GenerateImage(context.Background(), imageReq("mock/image-model"), nil)
	if err != nil {
		t.Fatalf("generate image failed: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("data len: %d", len(resp.Data))
	}
}

// embedMockAdapter extends the router test mock with Embed.
type embedMockAdapter struct {
	mockAdapter
	infos     []models.ModelInfo
	embedFunc func(context.Context, *models.Credential, *models.EmbeddingsRequest) (*models.EmbeddingsResponse, error)
}

func (m *embedMockAdapter) GetModelInfos(ctx context.Context, cred *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	return m.infos, nil
}

func (m *embedMockAdapter) Embed(ctx context.Context, cred *models.Credential, req *models.EmbeddingsRequest, _ map[string]any) (*models.EmbeddingsResponse, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, cred, req)
	}
	data := make([]models.Embedding, len(req.Input))
	for i := range req.Input {
		data[i] = models.Embedding{Index: i, Values: []float64{0.1, 0.2}}
	}
	return &models.EmbeddingsResponse{Data: data}, nil
}

func setupEmbedRouter(t *testing.T, maxRetries int) (*Service, *credential.Service, *modelinfo.Service, *embedMockAdapter) {
	t.Helper()
	database := testutil.SetupTestDB(t)

	providerSvc := provider.NewService(database)
	mock := &embedMockAdapter{mockAdapter: mockAdapter{callCount: new(int)}}
	providerSvc.RegisterGoAdapter(mock)
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)

	return New(providerSvc, credSvc, modelInfoSvc, maxRetries, slog.Default()), credSvc, modelInfoSvc, mock
}

func embedReq(model string) *models.EmbeddingsRequest {
	return &models.EmbeddingsRequest{Model: models.ModelId(model), Input: []string{"hello", "world"}}
}

func TestRouterService_Embed_Success(t *testing.T) {
	svc, credSvc, _, _ := setupEmbedRouter(t, 3)
	addTranscribeCred(t, credSvc, "Cred 1")

	resp, err := svc.Embed(context.Background(), embedReq("mock/gemini-embedding-001"), nil)
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	if len(resp.Data) != 2 || len(resp.Data[0].Values) != 2 {
		t.Fatalf("response: %+v", resp)
	}
}

func TestRouterService_Embed_UnsupportedAdapter(t *testing.T) {
	svc, credSvc, _ := setupRouterService(t, 3)
	addTranscribeCred(t, credSvc, "Cred 1")

	_, err := svc.Embed(context.Background(), embedReq("mock/test-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Embed_ModelGateRejects(t *testing.T) {
	svc, credSvc, modelInfoSvc, mock := setupEmbedRouter(t, 3)
	addTranscribeCred(t, credSvc, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "chat-model", DisplayName: "Chat", Endpoints: []string{models.EndpointChatCompletions}},
	}
	if _, err := modelInfoSvc.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	_, err := svc.Embed(context.Background(), embedReq("mock/chat-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Embed_ModelGateAllowsAndBlocksChat(t *testing.T) {
	svc, credSvc, modelInfoSvc, mock := setupEmbedRouter(t, 3)
	addTranscribeCred(t, credSvc, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "emb-model", DisplayName: "Emb", Endpoints: []string{models.EndpointEmbeddings}},
	}
	if _, err := modelInfoSvc.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	if _, err := svc.Embed(context.Background(), embedReq("mock/emb-model"), nil); err != nil {
		t.Fatalf("embed failed: %v", err)
	}

	_, err := svc.Complete(context.Background(), &models.ChatCompletionRequest{
		Model:    "mock/emb-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported for chat on embedding model, got %v", err)
	}
}

func TestRouterService_Embed_RateLimitRotates(t *testing.T) {
	svc, credSvc, _, mock := setupEmbedRouter(t, 3)
	addTranscribeCred(t, credSvc, "Cred 1")
	addTranscribeCred(t, credSvc, "Cred 2")

	firstCall := true
	mock.embedFunc = func(ctx context.Context, cred *models.Credential, req *models.EmbeddingsRequest) (*models.EmbeddingsResponse, error) {
		if firstCall {
			firstCall = false
			resetAt := time.Now().Add(60 * time.Second)
			return nil, &models.ProviderError{
				StatusCode: 429,
				Message:    "rate limit exceeded",
				Type:       models.ErrorTypeRateLimit,
				RetryAfter: &resetAt,
			}
		}
		return &models.EmbeddingsResponse{
			Data: []models.Embedding{{Index: 0, Values: []float64{9.9}}, {Index: 1, Values: []float64{8.8}}},
		}, nil
	}

	resp, err := svc.Embed(context.Background(), embedReq("mock/gemini-embedding-001"), nil)
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	if resp.Data[0].Values[0] != 9.9 {
		t.Fatalf("expected rotation to second credential, got %+v", resp.Data[0])
	}
}
