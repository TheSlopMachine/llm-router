package modelinfo

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestRemoveModel_DropsFromCacheAndStore(t *testing.T) {
	svc, _, _, _ := setupModelInfoService(t)
	seed := []models.ModelInfo{{Name: "gone-model"}, {Name: "stays-model"}}
	if err := svc.records.Put("modelinfo-test", &modelInfoRecord{Models: seed}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Hydrate memory so both layers are exercised.
	if got := svc.PeekModelInfos("modelinfo-test"); len(got) != 2 {
		t.Fatalf("seeded peek: %d", len(got))
	}

	removed, err := svc.RemoveModel("modelinfo-test", "gone-model")
	if err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
	got := svc.PeekModelInfos("modelinfo-test")
	if len(got) != 1 || got[0].Name != "stays-model" {
		t.Fatalf("memory after remove: %+v", got)
	}
	rec, err := svc.records.Get("modelinfo-test")
	if err != nil || rec == nil || len(rec.Models) != 1 || rec.Models[0].Name != "stays-model" {
		t.Fatalf("store after remove: %+v err=%v", rec, err)
	}
}

func TestRemoveModel_MissingIsNoop(t *testing.T) {
	svc, _, _, _ := setupModelInfoService(t)
	if removed, err := svc.RemoveModel("no-such-provider", "x"); err != nil || removed {
		t.Fatalf("missing provider: removed=%v err=%v", removed, err)
	}
	if err := svc.records.Put("modelinfo-test", &modelInfoRecord{Models: []models.ModelInfo{{Name: "a"}}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if removed, err := svc.RemoveModel("modelinfo-test", "no-such-model"); err != nil || removed {
		t.Fatalf("missing model: removed=%v err=%v", removed, err)
	}
}
