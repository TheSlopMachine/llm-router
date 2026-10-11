package responses

import (
	"context"
	"errors"
	"testing"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func setupGormService(t *testing.T) (*GormService, context.Context) {
	t.Helper()
	return NewGormService(testutil.SetupTestSQLite(t).Handle), context.Background()
}

func TestGormResponsesCRUD(t *testing.T) {
	service, ctx := setupGormService(t)
	current := &models.ResponseRecord{ID: "resp_1", Kind: models.ResponseRecordResponse, Model: "mock/m", Status: "completed"}
	if err := service.Create(ctx, current); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := service.Get(ctx, "resp_1", models.ResponseRecordResponse)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Model != "mock/m" {
		t.Fatalf("model: %q", got.Model)
	}
	if _, err := service.Get(ctx, "resp_1", models.ResponseRecordThread); err == nil {
		t.Fatal("kind mismatch must miss")
	}
	if _, err := service.Get(ctx, "missing", models.ResponseRecordResponse); err == nil {
		t.Fatal("missing must miss")
	}
	current.Status = "canceled"
	if err := service.Update(ctx, current); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = service.Get(ctx, "resp_1")
	if got.Status != "canceled" {
		t.Fatalf("status: %q", got.Status)
	}
	list, err := service.List(ctx, []models.ResponseRecordKind{models.ResponseRecordResponse}, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := service.Delete(ctx, "resp_1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := service.Delete(ctx, "resp_1"); err != nil {
		t.Fatalf("re-delete must not error: %v", err)
	}
}

func TestGormResponsesListParentFilter(t *testing.T) {
	service, ctx := setupGormService(t)
	for _, current := range []*models.ResponseRecord{
		{ID: "msg_1", Kind: models.ResponseRecordThreadMessage, ParentID: "thread_a"},
		{ID: "msg_2", Kind: models.ResponseRecordThreadMessage, ParentID: "thread_b"},
		{ID: "msg_3", Kind: models.ResponseRecordThreadMessage},
	} {
		if err := service.Create(ctx, current); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	list, err := service.List(ctx, []models.ResponseRecordKind{models.ResponseRecordThreadMessage}, "thread_a")
	if err != nil || len(list) != 1 || list[0].ID != "msg_1" {
		t.Fatalf("filtered list: %+v %v", list, err)
	}
	unfiltered, err := service.List(ctx, []models.ResponseRecordKind{models.ResponseRecordThreadMessage}, "")
	if err != nil || len(unfiltered) != 3 {
		t.Fatalf("unfiltered list: %+v %v", unfiltered, err)
	}
	if unfiltered[0].ID != "msg_1" || unfiltered[1].ID != "msg_2" || unfiltered[2].ID != "msg_3" {
		t.Fatalf("creation order: %+v", unfiltered)
	}
}

func TestGormResponsesPayloadRoundTrip(t *testing.T) {
	service, ctx := setupGormService(t)
	current := &models.ResponseRecord{
		ID:      "resp_9",
		Kind:    models.ResponseRecordResponse,
		Model:   "mock/m",
		Status:  "completed",
		RefID:   "resp_8",
		Payload: []byte(`{"id":"resp_9","status":"completed"}`),
		Input:   []byte(`[{"type":"message","role":"user"}]`),
	}
	if err := service.Create(ctx, current); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := service.Get(ctx, "resp_9")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got.Payload) != string(current.Payload) {
		t.Fatalf("payload: %q", got.Payload)
	}
	if string(got.Input) != string(current.Input) {
		t.Fatalf("input: %q", got.Input)
	}
	if got.RefID != "resp_8" {
		t.Fatalf("ref id: %q", got.RefID)
	}
	absent := &models.ResponseRecord{ID: "resp_10", Kind: models.ResponseRecordResponse, Model: "mock/m"}
	if err := service.Create(ctx, absent); err != nil {
		t.Fatalf("create without bodies: %v", err)
	}
	got, err = service.Get(ctx, "resp_10")
	if err != nil {
		t.Fatalf("get without bodies: %v", err)
	}
	if len(got.Payload) != 0 || len(got.Input) != 0 {
		t.Fatalf("bodies must stay absent: %q %q", got.Payload, got.Input)
	}
}

func TestGormResponsesUpdateMissing(t *testing.T) {
	service, ctx := setupGormService(t)
	err := service.Update(ctx, &models.ResponseRecord{ID: "missing", Kind: models.ResponseRecordResponse})
	if !errors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	if err := service.Create(ctx, nil); err == nil {
		t.Fatal("nil create must fail")
	}
	if err := service.Create(ctx, &models.ResponseRecord{}); err == nil {
		t.Fatal("empty id create must fail")
	}
	if err := service.Update(ctx, nil); err == nil {
		t.Fatal("nil update must fail")
	}
}

func TestGormResponsesConverterParentNull(t *testing.T) {
	service, ctx := setupGormService(t)
	if err := service.Create(ctx, &models.ResponseRecord{ID: "thread_1", Kind: models.ResponseRecordThread}); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := service.Get(ctx, "thread_1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ParentID != "" {
		t.Fatalf("parent id: %q", got.ParentID)
	}
	row := toStoredRecord(&models.ResponseRecord{ID: "thread_2", Kind: models.ResponseRecordThread})
	if row.ParentID != nil {
		t.Fatal("empty parent must map to NULL")
	}
	round := fromStoredRecord(row)
	if round.ParentID != "" {
		t.Fatalf("NULL parent must map to empty: %q", round.ParentID)
	}
}
