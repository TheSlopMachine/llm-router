package virtual

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// fakeMemberRouter is a minimal provider.MemberRouter for exercising the
// fall-through loop without a real router service: succeedAt is the model
// whose attempt succeeds (empty = every attempt fails).
type fakeMemberRouter struct {
	succeedAt string
	attempted []string
}

// attempt records one member call and answers it with response or a failure.
func attempt[Response any](f *fakeMemberRouter, model models.ModelId, response Response) (Response, error) {
	f.attempted = append(f.attempted, model.String())
	if model.String() == f.succeedAt {
		return response, nil
	}
	var empty Response
	return empty, fmt.Errorf("fake failure for %s", model)
}

func (f *fakeMemberRouter) Complete(_ context.Context, request *models.ChatCompletionRequest, _ *models.RouterToken) (*models.ChatCompletionResponse, error) {
	return attempt(f, request.Model, &models.ChatCompletionResponse{})
}

func (f *fakeMemberRouter) CompleteStream(_ context.Context, request *models.ChatCompletionRequest, _ io.Writer, _ *models.RouterToken) error {
	_, err := attempt(f, request.Model, struct{}{})
	return err
}

func (f *fakeMemberRouter) Transcribe(_ context.Context, request *models.TranscriptionRequest, _ *models.RouterToken) (*models.TranscriptionResponse, error) {
	return attempt(f, request.Model, &models.TranscriptionResponse{})
}

func (f *fakeMemberRouter) Speech(_ context.Context, request *models.SpeechRequest, _ *models.RouterToken) (*models.SpeechResponse, error) {
	return attempt(f, request.Model, &models.SpeechResponse{})
}

func (f *fakeMemberRouter) GenerateImage(_ context.Context, request *models.ImageGenerationRequest, _ *models.RouterToken) (*models.ImageGenerationResponse, error) {
	return attempt(f, request.Model, &models.ImageGenerationResponse{})
}

func (f *fakeMemberRouter) Embed(_ context.Context, request *models.EmbeddingsRequest, _ *models.RouterToken) (*models.EmbeddingsResponse, error) {
	return attempt(f, request.Model, &models.EmbeddingsResponse{})
}

func (f *fakeMemberRouter) Moderate(_ context.Context, request *models.ModerationRequest, _ *models.RouterToken) (*models.ModerationResponse, error) {
	return attempt(f, request.Model, &models.ModerationResponse{})
}

func (f *fakeMemberRouter) SubmitVideo(_ context.Context, request *models.VideoGenerationRequest, _ *models.RouterToken) (*models.VideoGenerationResponse, error) {
	return attempt(f, request.Model, &models.VideoGenerationResponse{ID: "job-1", PollingURL: "/v1/videos/job-1", Status: models.VideoStatusCompleted})
}

func newVirtualModelStack(t *testing.T) *virtual.GormService {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()
	providerService := provider.NewGorm(database)
	credentialService := credential.NewGorm(database, providerService)
	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, time.Hour)
	virtualService := virtual.NewGorm(database, providerService, modelInfoService)
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Demo", TypeKey: "demo"}); err != nil {
		t.Fatalf("seed demo provider: %v", err)
	}
	return virtualService
}

