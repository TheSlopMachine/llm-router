package router

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// speechMockAdapter extends the shared test mock with Speech and GenerateImage.
type speechMockAdapter struct {
	*testutil.MockAdapter
	infos      []models.ModelInfo
	speechFunc func(context.Context, []*models.Credential, *models.SpeechRequest) (*models.SpeechResponse, error)
	imageFunc  func(context.Context, []*models.Credential, *models.ImageGenerationRequest) (*models.ImageGenerationResponse, error)
}

func (m *speechMockAdapter) GetModelInfos(ctx context.Context, credential *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	return m.infos, nil
}

func (m *speechMockAdapter) Speech(ctx context.Context, creds []*models.Credential, request *models.SpeechRequest, _ map[string]any) (*models.SpeechResponse, error) {
	if m.speechFunc != nil {
		return m.speechFunc(ctx, creds, request)
	}
	return &models.SpeechResponse{Audio: []byte("AUDIO"), Format: "mp3"}, nil
}

func (m *speechMockAdapter) GenerateImage(ctx context.Context, creds []*models.Credential, request *models.ImageGenerationRequest, _ map[string]any) (*models.ImageGenerationResponse, error) {
	if m.imageFunc != nil {
		return m.imageFunc(ctx, creds, request)
	}
	return &models.ImageGenerationResponse{
		Created: 1700000001,
		Data:    []models.ImageData{{B64JSON: "aW1n"}},
	}, nil
}

func setupSpeechRouter(t *testing.T) (*Service, *credential.GormService, *modelinfo.GormService, *speechMockAdapter) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()

	providerService := provider.NewGorm(database)
	mock := &speechMockAdapter{MockAdapter: testutil.NewMockAdapter("mock")}
	providerService.RegisterGoAdapter(mock)
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credentialService := credential.NewGorm(database, providerService)
	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)

	return New(providerService, credentialService, modelInfoService, videojobs.New(database), nil, nil, slog.Default()), credentialService, modelInfoService, mock
}

func speechRequest(model string) *models.SpeechRequest {
	return &models.SpeechRequest{Model: models.ModelId(model), Input: "hello", Voice: "Kore"}
}

func imageRequest(model string) *models.ImageGenerationRequest {
	return &models.ImageGenerationRequest{Model: models.ModelId(model), Prompt: "a cat", N: 1}
}

func TestRouterService_Speech_Success(t *testing.T) {
	service, credentialService, _, _ := setupSpeechRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")

	response, err := service.Speech(context.Background(), speechRequest("mock/gemini-tts"), nil)
	if err != nil {
		t.Fatalf("speech failed: %v", err)
	}
	if string(response.Audio) != "AUDIO" || response.Format != "mp3" {
		t.Fatalf("response: %+v", response)
	}
}

func TestRouterService_Speech_UnsupportedAdapter(t *testing.T) {
	service, credentialService, _, _ := setupRouterService(t)
	addTranscribeCred(t, credentialService, "Cred 1")

	_, err := service.Speech(context.Background(), speechRequest("mock/test-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Speech_ModelGateRejects(t *testing.T) {
	service, credentialService, modelInfoService, mock := setupSpeechRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "chat-model", DisplayName: "Chat", Endpoints: []string{models.EndpointChatCompletions}},
	}
	if _, err := modelInfoService.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	_, err := service.Speech(context.Background(), speechRequest("mock/chat-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Speech_ModelGateAllowsAndBlocksChat(t *testing.T) {
	service, credentialService, modelInfoService, mock := setupSpeechRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "tts-model", DisplayName: "TTS", Endpoints: []string{models.EndpointAudioSpeech}},
	}
	if _, err := modelInfoService.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	if _, err := service.Speech(context.Background(), speechRequest("mock/tts-model"), nil); err != nil {
		t.Fatalf("speech failed: %v", err)
	}

	_, err := service.Complete(context.Background(), &models.ChatCompletionRequest{
		Model:    "mock/tts-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported for chat on tts model, got %v", err)
	}
}

