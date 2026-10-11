package config

import (
	"context"
	"errors"
	"testing"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func setupSQLiteService(t *testing.T) *Service {
	t.Helper()
	database := testutil.SetupTestDB(t)
	sqlite := testutil.SetupTestSQLite(t)
	return NewWithSQLite(database, sqlite)
}

func TestConfigBboltGetPutRoundTrip(t *testing.T) {
	database := testutil.SetupTestDB(t)
	service := New(database)

	want := models.RouterConfiguration{IsClusterNode: true, DisableTelemetry: true, ModelsFilter: "enabled"}
	if err := service.Put(want); err != nil {
		t.Fatalf("bbolt put: %v", err)
	}
	got, err := service.Get()
	if err != nil {
		t.Fatalf("bbolt get: %v", err)
	}
	if got != want {
		t.Errorf("bbolt round trip holds %+v, want %+v", got, want)
	}
	if err := service.Put(models.RouterConfiguration{ModelsFilter: "bogus"}); err == nil {
		t.Errorf("bbolt put with bogus filter succeeded, want validation error")
	}
}

func TestConfigSettingsMissReturnsZero(t *testing.T) {
	ctx := context.Background()
	service := setupSQLiteService(t)

	configuration, err := service.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings on empty table: %v", err)
	}
	if configuration != (models.RouterConfiguration{}) {
		t.Errorf("miss holds %+v, want zero configuration", configuration)
	}
	bootstrapped, err := service.IsBootstrapped(ctx)
	if err != nil {
		t.Fatalf("is bootstrapped on empty table: %v", err)
	}
	if bootstrapped {
		t.Errorf("miss reports bootstrapped, want false")
	}
}

func TestConfigPutSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	service := setupSQLiteService(t)

	want := models.RouterConfiguration{IsClusterNode: true, DisableTelemetry: true, ModelsFilter: "enabled"}
	if err := service.PutSettings(ctx, want); err != nil {
		t.Fatalf("put settings: %v", err)
	}
	got, err := service.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if got != want {
		t.Errorf("round trip holds %+v, want %+v", got, want)
	}
}

func TestConfigPutSettingsEmptyFilterStoresAll(t *testing.T) {
	ctx := context.Background()
	service := setupSQLiteService(t)

	if err := service.PutSettings(ctx, models.RouterConfiguration{}); err != nil {
		t.Fatalf("put settings with empty filter: %v", err)
	}
	got, err := service.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if got.ModelsFilter != "all" {
		t.Errorf("models filter is %q, want all", got.ModelsFilter)
	}
}

func TestConfigPutSettingsInvalidFilterRejected(t *testing.T) {
	ctx := context.Background()
	service := setupSQLiteService(t)

	if err := service.PutSettings(ctx, models.RouterConfiguration{ModelsFilter: "bogus"}); err == nil {
		t.Fatalf("put settings with bogus filter succeeded, want validation error")
	}
	got, err := service.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if got != (models.RouterConfiguration{}) {
		t.Errorf("rejected write holds %+v, want zero configuration", got)
	}
}

func TestConfigSetBootstrappedMarksBootstrapped(t *testing.T) {
	ctx := context.Background()
	service := setupSQLiteService(t)

	if err := service.SetBootstrapped(ctx); err != nil {
		t.Fatalf("set bootstrapped: %v", err)
	}
	bootstrapped, err := service.IsBootstrapped(ctx)
	if err != nil {
		t.Fatalf("is bootstrapped: %v", err)
	}
	if !bootstrapped {
		t.Errorf("flag reads false after set, want true")
	}
	if err := service.SetBootstrapped(ctx); err != nil {
		t.Fatalf("second set bootstrapped: %v", err)
	}
}

func TestConfigPutSettingsPreservesBootstrapped(t *testing.T) {
	ctx := context.Background()
	service := setupSQLiteService(t)

	if err := service.SetBootstrapped(ctx); err != nil {
		t.Fatalf("set bootstrapped: %v", err)
	}
	want := models.RouterConfiguration{IsClusterNode: true, ModelsFilter: "disabled"}
	if err := service.PutSettings(ctx, want); err != nil {
		t.Fatalf("put settings: %v", err)
	}
	bootstrapped, err := service.IsBootstrapped(ctx)
	if err != nil {
		t.Fatalf("is bootstrapped: %v", err)
	}
	if !bootstrapped {
		t.Errorf("put cleared the bootstrapped flag, want it preserved")
	}
	got, err := service.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if got != want {
		t.Errorf("configuration holds %+v, want %+v", got, want)
	}
}

func TestConfigSetBootstrappedPreservesConfiguration(t *testing.T) {
	ctx := context.Background()
	service := setupSQLiteService(t)

	want := models.RouterConfiguration{DisableTelemetry: true, ModelsFilter: "enabled"}
	if err := service.PutSettings(ctx, want); err != nil {
		t.Fatalf("put settings: %v", err)
	}
	if err := service.SetBootstrapped(ctx); err != nil {
		t.Fatalf("set bootstrapped: %v", err)
	}
	got, err := service.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if got != want {
		t.Errorf("set bootstrapped holds %+v, want %+v", got, want)
	}
}

func TestConfigSQLiteMethodsFailClosedWithoutHandle(t *testing.T) {
	ctx := context.Background()
	database := testutil.SetupTestDB(t)
	service := New(database)

	if _, err := service.GetSettings(ctx); !errors.Is(err, apierrors.ErrInvalidInput) {
		t.Errorf("get settings without sqlite returns %v, want invalid input", err)
	}
	if err := service.PutSettings(ctx, models.RouterConfiguration{}); !errors.Is(err, apierrors.ErrInvalidInput) {
		t.Errorf("put settings without sqlite returns %v, want invalid input", err)
	}
	if _, err := service.IsBootstrapped(ctx); !errors.Is(err, apierrors.ErrInvalidInput) {
		t.Errorf("is bootstrapped without sqlite returns %v, want invalid input", err)
	}
	if err := service.SetBootstrapped(ctx); !errors.Is(err, apierrors.ErrInvalidInput) {
		t.Errorf("set bootstrapped without sqlite returns %v, want invalid input", err)
	}
}
