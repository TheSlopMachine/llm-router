package modelinfo

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

func TestRemoveModel_DropsFromCacheAndStore(t *testing.T) {
	service, ctx, database := setupGormModelInfo(t)
	seedGormProvider(t, database, ctx, "modelinfo-test", "Test")
	seed := []models.ModelInfo{{Name: "gone-model"}, {Name: "stays-model"}}
	if err := service.PutSnapshot(ctx, "modelinfo-test", seed, util.Now()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Hydrate memory so both layers are exercised.
	if got := service.PeekModelInfos(ctx, "modelinfo-test"); len(got) != 2 {
		t.Fatalf("seeded peek: %d", len(got))
	}

	removed, err := service.RemoveModel(ctx, "modelinfo-test", "gone-model")
	if err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
	got := service.PeekModelInfos(ctx, "modelinfo-test")
	if len(got) != 1 || got[0].Name != "stays-model" {
		t.Fatalf("memory after remove: %+v", got)
	}
	stored, _, err := service.GetSnapshot(ctx, "modelinfo-test")
	if err != nil || len(stored) != 1 || stored[0].Name != "stays-model" {
		t.Fatalf("store after remove: %+v err=%v", stored, err)
	}
}

func TestRemoveModel_MissingIsNoop(t *testing.T) {
	service, ctx, database := setupGormModelInfo(t)
	if removed, err := service.RemoveModel(ctx, "no-such-provider", "x"); err != nil || removed {
		t.Fatalf("missing provider: removed=%v err=%v", removed, err)
	}
	seedGormProvider(t, database, ctx, "modelinfo-test", "Test")
	if err := service.PutSnapshot(ctx, "modelinfo-test", []models.ModelInfo{{Name: "a"}}, util.Now()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if removed, err := service.RemoveModel(ctx, "modelinfo-test", "no-such-model"); err != nil || removed {
		t.Fatalf("missing model: removed=%v err=%v", removed, err)
	}
}
