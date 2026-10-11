package modelinfo

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestRemoveModel_DropsFromCacheAndStore(t *testing.T) {
	service, _, _, _ := setupModelInfoService(t)
	seed := []models.ModelInfo{{Name: "gone-model"}, {Name: "stays-model"}}
	if err := service.records.Put("modelinfo-test", &modelInfoRecord{Models: seed}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Hydrate memory so both layers are exercised.
	if got := service.PeekModelInfos("modelinfo-test"); len(got) != 2 {
		t.Fatalf("seeded peek: %d", len(got))
	}

	removed, err := service.RemoveModel("modelinfo-test", "gone-model")
	if err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
	got := service.PeekModelInfos("modelinfo-test")
	if len(got) != 1 || got[0].Name != "stays-model" {
		t.Fatalf("memory after remove: %+v", got)
	}
	record, err := service.records.Get("modelinfo-test")
	if err != nil || record == nil || len(record.Models) != 1 || record.Models[0].Name != "stays-model" {
		t.Fatalf("store after remove: %+v err=%v", record, err)
	}
}

func TestRemoveModel_MissingIsNoop(t *testing.T) {
	service, _, _, _ := setupModelInfoService(t)
	if removed, err := service.RemoveModel("no-such-provider", "x"); err != nil || removed {
		t.Fatalf("missing provider: removed=%v err=%v", removed, err)
	}
	if err := service.records.Put("modelinfo-test", &modelInfoRecord{Models: []models.ModelInfo{{Name: "a"}}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if removed, err := service.RemoveModel("modelinfo-test", "no-such-model"); err != nil || removed {
		t.Fatalf("missing model: removed=%v err=%v", removed, err)
	}
}
