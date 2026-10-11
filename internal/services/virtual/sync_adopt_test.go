package virtual

import (
	"context"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestSyncAdoptsOrphanWithAutoName(t *testing.T) {
	service, _ := newSyncTestStack(t)
	ctx := context.Background()

	orphan := &models.VirtualModel{
		Name:   "Demo Chat",
		Models: []models.VirtualModelEntry{{ModelID: "demo/chat-a"}},
	}
	if err := service.Create(orphan); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}

	groups, err := service.SyncProviderModels(ctx, "demo")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	chat := groupByEndpoint(groups)["chat"]
	if chat.Virtual == nil {
		t.Fatal("chat group has no virtual after sync")
	}
	if chat.Virtual.ID != orphan.ID {
		t.Fatalf("orphan not adopted: %q vs %q", chat.Virtual.ID, orphan.ID)
	}
	if chat.Virtual.ManagedBy != ProviderMarker("demo", "chat") {
		t.Fatalf("marker: %q", chat.Virtual.ManagedBy)
	}
	if all, _ := service.List(); len(all) != 2 {
		t.Fatalf("expected 2 virtual models (adopted + speech), got %d", len(all))
	}
}
