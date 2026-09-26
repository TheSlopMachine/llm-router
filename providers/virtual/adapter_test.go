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
// fall-through loop without a real router service: exhausted names the
// models LikelyExhausted reports true for, and succeedAt is the model whose
// attempt succeeds (empty = every attempt fails).
type fakeCompleter struct {
	exhausted map[string]bool
	succeedAt string
	attempted []string
}

func (f *fakeCompleter) LikelyExhausted(model models.ModelId) bool {
	return f.exhausted[model.String()]
}

func (f *fakeCompleter) Complete(_ context.Context, req *models.ChatCompletionRequest, _ *models.RouterToken) (*models.ChatCompletionResponse, error) {
	f.attempted = append(f.attempted, req.Model.String())
	if req.Model.String() == f.succeedAt {
		return &models.ChatCompletionResponse{}, nil
	}
	return nil, fmt.Errorf("fake failure for %s", req.Model)
}

func (f *fakeCompleter) CompleteStream(_ context.Context, req *models.ChatCompletionRequest, _ io.Writer, _ *models.RouterToken) error {
	f.attempted = append(f.attempted, req.Model.String())
	if req.Model.String() == f.succeedAt {
		return nil
	}
	return fmt.Errorf("fake failure for %s", req.Model)
}

func newVirtualModelStack(t *testing.T) *virtual.Service {
	t.Helper()
	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, time.Hour)
	virtualSvc := virtual.New(database, providerSvc, modelInfoSvc)
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Demo", TypeKey: "demo"}); err != nil {
		t.Fatalf("seed demo provider: %v", err)
	}
	return virtualSvc
}

func TestComplete_SkipsLikelyExhaustedMember(t *testing.T) {
	virtualSvc := newVirtualModelStack(t)
	vm := &models.VirtualModel{
		Name: "Fallback",
		Models: []models.VirtualModelEntry{
			{ModelID: "demo/model-a"},
			{ModelID: "demo/model-b"},
			{ModelID: "demo/model-c"},
		},
	}
	if err := virtualSvc.Create(vm); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	fake := &fakeCompleter{exhausted: map[string]bool{"demo/model-a": true}, succeedAt: "demo/model-b"}
	adapter := New(fake, virtualSvc, nil)
	req := &models.ChatCompletionRequest{Model: models.ModelId("virtual/" + vm.ID)}
	if _, err := adapter.Complete(context.Background(), nil, req, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(fake.attempted) != 1 || fake.attempted[0] != "demo/model-b" {
		t.Fatalf("attempted = %v, want exactly [demo/model-b] (model-a skipped, model-c never reached)", fake.attempted)
	}
}

func TestComplete_NeverSkipsLastMember(t *testing.T) {
	// Every member is flagged exhausted, including the last. The last must
	// still be attempted: a stale or wrong mark must never deny the request
	// outright.
	virtualSvc := newVirtualModelStack(t)
	vm := &models.VirtualModel{
		Name: "AllFlagged",
		Models: []models.VirtualModelEntry{
			{ModelID: "demo/model-a"},
			{ModelID: "demo/model-b"},
		},
	}
	if err := virtualSvc.Create(vm); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	fake := &fakeCompleter{exhausted: map[string]bool{"demo/model-a": true, "demo/model-b": true}}
	adapter := New(fake, virtualSvc, nil)
	req := &models.ChatCompletionRequest{Model: models.ModelId("virtual/" + vm.ID)}
	_, err := adapter.Complete(context.Background(), nil, req, nil)
	if err == nil {
		t.Fatal("expected an error: every member fails and none should silently succeed")
	}
	if len(fake.attempted) != 1 || fake.attempted[0] != "demo/model-b" {
		t.Fatalf("attempted = %v, want exactly [demo/model-b]: model-a skipped, model-b is last and must still be tried", fake.attempted)
	}
}

func TestCompleteStream_SkipsLikelyExhaustedMember(t *testing.T) {
	virtualSvc := newVirtualModelStack(t)
	vm := &models.VirtualModel{
		Name: "FallbackStream",
		Models: []models.VirtualModelEntry{
			{ModelID: "demo/model-a"},
			{ModelID: "demo/model-b"},
		},
	}
	if err := virtualSvc.Create(vm); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	fake := &fakeCompleter{exhausted: map[string]bool{"demo/model-a": true}, succeedAt: "demo/model-b"}
	adapter := New(fake, virtualSvc, nil)
	req := &models.ChatCompletionRequest{Model: models.ModelId("virtual/" + vm.ID)}
	var buf bytes.Buffer
	if err := adapter.CompleteStream(context.Background(), nil, req, &buf, nil); err != nil {
		t.Fatalf("complete stream: %v", err)
	}
	if len(fake.attempted) != 1 || fake.attempted[0] != "demo/model-b" {
		t.Fatalf("attempted = %v, want exactly [demo/model-b]", fake.attempted)
	}
}

func TestGetModelInfosUsesVirtualModelID(t *testing.T) {
	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)
	virtualSvc := virtual.New(database, providerSvc, modelInfoSvc)

	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Demo", TypeKey: "demo"}); err != nil {
		t.Fatalf("seed demo provider: %v", err)
	}
	vm := &models.VirtualModel{
		Name:   "My Helper",
		Models: []models.VirtualModelEntry{{ModelID: "demo/model-a"}},
	}
	if err := virtualSvc.Create(vm); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	adapter := New(nil, virtualSvc, nil)
	infos, err := adapter.GetModelInfos(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("model infos failed: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("infos: got %d, want 1", len(infos))
	}
	if infos[0].Name != "my-helper" {
		t.Errorf("info name: got %q, want virtual model ID %q", infos[0].Name, "my-helper")
	}
	if infos[0].DisplayName != "My Helper" {
		t.Errorf("display name: got %q", infos[0].DisplayName)
	}
}
