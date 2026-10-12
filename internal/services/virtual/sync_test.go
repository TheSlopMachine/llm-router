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

func newSyncTestStack(t *testing.T) (*GormService, *modelinfo.GormService) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()
	providerService := provider.NewGorm(database)
	providerService.RegisterGoAdapter(testutil.NewMockAdapter("demo").WithModelInfos([]models.ModelInfo{
		{Name: "chat-a", Endpoints: []string{models.EndpointChatCompletions}},
		{Name: "voice-a", Endpoints: []string{models.EndpointAudioSpeech}},
	}))
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Demo", TypeKey: "demo"}); err != nil {
		t.Fatalf("seed demo provider: %v", err)
	}
	credentialService := credential.NewGorm(database, providerService)
	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)
	if _, err := modelInfoService.GetModelInfos(context.Background(), "demo"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}
	return NewGorm(database, providerService, modelInfoService), modelInfoService
}

func groupByEndpoint(groups []ProviderVMGroup) map[string]ProviderVMGroup {
	output := map[string]ProviderVMGroup{}
	for _, group := range groups {
		output[group.Endpoint] = group
	}
	return output
}

func TestSyncProviderModelsCreatesOnePerEndpoint(t *testing.T) {
	service, _ := newSyncTestStack(t)
	ctx := context.Background()

	groups, err := service.SyncProviderModels(ctx, "demo")
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
	again, err := service.SyncProviderModels(ctx, "demo")
	if err != nil {
		t.Fatalf("resync: %v", err)
	}
	if groupByEndpoint(again)["chat"].Virtual.ID != chat.Virtual.ID {
		t.Fatal("resync must not recreate")
	}
	if all, _ := service.List(ctx); len(all) != 2 {
		t.Fatalf("expected 2 managed virtual models, got %d", len(all))
	}
}

func TestSyncProviderModelsCreatesVideosGroup(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()
	providerService := provider.NewGorm(database)
	providerService.RegisterGoAdapter(testutil.NewMockAdapter("demo").WithModelInfos([]models.ModelInfo{
		{Name: "vid-a", Endpoints: []string{models.EndpointVideos}},
	}))
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Demo", TypeKey: "demo"}); err != nil {
		t.Fatalf("seed demo provider: %v", err)
	}
	credentialService := credential.NewGorm(database, providerService)
	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)
	if _, err := modelInfoService.GetModelInfos(ctx, "demo"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}
	service := NewGorm(database, providerService, modelInfoService)

	groups, err := service.SyncProviderModels(ctx, "demo")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	bySlug := groupByEndpoint(groups)
	if len(bySlug) != 1 {
		t.Fatalf("groups: got %+v", groups)
	}
	videos := bySlug["videos"]
	if len(videos.Models) != 1 || videos.Models[0] != "vid-a" {
		t.Fatalf("videos members: %+v", videos.Models)
	}
	if videos.Virtual == nil || videos.Virtual.ManagedBy != ProviderMarker("demo", "videos") {
		t.Fatalf("videos virtual missing marker: %+v", videos.Virtual)
	}
	if len(videos.Virtual.Models) != 1 || string(videos.Virtual.Models[0].ModelID) != "demo/vid-a" {
		t.Fatalf("videos virtual members: %+v", videos.Virtual.Models)
	}

	// Idempotent: resync changes nothing.
	again, err := service.SyncProviderModels(ctx, "demo")
	if err != nil {
		t.Fatalf("resync: %v", err)
	}
	if groupByEndpoint(again)["videos"].Virtual.ID != videos.Virtual.ID {
		t.Fatal("resync must not recreate")
	}
	if all, _ := service.List(ctx); len(all) != 1 {
		t.Fatalf("expected 1 managed virtual model, got %d", len(all))
	}
}

func TestSyncProviderModelsDropsEmptiedGroup(t *testing.T) {
	service, modelInfoService := newSyncTestStack(t)
	ctx := context.Background()

	if _, err := service.SyncProviderModels(ctx, "demo"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := modelInfoService.SetOverride(ctx, models.ModelOverride{
		ProviderID: "demo",
		Name:       "voice-a",
		Disabled:   true,
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	groups, err := service.SyncProviderModels(ctx, "demo")
	if err != nil {
		t.Fatalf("resync: %v", err)
	}
	if _, ok := groupByEndpoint(groups)["speech"]; ok {
		t.Fatal("emptied group must vanish")
	}
	if all, _ := service.List(ctx); len(all) != 1 {
		t.Fatalf("managed virtual model of emptied group must be deleted, got %d", len(all))
	}
}

func TestSyncProviderModelsDisabledDeletesManaged(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()
	providerService := provider.NewGorm(database)
	providerService.RegisterGoAdapter(testutil.NewMockAdapter("demo").WithModelInfos([]models.ModelInfo{
		{Name: "chat-a", Endpoints: []string{models.EndpointChatCompletions}},
	}))
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Demo", TypeKey: "demo"}); err != nil {
		t.Fatalf("seed demo provider: %v", err)
	}
	credentialService := credential.NewGorm(database, providerService)
	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)
	if _, err := modelInfoService.GetModelInfos(ctx, "demo"); err != nil {
		t.Fatalf("warm model cache: %v", err)
	}
	service := NewGorm(database, providerService, modelInfoService)

	if _, err := service.SyncProviderModels(ctx, "demo"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if all, _ := service.List(ctx); len(all) != 1 {
		t.Fatalf("expected 1 managed virtual model, got %d", len(all))
	}
	disabled := true
	if _, err := providerService.Update(ctx, "demo", provider.UpdateOptions{Name: "Demo", Disabled: &disabled}); err != nil {
		t.Fatalf("disable provider: %v", err)
	}
	groups, err := service.SyncProviderModels(ctx, "demo")
	if err != nil {
		t.Fatalf("resync: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("disabled provider must hold no groups, got %+v", groups)
	}
	if all, _ := service.List(ctx); len(all) != 0 {
		t.Fatalf("managed virtual models of a disabled provider must delete, got %d", len(all))
	}
	// Re-enabling recreates them from the cache.
	enabled := false
	if _, err := providerService.Update(ctx, "demo", provider.UpdateOptions{Name: "Demo", Disabled: &enabled}); err != nil {
		t.Fatalf("enable provider: %v", err)
	}
	if _, err := service.SyncProviderModels(ctx, "demo"); err != nil {
		t.Fatalf("re-enable sync: %v", err)
	}
	if all, _ := service.List(ctx); len(all) != 1 {
		t.Fatalf("re-enable must recreate managed virtual models, got %d", len(all))
	}
}
