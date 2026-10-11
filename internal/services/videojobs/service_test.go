package videojobs

import (
	"context"
	"errors"
	"testing"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func openSQLiteService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	return New(database), context.Background()
}

func buildJob(id string) *models.VideoJob {
	return &models.VideoJob{
		ID:            id,
		Model:         "provider/model",
		BackendModel:  "provider/model",
		ProviderID:    "provider",
		TypeKey:       "mock",
		UpstreamJobID: "upstream-1",
		Status:        "queued",
	}
}

func TestCreateGetRoundTrip(t *testing.T) {
	service, ctx := openSQLiteService(t)
	job := buildJob("job-roundtrip")
	if err := service.Create(ctx, job); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := service.Get(ctx, "job-roundtrip")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != job.ID || string(got.Model) != "provider/model" || string(got.BackendModel) != "provider/model" {
		t.Errorf("identity fields = %+v, want id %q with provider/model", got, job.ID)
	}
	if got.ProviderID != "provider" || got.TypeKey != "mock" || got.UpstreamJobID != "upstream-1" || got.Status != "queued" {
		t.Errorf("stored fields = %+v, want provider/mock/upstream-1/queued", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("timestamps = %v/%v, want both set", got.CreatedAt, got.UpdatedAt)
	}
}

func TestCreateValidation(t *testing.T) {
	service, ctx := openSQLiteService(t)
	if err := service.Create(ctx, nil); err == nil {
		t.Errorf("create nil succeeded, want required error")
	}
	if err := service.Create(ctx, &models.VideoJob{}); err == nil {
		t.Errorf("create empty id succeeded, want id required error")
	}
}

func TestCreateOverwrites(t *testing.T) {
	service, ctx := openSQLiteService(t)
	if err := service.Create(ctx, buildJob("job-overwrite")); err != nil {
		t.Fatalf("create: %v", err)
	}
	second := buildJob("job-overwrite")
	second.Status = "processing"
	if err := service.Create(ctx, second); err != nil {
		t.Fatalf("recreate: %v", err)
	}
	got, err := service.Get(ctx, "job-overwrite")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "processing" {
		t.Errorf("status is %q, want processing", got.Status)
	}
}

func TestGetMissReturnsNotFound(t *testing.T) {
	service, ctx := openSQLiteService(t)
	_, err := service.Get(ctx, "job-missing")
	if !errors.Is(err, apierrors.ErrNotFound) {
		t.Errorf("error is %v, want not found sentinel", err)
	}
}

func TestSetStatusUpdates(t *testing.T) {
	service, ctx := openSQLiteService(t)
	if err := service.Create(ctx, buildJob("job-status")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := service.SetStatus(ctx, "job-status", "completed"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	got, err := service.Get(ctx, "job-status")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("status is %q, want completed", got.Status)
	}
}

func TestSetStatusMissReturnsNotFound(t *testing.T) {
	service, ctx := openSQLiteService(t)
	err := service.SetStatus(ctx, "job-missing", "completed")
	if !errors.Is(err, apierrors.ErrNotFound) {
		t.Errorf("error is %v, want not found sentinel", err)
	}
}

func TestProviderIDStaysSoft(t *testing.T) {
	service, ctx := openSQLiteService(t)
	job := buildJob("job-soft-provider")
	job.ProviderID = "provider-that-does-not-exist"
	if err := service.Create(ctx, job); err != nil {
		t.Fatalf("create with unknown provider: %v", err)
	}
	got, err := service.Get(ctx, "job-soft-provider")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ProviderID != "provider-that-does-not-exist" {
		t.Errorf("provider id is %q, want soft reference kept", got.ProviderID)
	}
}

func TestMethodsFailClosedWithoutHandle(t *testing.T) {
	service := New(nil)
	ctx := context.Background()
	if err := service.Create(ctx, buildJob("job-nowhere")); err == nil {
		t.Errorf("create without sqlite succeeded, want wired error")
	}
	if _, err := service.Get(ctx, "job-nowhere"); err == nil {
		t.Errorf("get without sqlite succeeded, want wired error")
	}
	if err := service.SetStatus(ctx, "job-nowhere", "completed"); err == nil {
		t.Errorf("set status without sqlite succeeded, want wired error")
	}
}
