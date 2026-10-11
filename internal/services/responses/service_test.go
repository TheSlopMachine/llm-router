package responses

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func setupService(t *testing.T) *Service {
	t.Helper()
	return New(testutil.SetupTestDB(t))
}

func TestResponses_CRUD(t *testing.T) {
	service := setupService(t)
	record := &models.ResponseRecord{ID: "resp_1", Kind: models.ResponseRecordResponse, Model: "mock/m", Status: "completed"}
	if err := service.Create(record); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := service.Get("resp_1", models.ResponseRecordResponse)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Model != "mock/m" {
		t.Fatalf("model: %q", got.Model)
	}
	if _, err := service.Get("resp_1", models.ResponseRecordThread); err == nil {
		t.Fatal("kind mismatch must miss")
	}
	if _, err := service.Get("missing", models.ResponseRecordResponse); err == nil {
		t.Fatal("missing must miss")
	}
	record.Status = "canceled"
	if err := service.Update(record); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = service.Get("resp_1")
	if got.Status != "canceled" {
		t.Fatalf("status: %q", got.Status)
	}
	list, err := service.List([]models.ResponseRecordKind{models.ResponseRecordResponse}, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := service.Delete("resp_1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := service.Delete("resp_1"); err != nil {
		t.Fatalf("re-delete must not error: %v", err)
	}
}

func TestResponses_ListParentFilter(t *testing.T) {
	service := setupService(t)
	for _, record := range []*models.ResponseRecord{
		{ID: "msg_1", Kind: models.ResponseRecordThreadMessage, ParentID: "thread_a"},
		{ID: "msg_2", Kind: models.ResponseRecordThreadMessage, ParentID: "thread_b"},
	} {
		if err := service.Create(record); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	list, err := service.List([]models.ResponseRecordKind{models.ResponseRecordThreadMessage}, "thread_a")
	if err != nil || len(list) != 1 || list[0].ID != "msg_1" {
		t.Fatalf("filtered list: %+v %v", list, err)
	}
}
