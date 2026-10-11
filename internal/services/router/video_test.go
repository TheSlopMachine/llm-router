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
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// videoMockAdapter extends the shared test mock with video generation.
type videoMockAdapter struct {
	*testutil.MockAdapter
	infos []models.ModelInfo
}

func (m *videoMockAdapter) GetModelInfos(ctx context.Context, credential *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	return m.infos, nil
}

func (m *videoMockAdapter) SubmitVideo(ctx context.Context, creds []*models.Credential, request *models.VideoGenerationRequest, _ map[string]any) (*models.VideoGenerationResponse, error) {
	return &models.VideoGenerationResponse{
		ID:         "up-123",
		PollingURL: "https://upstream.example.com/v/up-123",
		Status:     models.VideoStatusPending,
	}, nil
}

func (m *videoMockAdapter) PollVideo(ctx context.Context, creds []*models.Credential, model models.ModelId, upstreamJobID string, _ map[string]any) (*models.VideoGenerationResponse, error) {
	if upstreamJobID != "up-123" {
		return nil, &models.ProviderError{StatusCode: 404, Code: "not_found", Message: "job not found upstream"}
	}
	return &models.VideoGenerationResponse{
		ID:           upstreamJobID,
		PollingURL:   "/v1/videos/" + upstreamJobID,
		Status:       models.VideoStatusCompleted,
		UnsignedURLs: []string{"https://cdn.example.com/up-123.mp4"},
	}, nil
}

func (m *videoMockAdapter) VideoContent(ctx context.Context, creds []*models.Credential, model models.ModelId, upstreamJobID string, index int, _ map[string]any) (*models.VideoContentResponse, error) {
	return &models.VideoContentResponse{Video: []byte{0, 0, 0, 24, 'f', 't', 'y', 'p'}, ContentType: "video/mp4"}, nil
}

func setupVideoRouter(t *testing.T) (*Service, *credential.Service, *modelinfo.Service, *videoMockAdapter) {
	t.Helper()
	database := testutil.SetupTestDB(t)

	providerService := provider.NewService(database)
	mock := &videoMockAdapter{MockAdapter: testutil.NewMockAdapter("mock")}
	providerService.RegisterGoAdapter(mock)
	if _, err := providerService.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credentialService := credential.New(database, providerService)
	modelInfoService := modelinfo.New(database, providerService, credentialService, 1*time.Hour)

	return New(providerService, credentialService, modelInfoService, videojobs.New(database), nil, nil, slog.Default()), credentialService, modelInfoService, mock
}

func videoRequest(model string) *models.VideoGenerationRequest {
	return &models.VideoGenerationRequest{Model: models.ModelId(model), Prompt: "a cat", Duration: 1}
}

func TestRouterService_Video_SubmitPollContent(t *testing.T) {
	service, credentialService, _, _ := setupVideoRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")

	submission, err := service.SubmitVideo(context.Background(), videoRequest("mock/veo"), nil)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if !strings.HasPrefix(submission.ID, "gen-vid-") || submission.ID == "up-123" {
		t.Fatalf("submit must mint a local id, got %q", submission.ID)
	}
	if submission.PollingURL != "/v1/videos/"+submission.ID {
		t.Fatalf("polling url: %q", submission.PollingURL)
	}
	if submission.Status != models.VideoStatusPending {
		t.Fatalf("status: %q", submission.Status)
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
		t.Fatalf("content: %+v", content)
	}
}

func TestRouterService_Video_UnknownJob(t *testing.T) {
	service, _, _, _ := setupVideoRouter(t)

	if _, err := service.PollVideo(context.Background(), "gen-vid-0-AAAAAAAAAAAAAAAAAAAA", nil); !errors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := service.VideoContent(context.Background(), "gen-vid-0-AAAAAAAAAAAAAAAAAAAA", 0, nil); !errors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRouterService_Video_UnsupportedAdapter(t *testing.T) {
	service, credentialService, _, _ := setupRouterService(t)
	addTranscribeCred(t, credentialService, "Cred 1")

	if _, err := service.SubmitVideo(context.Background(), videoRequest("mock/test-model"), nil); !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Video_ModelGateRejects(t *testing.T) {
	service, credentialService, modelInfoService, mock := setupVideoRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "chat-model", DisplayName: "Chat", Endpoints: []string{models.EndpointChatCompletions}},
	}
	if _, err := modelInfoService.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	if _, err := service.SubmitVideo(context.Background(), videoRequest("mock/chat-model"), nil); !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Video_PreviousJobTranslates(t *testing.T) {
	service, credentialService, _, _ := setupVideoRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")

	submission, err := service.SubmitVideo(context.Background(), videoRequest("mock/veo"), nil)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	cont := videoRequest("mock/veo")
	cont.PreviousJobID = submission.ID
	if _, err := service.SubmitVideo(context.Background(), cont, nil); err != nil {
		t.Fatalf("continuation failed: %v", err)
	}

	bad := videoRequest("mock/veo")
	bad.PreviousJobID = "gen-vid-0-AAAAAAAAAAAAAAAAAAAA"
	_, err = service.SubmitVideo(context.Background(), bad, nil)
	var perr *models.ProviderError
	if !errors.As(err, &perr) || perr.Code != "invalid_request_error" {
		t.Fatalf("expected invalid_request for unknown previous job, got %v", err)
	}
}