func TestRouterService_Speech_PassesPoolToBackend(t *testing.T) {
	service, credentialService, _, mock := setupSpeechRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")
	addTranscribeCred(t, credentialService, "Cred 2")

	mock.speechFunc = func(ctx context.Context, creds []*models.Credential, request *models.SpeechRequest) (*models.SpeechResponse, error) {
		if len(creds) != 2 {
			return nil, fmt.Errorf("expected 2-credential pool, got %d", len(creds))
		}
		return &models.SpeechResponse{Audio: []byte("SECOND"), Format: "mp3"}, nil
	}

	response, err := service.Speech(context.Background(), speechRequest("mock/gemini-tts"), nil)
	if err != nil {
		t.Fatalf("speech failed: %v", err)
	}
	if string(response.Audio) != "SECOND" {
		t.Fatalf("audio: got %q", response.Audio)
	}
}

func TestRouterService_GenerateImage_Success(t *testing.T) {
	service, credentialService, _, _ := setupSpeechRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")

	response, err := service.GenerateImage(context.Background(), imageRequest("mock/imagen-3"), nil)
	if err != nil {
		t.Fatalf("generate image failed: %v", err)
	}
	if len(response.Data) != 1 || response.Data[0].B64JSON != "aW1n" {
		t.Fatalf("response: %+v", response)
	}
}

func TestRouterService_GenerateImage_UnsupportedAdapter(t *testing.T) {
	service, credentialService, _, _ := setupRouterService(t)
	addTranscribeCred(t, credentialService, "Cred 1")

	_, err := service.GenerateImage(context.Background(), imageRequest("mock/test-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_GenerateImage_ModelGateRejects(t *testing.T) {
	service, credentialService, modelInfoService, mock := setupSpeechRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "chat-model", DisplayName: "Chat", Endpoints: []string{models.EndpointChatCompletions}},
	}
	if _, err := modelInfoService.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	_, err := service.GenerateImage(context.Background(), imageRequest("mock/chat-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_GenerateImage_ModelGateAllows(t *testing.T) {
	service, credentialService, modelInfoService, mock := setupSpeechRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "image-model", DisplayName: "Image", Endpoints: []string{models.EndpointImagesGenerations}},
	}
	if _, err := modelInfoService.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	response, err := service.GenerateImage(context.Background(), imageRequest("mock/image-model"), nil)
	if err != nil {
		t.Fatalf("generate image failed: %v", err)
	}
	if len(response.Data) != 1 {
		t.Fatalf("data len: %d", len(response.Data))
	}
}

// embedMockAdapter extends the shared test mock with Embed.
type embedMockAdapter struct {
	*testutil.MockAdapter
	infos     []models.ModelInfo
	embedFunc func(context.Context, []*models.Credential, *models.EmbeddingsRequest) (*models.EmbeddingsResponse, error)
}

func (m *embedMockAdapter) GetModelInfos(ctx context.Context, credential *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	return m.infos, nil
}

func (m *embedMockAdapter) Embed(ctx context.Context, creds []*models.Credential, request *models.EmbeddingsRequest, _ map[string]any) (*models.EmbeddingsResponse, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, creds, request)
	}
	data := make([]models.Embedding, len(request.Input))
	for i := range request.Input {
		data[i] = models.Embedding{Index: i, Values: []float64{0.1, 0.2}}
	}
	return &models.EmbeddingsResponse{Data: data}, nil
}

func setupEmbedRouter(t *testing.T) (*Service, *credential.GormService, *modelinfo.GormService, *embedMockAdapter) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()

	providerService := provider.NewGorm(database)
	mock := &embedMockAdapter{MockAdapter: testutil.NewMockAdapter("mock")}
	providerService.RegisterGoAdapter(mock)
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credentialService := credential.NewGorm(database, providerService)
	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)

	return New(providerService, credentialService, modelInfoService, videojobs.New(database), nil, nil, slog.Default()), credentialService, modelInfoService, mock
}

