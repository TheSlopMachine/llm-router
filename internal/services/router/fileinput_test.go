package router

import (
	"context"
	"errors"
	"io"
	"testing"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

func fileRequest(model string) *models.ChatCompletionRequest {
	return &models.ChatCompletionRequest{
		Model: models.ModelId(model),
		Messages: []models.ChatMessage{{Role: "user", ContentParts: []models.ChatMessageContentPart{
			{Type: "text", Text: "summarize"},
			{Type: "file", File: &models.ChatMessageFile{FileData: "data:application/pdf;base64,JVBE", Filename: "a.pdf"}},
		}}},
	}
}

func TestRouterService_FileInputGate(t *testing.T) {
	service, credentialService, modelInfoService, mock := setupSpeechRouter(t)
	addTranscribeCred(t, credentialService, "Cred 1")
	mock.infos = []models.ModelInfo{
		{Name: "text-only", InputModalities: []string{"text", "image"}},
		{Name: "doc-model", InputModalities: []string{"text", "file"}},
		{Name: "undeclared"},
	}
	ctx := context.Background()
	if _, err := modelInfoService.GetModelInfos(ctx, "mock"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}

	if _, err := service.Complete(ctx, fileRequest("mock/text-only"), nil); !errors.Is(err, apierrors.ErrInputNotSupported) {
		t.Fatalf("text-only model: got %v, want ErrInputNotSupported", err)
	}
	if err := service.CompleteStream(ctx, fileRequest("mock/text-only"), io.Discard, nil); !errors.Is(err, apierrors.ErrInputNotSupported) {
		t.Fatalf("text-only stream: got %v, want ErrInputNotSupported", err)
	}
	if _, err := service.Complete(ctx, fileRequest("mock/doc-model"), nil); err != nil {
		t.Fatalf("doc model: %v", err)
	}
	if _, err := service.Complete(ctx, fileRequest("mock/undeclared"), nil); err != nil {
		t.Fatalf("model without declared modalities: %v", err)
	}
	plain := &models.ChatCompletionRequest{Model: "mock/text-only", Messages: []models.ChatMessage{{Role: "user", Content: "hi"}}}
	if _, err := service.Complete(ctx, plain, nil); err != nil {
		t.Fatalf("text request on text-only model: %v", err)
	}
}
