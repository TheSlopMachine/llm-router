package batches

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func TestBatches_CRUD(t *testing.T) {
	service := New(testutil.SetupTestDB(t))
	record := &models.BatchRecord{ID: "batch_1", Model: "mock/m", Status: StatusInProgress}
	if err := service.Create(record); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := service.Get("batch_1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusInProgress {
		t.Fatalf("status: %q", got.Status)
	}
	if _, err := service.Get("missing"); err == nil {
		t.Fatal("missing must miss")
	}
	record.Status = StatusEnded
	if err := service.Update(record); err != nil {
		t.Fatalf("update: %v", err)
	}
	list, err := service.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := service.Delete("batch_1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = service.List()
	if len(list) != 0 {
		t.Fatalf("list after delete: %+v", list)
	}
}