func embedRequest(model string) *models.EmbeddingsRequest {
	return &models.EmbeddingsRequest{Model: models.ModelId(model), Input: []string{"hello", "world"}}
}

func TestRouterService_Embed_Success(t *testing.T) {
	service, credentialService, _, _ := setupEmbedRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")

	response, err := service.Embed(context.Background(), embedRequest("mock/gemini-embedding-001"), nil)
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	if len(response.Data) != 2 || len(response.Data[0].Values) != 2 {
		t.Fatalf("response: %+v", response)
	}
}

func TestRouterService_Embed_UnsupportedAdapter(t *testing.T) {
	service, credentialService, _, _ := setupRouterService(t)
	addTranscribeCred(t, credentialService, "Cred 1")

	_, err := service.Embed(context.Background(), embedRequest("mock/test-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Embed_ModelGateRejects(t *testing.T) {
	service, credentialService, modelInfoService, mock := setupEmbedRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "chat-model", DisplayName: "Chat", Endpoints: []string{models.EndpointChatCompletions}},
	}
	if _, err := modelInfoService.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	_, err := service.Embed(context.Background(), embedRequest("mock/chat-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Embed_ModelGateAllowsAndBlocksChat(t *testing.T) {
	service, credentialService, modelInfoService, mock := setupEmbedRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "emb-model", DisplayName: "Emb", Endpoints: []string{models.EndpointEmbeddings}},
	}
	if _, err := modelInfoService.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	if _, err := service.Embed(context.Background(), embedRequest("mock/emb-model"), nil); err != nil {
		t.Fatalf("embed failed: %v", err)
	}

	_, err := service.Complete(context.Background(), &models.ChatCompletionRequest{
		Model:    "mock/emb-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported for chat on embedding model, got %v", err)
	}
}

func TestRouterService_Embed_PassesPoolToBackend(t *testing.T) {
	service, credentialService, _, mock := setupEmbedRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")
	addTranscribeCred(t, credentialService, "Cred 2")

	mock.embedFunc = func(ctx context.Context, creds []*models.Credential, request *models.EmbeddingsRequest) (*models.EmbeddingsResponse, error) {
		if len(creds) != 2 {
			return nil, fmt.Errorf("expected 2-credential pool, got %d", len(creds))
		}
		return &models.EmbeddingsResponse{
			Data: []models.Embedding{{Index: 0, Values: []float64{9.9}}, {Index: 1, Values: []float64{8.8}}},
		}, nil
	}

	response, err := service.Embed(context.Background(), embedRequest("mock/gemini-embedding-001"), nil)
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	if response.Data[0].Values[0] != 9.9 {
		t.Fatalf("expected backend response, got %+v", response.Data[0])
	}
}

func TestRouterService_TestModel_ProbeUnsupportedOnGoBackend(t *testing.T) {
	service, credentialService, _, _ := setupSpeechRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")

	for _, probe := range []struct {
		model    string
		endpoint string
	}{
		{"mock/gemini-tts", "speech"},
		{"mock/imagen-3", "image"},
	} {
		result := service.TestModel(context.Background(), models.ModelId(probe.model), probe.endpoint)
		if result.OK || result.Code != "unsupported" {
			t.Fatalf("Go backends must report unsupported: %+v", result)
		}
	}
}

func TestRouterService_TestEmbeddings_ProbeUnsupportedOnGoBackend(t *testing.T) {
	service, credentialService, _, _ := setupEmbedRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")

	result := service.TestModel(context.Background(), "mock/gemini-embedding-001", "embeddings")
	if result.OK || result.Code != "unsupported" {
		t.Fatalf("Go backends must report unsupported: %+v", result)
	}
}
