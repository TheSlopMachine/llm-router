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

// fakeCompleter is a minimal provider.Completer for exercising the
// fall-through loop without a real router service: succeedAt is the model
// whose attempt succeeds (empty = every attempt fails).
type fakeCompleter struct {
	succeedAt string
	attempted []string
}

func (f *fakeCompleter) Complete(_ context.Context, request *models.ChatCompletionRequest, _ *models.RouterToken) (*models.ChatCompletionResponse, error) {
	f.attempted = append(f.attempted, request.Model.String())
	if request.Model.String() == f.succeedAt {
		return &models.ChatCompletionResponse{}, nil
	}
	return nil, fmt.Errorf("fake failure for %s", request.Model)
}

func (f *fakeCompleter) CompleteStream(_ context.Context, request *models.ChatCompletionRequest, _ io.Writer, _ *models.RouterToken) error {
	f.attempted = append(f.attempted, request.Model.String())
	if request.Model.String() == f.succeedAt {
		return nil
	}
	return fmt.Errorf("fake failure for %s", request.Model)
}

func (f *fakeCompleter) SubmitVideo(_ context.Context, request *models.VideoGenerationRequest, _ *models.RouterToken) (*models.VideoGenerationResponse, error) {
	f.attempted = append(f.attempted, request.Model.String())
	if request.Model.String() == f.succeedAt {
		return &models.VideoGenerationResponse{ID: "job-1", PollingURL: "/v1/videos/job-1", Status: models.VideoStatusCompleted}, nil
	}
	return nil, fmt.Errorf("fake failure for %s", request.Model)
}

func newVirtualModelStack(t *testing.T) *virtual.Service {
	t.Helper()
	database := testutil.SetupTestDB(t)
	providerService := provider.NewService(database)
	credentialService := credential.New(database, providerService)
	modelInfoService := modelinfo.New(database, providerService, credentialService, time.Hour)
	virtualService := virtual.New(database, providerService, modelInfoService)
	if _, err := providerService.Create(provider.CreateOptions{Name: "Demo", TypeKey: "demo"}); err != nil {
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
	if err := virtualService.Create(virtualModel); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	fake := &fakeCompleter{succeedAt: "demo/model-b"}
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
	if err := virtualService.Create(virtualModel); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	fake := &fakeCompleter{}
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
	if err := virtualService.Create(virtualModel); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	fake := &fakeCompleter{succeedAt: "demo/model-b"}
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

func TestGetModelInfosUsesVirtualModelID(t *testing.T) {
	database := testutil.SetupTestDB(t)
	providerService := provider.NewService(database)
	credentialService := credential.New(database, providerService)
	modelInfoService := modelinfo.New(database, providerService, credentialService, 1*time.Hour)
	virtualService := virtual.New(database, providerService, modelInfoService)

	if _, err := providerService.Create(provider.CreateOptions{Name: "Demo", TypeKey: "demo"}); err != nil {
		t.Fatalf("seed demo provider: %v", err)
	}
	virtualModel := &models.VirtualModel{
		Name:   "My Helper",
		Models: []models.VirtualModelEntry{{ModelID: "demo/model-a"}},
	}
	if err := virtualService.Create(virtualModel); err != nil {
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
