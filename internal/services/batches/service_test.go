package batches

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func TestBatches_CRUD(t *testing.T) {
	svc := New(testutil.SetupTestDB(t))
	rec := &models.BatchRecord{ID: "batch_1", Model: "mock/m", Status: StatusInProgress}
	if err := svc.Create(rec); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.Get("batch_1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusInProgress {
		t.Fatalf("status: %q", got.Status)
	}
	if _, err := svc.Get("missing"); err == nil {
		t.Fatal("missing must miss")
	}
	rec.Status = StatusEnded
	if err := svc.Update(rec); err != nil {
		t.Fatalf("update: %v", err)
	}
	list, err := svc.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := svc.Delete("batch_1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = svc.List()
	if len(list) != 0 {
		t.Fatalf("list after delete: %+v", list)
	}
}
