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
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
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

func (m *videoMockAdapter) GetModelInfos(ctx context.Context, cred *models.Credential, _ map[string]any) ([]models.ModelInfo, error) {
	return m.infos, nil
}

func (m *videoMockAdapter) SubmitVideo(ctx context.Context, creds []*models.Credential, req *models.VideoGenerationRequest, _ map[string]any) (*models.VideoGenerationResponse, error) {
	return &models.VideoGenerationResponse{
		ID:         "up-123",
		PollingURL: "https://upstream.example.com/v/up-123",
		Status:     models.VideoStatusPending,
	}, nil
}

func (m *videoMockAdapter) PollVideo(ctx context.Context, creds []*models.Credential, model models.ModelId, upstreamJobID string, _ map[string]any) (*models.VideoGenerationResponse, error) {
	if upstreamJobID != "up-123" {
		return nil, &models.ProviderError{StatusCode: 404, Type: models.ErrorTypeNotFound, Message: "job not found upstream"}
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

	providerSvc := provider.NewService(database)
	mock := &videoMockAdapter{MockAdapter: testutil.NewMockAdapter("mock")}
	providerSvc.RegisterGoAdapter(mock)
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)

	return New(providerSvc, credSvc, modelInfoSvc, exhausted.New(database), videojobs.New(database), slog.Default()), credSvc, modelInfoSvc, mock
}

func videoReq(model string) *models.VideoGenerationRequest {
	return &models.VideoGenerationRequest{Model: models.ModelId(model), Prompt: "a cat", Duration: 1}
}

func TestRouterService_Video_SubmitPollContent(t *testing.T) {
	svc, credSvc, _, _ := setupVideoRouter(t)
	addTranscribeCred(t, credSvc, "Cred 1")

	sub, err := svc.SubmitVideo(context.Background(), videoReq("mock/veo"), nil)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if !strings.HasPrefix(sub.ID, "gen-vid-") || sub.ID == "up-123" {
		t.Fatalf("submit must mint a local id, got %q", sub.ID)
	}
	if sub.PollingURL != "/v1/videos/"+sub.ID {
		t.Fatalf("polling url: %q", sub.PollingURL)
	}
	if sub.Status != models.VideoStatusPending {
		t.Fatalf("status: %q", sub.Status)
	}

	polled, err := svc.PollVideo(context.Background(), sub.ID, nil)
	if err != nil {
		t.Fatalf("poll failed: %v", err)
	}
	if polled.ID != sub.ID || polled.Status != models.VideoStatusCompleted {
		t.Fatalf("poll: %+v", polled)
	}
	if len(polled.UnsignedURLs) != 1 {
		t.Fatalf("unsigned urls: %+v", polled.UnsignedURLs)
	}

	content, err := svc.VideoContent(context.Background(), sub.ID, 0, nil)
	if err != nil {
		t.Fatalf("content failed: %v", err)
	}
	if string(content.Video[4:8]) != "ftyp" || content.ContentType != "video/mp4" {
		t.Fatalf("content: %+v", content)
	}
}

func TestRouterService_Video_UnknownJob(t *testing.T) {
	svc, _, _, _ := setupVideoRouter(t)

	if _, err := svc.PollVideo(context.Background(), "gen-vid-0-AAAAAAAAAAAAAAAAAAAA", nil); !errors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := svc.VideoContent(context.Background(), "gen-vid-0-AAAAAAAAAAAAAAAAAAAA", 0, nil); !errors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRouterService_Video_UnsupportedAdapter(t *testing.T) {
	svc, credSvc, _, _ := setupRouterService(t)
	addTranscribeCred(t, credSvc, "Cred 1")

	if _, err := svc.SubmitVideo(context.Background(), videoReq("mock/test-model"), nil); !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Video_ModelGateRejects(t *testing.T) {
	svc, credSvc, modelInfoSvc, mock := setupVideoRouter(t)
	addTranscribeCred(t, credSvc, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "chat-model", DisplayName: "Chat", Endpoints: []string{models.EndpointChatCompletions}},
	}
	if _, err := modelInfoSvc.GetModelInfos(context.Background(), "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	if _, err := svc.SubmitVideo(context.Background(), videoReq("mock/chat-model"), nil); !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("expected ErrEndpointNotSupported, got %v", err)
	}
}

func TestRouterService_Video_PreviousJobTranslates(t *testing.T) {
	svc, credSvc, _, _ := setupVideoRouter(t)
	addTranscribeCred(t, credSvc, "Cred 1")

	sub, err := svc.SubmitVideo(context.Background(), videoReq("mock/veo"), nil)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	cont := videoReq("mock/veo")
	cont.PreviousJobID = sub.ID
	if _, err := svc.SubmitVideo(context.Background(), cont, nil); err != nil {
		t.Fatalf("continuation failed: %v", err)
	}

	bad := videoReq("mock/veo")
	bad.PreviousJobID = "gen-vid-0-AAAAAAAAAAAAAAAAAAAA"
	_, err = svc.SubmitVideo(context.Background(), bad, nil)
	var perr *models.ProviderError
	if !errors.As(err, &perr) || perr.Type != models.ErrorTypeInvalidRequest {
		t.Fatalf("expected invalid_request for unknown previous job, got %v", err)
	}
}
