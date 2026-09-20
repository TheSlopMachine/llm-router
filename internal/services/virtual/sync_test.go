package virtual

import (
	"context"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func newSyncTestStack(t *testing.T) (*Service, *modelinfo.Service) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	providerSvc.RegisterGoAdapter(testutil.NewMockAdapter("demo").WithModelInfos([]models.ModelInfo{
		{Name: "chat-a", Endpoints: []string{models.EndpointChatCompletions}},
		{Name: "voice-a", Endpoints: []string{models.EndpointAudioSpeech}},
	}))
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Demo", TypeKey: "demo"}); err != nil {
		t.Fatalf("seed demo provider: %v", err)
	}
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)
	if _, err := modelInfoSvc.GetModelInfos(context.Background(), "demo"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}
	return New(database, providerSvc, modelInfoSvc), modelInfoSvc
}

func groupByEndpoint(groups []ProviderVMGroup) map[string]ProviderVMGroup {
	out := map[string]ProviderVMGroup{}
	for _, g := range groups {
		out[g.Endpoint] = g
	}
	return out
}

func TestSyncProviderModelsCreatesOnePerEndpoint(t *testing.T) {
	svc, _ := newSyncTestStack(t)
	ctx := context.Background()

	groups, err := svc.SyncProviderModels(ctx, "demo")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	bySlug := groupByEndpoint(groups)
	if len(bySlug) != 2 {
		t.Fatalf("groups: got %+v", groups)
	}
	chat := bySlug["chat"]
	if len(chat.Models) != 1 || chat.Models[0] != "chat-a" {
		t.Fatalf("chat members: %+v", chat.Models)
	}
	if chat.Virtual == nil || chat.Virtual.ManagedBy != ProviderMarker("demo", "chat") {
		t.Fatalf("chat virtual missing marker: %+v", chat.Virtual)
	}
	if len(chat.Virtual.Models) != 1 || string(chat.Virtual.Models[0].ModelID) != "demo/chat-a" {
		t.Fatalf("chat virtual members: %+v", chat.Virtual.Models)
	}
	speech := bySlug["speech"]
	if speech.Virtual == nil || len(speech.Virtual.Models) != 1 {
		t.Fatalf("speech virtual: %+v", speech.Virtual)
	}

	// Idempotent: resync changes nothing.
	again, err := svc.SyncProviderModels(ctx, "demo")
	if err != nil {
		t.Fatalf("resync: %v", err)
	}
	if groupByEndpoint(again)["chat"].Virtual.ID != chat.Virtual.ID {
		t.Fatal("resync must not recreate")
	}
	if all, _ := svc.List(); len(all) != 2 {
		t.Fatalf("expected 2 managed VMs, got %d", len(all))
	}
}

func TestSyncProviderModelsDropsEmptiedGroup(t *testing.T) {
	svc, modelInfoSvc := newSyncTestStack(t)
	ctx := context.Background()

	if _, err := svc.SyncProviderModels(ctx, "demo"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := modelInfoSvc.SetOverride(models.ModelOverride{
		ProviderID: "demo",
		Name:       "voice-a",
		Disabled:   true,
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	groups, err := svc.SyncProviderModels(ctx, "demo")
	if err != nil {
		t.Fatalf("resync: %v", err)
	}
	if _, ok := groupByEndpoint(groups)["speech"]; ok {
		t.Fatal("emptied group must vanish")
	}
	if all, _ := svc.List(); len(all) != 1 {
		t.Fatalf("managed VM of emptied group must be deleted, got %d", len(all))
	}
}