func TestComplete_FallsThroughInListOrder(t *testing.T) {
	virtualService := newVirtualModelStack(t)
	virtualModel := &models.VirtualModel{
		Name: "Fallback",
		Models: []models.VirtualModelEntry{
			{ModelID: "demo/model-a"},
			{ModelID: "demo/model-b"},
			{ModelID: "demo/model-c"},
		},
	}
	if err := virtualService.Create(context.Background(), virtualModel); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	fake := &fakeMemberRouter{succeedAt: "demo/model-b"}
	adapter := New(fake, virtualService, nil)
	request := &models.ChatCompletionRequest{Model: models.ModelId("virtual/" + virtualModel.ID)}
	if _, err := adapter.Complete(context.Background(), nil, request, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(fake.attempted) != 2 || fake.attempted[0] != "demo/model-a" || fake.attempted[1] != "demo/model-b" {
		t.Fatalf("attempted = %v, want [demo/model-a demo/model-b] (list order, model-c never reached)", fake.attempted)
	}
}

func TestComplete_TriesEveryMemberInOrder(t *testing.T) {
	// Every member fails. All members are attempted in list order and the
	// last error surfaces: nothing is skipped or deprioritized.
	virtualService := newVirtualModelStack(t)
	virtualModel := &models.VirtualModel{
		Name: "AllFlagged",
		Models: []models.VirtualModelEntry{
			{ModelID: "demo/model-a"},
			{ModelID: "demo/model-b"},
		},
	}
	if err := virtualService.Create(context.Background(), virtualModel); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	fake := &fakeMemberRouter{}
	adapter := New(fake, virtualService, nil)
	request := &models.ChatCompletionRequest{Model: models.ModelId("virtual/" + virtualModel.ID)}
	_, err := adapter.Complete(context.Background(), nil, request, nil)
	if err == nil {
		t.Fatal("expected an error: every member fails and none should silently succeed")
	}
	if len(fake.attempted) != 2 || fake.attempted[0] != "demo/model-a" || fake.attempted[1] != "demo/model-b" {
		t.Fatalf("attempted = %v, want [demo/model-a demo/model-b]: members tried in order", fake.attempted)
	}
}

func TestCompleteStream_FallsThroughInListOrder(t *testing.T) {
	virtualService := newVirtualModelStack(t)
	virtualModel := &models.VirtualModel{
		Name: "FallbackStream",
		Models: []models.VirtualModelEntry{
			{ModelID: "demo/model-a"},
			{ModelID: "demo/model-b"},
		},
	}
	if err := virtualService.Create(context.Background(), virtualModel); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	fake := &fakeMemberRouter{succeedAt: "demo/model-b"}
	adapter := New(fake, virtualService, nil)
	request := &models.ChatCompletionRequest{Model: models.ModelId("virtual/" + virtualModel.ID)}
	var buffer bytes.Buffer
	if err := adapter.CompleteStream(context.Background(), nil, request, &buffer, nil); err != nil {
		t.Fatalf("complete stream: %v", err)
	}
	if len(fake.attempted) != 2 || fake.attempted[0] != "demo/model-a" || fake.attempted[1] != "demo/model-b" {
		t.Fatalf("attempted = %v, want [demo/model-a demo/model-b]", fake.attempted)
	}
}

// Every endpoint a virtual model serves falls through its members in list order.
func TestEveryEndpointFallsThroughInListOrder(t *testing.T) {
	virtualService := newVirtualModelStack(t)
	virtualModel := &models.VirtualModel{
		Name: "Endpoints",
		Models: []models.VirtualModelEntry{
			{ModelID: "demo/model-a"},
			{ModelID: "demo/model-b"},
		},
	}
	if err := virtualService.Create(context.Background(), virtualModel); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}
	model := models.ModelId("virtual/" + virtualModel.ID)
	calls := map[string]func(context.Context, *Adapter) error{
		"transcribe": func(ctx context.Context, adapter *Adapter) error {
			_, err := adapter.Transcribe(ctx, nil, &models.TranscriptionRequest{Model: model}, nil)
			return err
		},
		"speech": func(ctx context.Context, adapter *Adapter) error {
			_, err := adapter.Speech(ctx, nil, &models.SpeechRequest{Model: model}, nil)
			return err
		},
		"image": func(ctx context.Context, adapter *Adapter) error {
			_, err := adapter.GenerateImage(ctx, nil, &models.ImageGenerationRequest{Model: model}, nil)
			return err
		},
		"embed": func(ctx context.Context, adapter *Adapter) error {
			_, err := adapter.Embed(ctx, nil, &models.EmbeddingsRequest{Model: model}, nil)
			return err
		},
		"moderate": func(ctx context.Context, adapter *Adapter) error {
			_, err := adapter.Moderate(ctx, nil, &models.ModerationRequest{Model: model}, nil)
			return err
		},
		"video": func(ctx context.Context, adapter *Adapter) error {
			_, err := adapter.SubmitVideo(ctx, nil, &models.VideoGenerationRequest{Model: model}, nil)
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			fake := &fakeMemberRouter{succeedAt: "demo/model-b"}
			if err := call(context.Background(), New(fake, virtualService, nil)); err != nil {
				t.Fatalf("call: %v", err)
			}
			if len(fake.attempted) != 2 || fake.attempted[0] != "demo/model-a" || fake.attempted[1] != "demo/model-b" {
				t.Fatalf("attempted = %v, want [demo/model-a demo/model-b]", fake.attempted)
			}
		})
	}
}

func TestGetModelInfosUsesVirtualModelID(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()
	providerService := provider.NewGorm(database)
	credentialService := credential.NewGorm(database, providerService)
	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)
	virtualService := virtual.NewGorm(database, providerService, modelInfoService)

	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Demo", TypeKey: "demo"}); err != nil {
		t.Fatalf("seed demo provider: %v", err)
	}
	virtualModel := &models.VirtualModel{
		Name:   "My Helper",
		Models: []models.VirtualModelEntry{{ModelID: "demo/model-a"}},
	}
	if err := virtualService.Create(ctx, virtualModel); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	adapter := New(nil, virtualService, nil)
	modelInfos, err := adapter.GetModelInfos(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("model infos failed: %v", err)
	}
	if len(modelInfos) != 1 {
		t.Fatalf("modelInfos: got %d, want 1", len(modelInfos))
	}
	if modelInfos[0].Name != "my-helper" {
		t.Errorf("info name: got %q, want virtual model ID %q", modelInfos[0].Name, "my-helper")
	}
	if modelInfos[0].DisplayName != "My Helper" {
		t.Errorf("display name: got %q", modelInfos[0].DisplayName)
	}
}
