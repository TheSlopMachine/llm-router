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
	return NewSQLite(database), context.Background()
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

func TestCreateGetSQLiteRoundTrip(t *testing.T) {
	service, ctx := openSQLiteService(t)
	job := buildJob("job-roundtrip")
	if err := service.CreateSQLite(ctx, job); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := service.GetSQLite(ctx, "job-roundtrip")
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

func TestCreateSQLiteValidation(t *testing.T) {
	service, ctx := openSQLiteService(t)
	if err := service.CreateSQLite(ctx, nil); err == nil {
		t.Errorf("create nil succeeded, want required error")
	}
	if err := service.CreateSQLite(ctx, &models.VideoJob{}); err == nil {
		t.Errorf("create empty id succeeded, want id required error")
	}
}

func TestCreateSQLiteOverwrites(t *testing.T) {
	service, ctx := openSQLiteService(t)
	if err := service.CreateSQLite(ctx, buildJob("job-overwrite")); err != nil {
		t.Fatalf("create: %v", err)
	}
	second := buildJob("job-overwrite")
	second.Status = "processing"
	if err := service.CreateSQLite(ctx, second); err != nil {
		t.Fatalf("recreate: %v", err)
	}
	got, err := service.GetSQLite(ctx, "job-overwrite")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "processing" {
		t.Errorf("status is %q, want processing", got.Status)
	}
}

func TestGetSQLiteMissReturnsNotFound(t *testing.T) {
	service, ctx := openSQLiteService(t)
	_, err := service.GetSQLite(ctx, "job-missing")
	if !errors.Is(err, apierrors.ErrNotFound) {
		t.Errorf("error is %v, want not found sentinel", err)
	}
}

func TestSetStatusSQLiteUpdates(t *testing.T) {
	service, ctx := openSQLiteService(t)
	if err := service.CreateSQLite(ctx, buildJob("job-status")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := service.SetStatusSQLite(ctx, "job-status", "completed"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	got, err := service.GetSQLite(ctx, "job-status")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("status is %q, want completed", got.Status)
	}
}

func TestSetStatusSQLiteMissReturnsNotFound(t *testing.T) {
	service, ctx := openSQLiteService(t)
	err := service.SetStatusSQLite(ctx, "job-missing", "completed")
	if !errors.Is(err, apierrors.ErrNotFound) {
		t.Errorf("error is %v, want not found sentinel", err)
	}
}

func TestSQLiteProviderIDStaysSoft(t *testing.T) {
	service, ctx := openSQLiteService(t)
	job := buildJob("job-soft-provider")
	job.ProviderID = "provider-that-does-not-exist"
	if err := service.CreateSQLite(ctx, job); err != nil {
		t.Fatalf("create with unknown provider: %v", err)
	}
	got, err := service.GetSQLite(ctx, "job-soft-provider")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ProviderID != "provider-that-does-not-exist" {
		t.Errorf("provider id is %q, want soft reference kept", got.ProviderID)
	}
}

func TestSQLiteMethodsFailClosedWithoutHandle(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := New(database)
	ctx := context.Background()
	if err := service.CreateSQLite(ctx, buildJob("job-nowhere")); err == nil {
		t.Errorf("create without sqlite succeeded, want wired error")
	}
	if _, err := service.GetSQLite(ctx, "job-nowhere"); err == nil {
		t.Errorf("get without sqlite succeeded, want wired error")
	}
	if err := service.SetStatusSQLite(ctx, "job-nowhere", "completed"); err == nil {
		t.Errorf("set status without sqlite succeeded, want wired error")
	}
}

func TestWithSQLiteAttachesAlongside(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := New(database)
	sqliteDatabase := testutil.SetupTestSQLite(t)
	service.WithSQLite(sqliteDatabase)
	ctx := context.Background()

	bboltJob := buildJob("job-bbolt-kept")
	if err := service.Create(bboltJob); err != nil {
		t.Fatalf("bbolt create: %v", err)
	}
	sqliteJob := buildJob("job-sqlite-attached")
	if err := service.CreateSQLite(ctx, sqliteJob); err != nil {
		t.Fatalf("sqlite create: %v", err)
	}
	if _, err := service.Get("job-bbolt-kept"); err != nil {
		t.Errorf("bbolt get: %v", err)
	}
	if _, err := service.GetSQLite(ctx, "job-sqlite-attached"); err != nil {
		t.Errorf("sqlite get: %v", err)
	}
}
