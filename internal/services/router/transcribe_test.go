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

// transcribeMockAdapter extends the shared test mock with Transcribe.
type transcribeMockAdapter struct {
	*testutil.MockAdapter
	infos          []models.ModelInfo
	transcribeFunc func(context.Context, []*models.Credential, *models.TranscriptionRequest) (*models.TranscriptionResponse, error)
}

func (m *transcribeMockAdapter) GetModelInfos(ctx context.Context, credential *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	return m.infos, nil
}

func (m *transcribeMockAdapter) Transcribe(ctx context.Context, creds []*models.Credential, request *models.TranscriptionRequest, _ map[string]any) (*models.TranscriptionResponse, error) {
	if m.transcribeFunc != nil {
		return m.transcribeFunc(ctx, creds, request)
	}
	return &models.TranscriptionResponse{Text: "transcribed"}, nil
}

func setupTranscribeRouter(t *testing.T) (*Service, *credential.GormService, *modelinfo.GormService, *transcribeMockAdapter) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()

	providerService := provider.NewGorm(database)
	mock := &transcribeMockAdapter{MockAdapter: testutil.NewMockAdapter("mock")}
	providerService.RegisterGoAdapter(mock)
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credentialService := credential.NewGorm(database, providerService)
	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)

	return New(providerService, credentialService, modelInfoService, videojobs.New(database), nil, nil, slog.Default()), credentialService, modelInfoService, mock
}

func addTranscribeCred(t *testing.T, credentialService *credential.GormService, label string) {
	t.Helper()
	if _, err := credentialService.Add(context.Background(), credential.AddOptions{
		ProviderID: "mock",
		Label:      label,
		Data:       map[string]any{"api_key": "test-key"},
	}); err != nil {
		t.Fatalf("add credential: %v", err)
	}
}

func transcribeRequest(model string) *models.TranscriptionRequest {
	return &models.TranscriptionRequest{
		Model:       models.ModelId(model),
		File:        []byte("audio-bytes"),
		FileName:    "clip.wav",
		ContentType: "audio/wav",
	}
}

func TestRouterService_Transcribe_Success(t *testing.T) {
	service, credentialService, _, _ := setupTranscribeRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")

	response, err := service.Transcribe(context.Background(), transcribeRequest("mock/whisper-large-v3"), nil)
	if err != nil {
		t.Fatalf("transcribe failed: %v", err)
	}
	if response.Text != "transcribed" {
		t.Fatalf("text: got %q", response.Text)
	}
}

func TestRouterService_Transcribe_UnsupportedAdapter(t *testing.T) {
	service, credentialService, _, _ := setupRouterService(t)
	addTranscribeCred(t, credentialService, "Cred 1")

	_, err := service.Transcribe(context.Background(), transcribeRequest("mock/test-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Transcribe_ModelGateRejects(t *testing.T) {
	service, credentialService, modelInfoService, mock := setupTranscribeRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "chat-model", DisplayName: "Chat", Endpoints: []string{models.EndpointChatCompletions}},
	}
	if _, err := modelInfoService.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	_, err := service.Transcribe(context.Background(), transcribeRequest("mock/chat-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Transcribe_ModelGateAllowsAndBlocksChat(t *testing.T) {
	service, credentialService, modelInfoService, mock := setupTranscribeRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "stt-model", DisplayName: "STT", Endpoints: []string{models.EndpointAudioTranscription}},
	}
	if _, err := modelInfoService.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	response, err := service.Transcribe(context.Background(), transcribeRequest("mock/stt-model"), nil)
	if err != nil {
		t.Fatalf("transcribe failed: %v", err)
	}
	if response.Text != "transcribed" {
		t.Fatalf("text: got %q", response.Text)
	}

	// A transcription-only model must not serve chat completions.
	_, err = service.Complete(context.Background(), &models.ChatCompletionRequest{
		Model:    "mock/stt-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported for chat on stt model, got %v", err)
	}
}

func TestRouterService_Transcribe_PassesPoolToBackend(t *testing.T) {
	service, credentialService, _, mock := setupTranscribeRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")
	addTranscribeCred(t, credentialService, "Cred 2")

	mock.transcribeFunc = func(ctx context.Context, creds []*models.Credential, request *models.TranscriptionRequest) (*models.TranscriptionResponse, error) {
		if len(creds) != 2 {
			return nil, fmt.Errorf("expected 2-credential pool, got %d", len(creds))
		}
		return &models.TranscriptionResponse{Text: "second credential"}, nil
	}

	response, err := service.Transcribe(context.Background(), transcribeRequest("mock/whisper-large-v3"), nil)
	if err != nil {
		t.Fatalf("transcribe failed: %v", err)
	}
	if response.Text != "second credential" {
		t.Fatalf("text: got %q", response.Text)
	}
}

func TestRouterService_TestTranscribe_ProbeUnsupportedOnGoBackend(t *testing.T) {
	service, credentialService, _, _ := setupTranscribeRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")

	result := service.TestModel(context.Background(), "mock/whisper-large-v3", "transcription")
	if result.OK || result.Code != "unsupported" {
		t.Fatalf("Go backends must report unsupported: %+v", result)
	}
}
