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
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// transcribeMockAdapter extends the router test mock with Transcribe.
type transcribeMockAdapter struct {
	mockAdapter
	infos          []models.ModelInfo
	transcribeFunc func(context.Context, []*models.Credential, *models.TranscriptionRequest) (*models.TranscriptionResponse, error)
}

func (m *transcribeMockAdapter) GetModelInfos(ctx context.Context, cred *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	return m.infos, nil
}

func (m *transcribeMockAdapter) Transcribe(ctx context.Context, creds []*models.Credential, req *models.TranscriptionRequest, _ map[string]any) (*models.TranscriptionResponse, error) {
	if m.transcribeFunc != nil {
		return m.transcribeFunc(ctx, creds, req)
	}
	return &models.TranscriptionResponse{Text: "transcribed"}, nil
}

func setupTranscribeRouter(t *testing.T) (*Service, *credential.Service, *modelinfo.Service, *transcribeMockAdapter) {
	t.Helper()
	database := testutil.SetupTestDB(t)

	providerSvc := provider.NewService(database)
	mock := &transcribeMockAdapter{mockAdapter: mockAdapter{callCount: new(int)}}
	providerSvc.RegisterGoAdapter(mock)
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)

	return New(providerSvc, credSvc, modelInfoSvc, slog.Default()), credSvc, modelInfoSvc, mock
}

func addTranscribeCred(t *testing.T, credSvc *credential.Service, label string) {
	t.Helper()
	if _, err := credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      label,
		Data:       map[string]any{"api_key": "test-key"},
	}); err != nil {
		t.Fatalf("add credential: %v", err)
	}
}

func transcribeReq(model string) *models.TranscriptionRequest {
	return &models.TranscriptionRequest{
		Model:       models.ModelId(model),
		File:        []byte("audio-bytes"),
		FileName:    "clip.wav",
		ContentType: "audio/wav",
	}
}

func TestRouterService_Transcribe_Success(t *testing.T) {
	svc, credSvc, _, _ := setupTranscribeRouter(t)
	addTranscribeCred(t, credSvc, "Cred 1")

	resp, err := svc.Transcribe(context.Background(), transcribeReq("mock/whisper-large-v3"), nil)
	if err != nil {
		t.Fatalf("transcribe failed: %v", err)
	}
	if resp.Text != "transcribed" {
		t.Fatalf("text: got %q", resp.Text)
	}
}

func TestRouterService_Transcribe_UnsupportedAdapter(t *testing.T) {
	svc, credSvc, _ := setupRouterService(t)
	addTranscribeCred(t, credSvc, "Cred 1")

	_, err := svc.Transcribe(context.Background(), transcribeReq("mock/test-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Transcribe_ModelGateRejects(t *testing.T) {
	svc, credSvc, modelInfoSvc, mock := setupTranscribeRouter(t)
	addTranscribeCred(t, credSvc, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "chat-model", DisplayName: "Chat", Endpoints: []string{models.EndpointChatCompletions}},
	}
	if _, err := modelInfoSvc.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	_, err := svc.Transcribe(context.Background(), transcribeReq("mock/chat-model"), nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Transcribe_ModelGateAllowsAndBlocksChat(t *testing.T) {
	svc, credSvc, modelInfoSvc, mock := setupTranscribeRouter(t)
	addTranscribeCred(t, credSvc, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "stt-model", DisplayName: "STT", Endpoints: []string{models.EndpointAudioTranscription}},
	}
	if _, err := modelInfoSvc.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	resp, err := svc.Transcribe(context.Background(), transcribeReq("mock/stt-model"), nil)
	if err != nil {
		t.Fatalf("transcribe failed: %v", err)
	}
	if resp.Text != "transcribed" {
		t.Fatalf("text: got %q", resp.Text)
	}

	// A transcription-only model must not serve chat completions.
	_, err = svc.Complete(context.Background(), &models.ChatCompletionRequest{
		Model:    "mock/stt-model",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported for chat on stt model, got %v", err)
	}
}

func TestRouterService_Transcribe_PassesPoolToBackend(t *testing.T) {
	svc, credSvc, _, mock := setupTranscribeRouter(t)
	addTranscribeCred(t, credSvc, "Cred 1")
	addTranscribeCred(t, credSvc, "Cred 2")

	mock.transcribeFunc = func(ctx context.Context, creds []*models.Credential, req *models.TranscriptionRequest) (*models.TranscriptionResponse, error) {
		if len(creds) != 2 {
			return nil, fmt.Errorf("expected 2-credential pool, got %d", len(creds))
		}
		return &models.TranscriptionResponse{Text: "second credential"}, nil
	}

	resp, err := svc.Transcribe(context.Background(), transcribeReq("mock/whisper-large-v3"), nil)
	if err != nil {
		t.Fatalf("transcribe failed: %v", err)
	}
	if resp.Text != "second credential" {
		t.Fatalf("text: got %q", resp.Text)
	}
}
