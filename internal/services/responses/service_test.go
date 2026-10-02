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
	svc := setupService(t)
	rec := &models.ResponseRecord{ID: "resp_1", Kind: models.ResponseRecordResponse, Model: "mock/m", Status: "completed"}
	if err := svc.Create(rec); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.Get("resp_1", models.ResponseRecordResponse)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Model != "mock/m" {
		t.Fatalf("model: %q", got.Model)
	}
	if _, err := svc.Get("resp_1", models.ResponseRecordThread); err == nil {
		t.Fatal("kind mismatch must miss")
	}
	if _, err := svc.Get("missing", models.ResponseRecordResponse); err == nil {
		t.Fatal("missing must miss")
	}
	rec.Status = "canceled"
	if err := svc.Update(rec); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = svc.Get("resp_1")
	if got.Status != "canceled" {
		t.Fatalf("status: %q", got.Status)
	}
	list, err := svc.List([]models.ResponseRecordKind{models.ResponseRecordResponse}, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := svc.Delete("resp_1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := svc.Delete("resp_1"); err != nil {
		t.Fatalf("re-delete must not error: %v", err)
	}
}

func TestResponses_ListParentFilter(t *testing.T) {
	svc := setupService(t)
	for _, r := range []*models.ResponseRecord{
		{ID: "msg_1", Kind: models.ResponseRecordThreadMessage, ParentID: "thread_a"},
		{ID: "msg_2", Kind: models.ResponseRecordThreadMessage, ParentID: "thread_b"},
	} {
		if err := svc.Create(r); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	list, err := svc.List([]models.ResponseRecordKind{models.ResponseRecordThreadMessage}, "thread_a")
	if err != nil || len(list) != 1 || list[0].ID != "msg_1" {
		t.Fatalf("filtered list: %+v %v", list, err)
	}
}
