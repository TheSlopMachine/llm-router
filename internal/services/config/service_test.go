package config

import (
	"context"
	"errors"
	"reflect"
	"testing"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// checks returns a pointer for RouterConfiguration.ProxyMaxChecks.
func checks(value int) *int { return &value }

// flag returns a pointer for the proxy switch fields.
func flag(value bool) *bool { return &value }

func setupSQLiteService(t *testing.T) *Service {
	t.Helper()
	return New(testutil.SetupTestSQLite(t))
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

	want := models.RouterConfiguration{IsClusterNode: true, DisableTelemetry: true, ModelsFilter: "enabled", ProxyMaxChecks: checks(0), ProxyEnabled: flag(true), ProxyPoolEnabled: flag(true)}
	if err := service.PutSettings(ctx, want); err != nil {
		t.Fatalf("put settings: %v", err)
	}
	got, err := service.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
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
	want := models.RouterConfiguration{IsClusterNode: true, ModelsFilter: "disabled", ProxyMaxChecks: checks(0), ProxyEnabled: flag(true), ProxyPoolEnabled: flag(true)}
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
	if !reflect.DeepEqual(got, want) {
		t.Errorf("configuration holds %+v, want %+v", got, want)
	}
}

func TestConfigSetBootstrappedPreservesConfiguration(t *testing.T) {
	ctx := context.Background()
	service := setupSQLiteService(t)

	want := models.RouterConfiguration{DisableTelemetry: true, ModelsFilter: "enabled", ProxyMaxChecks: checks(0), ProxyEnabled: flag(true), ProxyPoolEnabled: flag(true)}
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
	if !reflect.DeepEqual(got, want) {
		t.Errorf("set bootstrapped holds %+v, want %+v", got, want)
	}
}

func TestConfigSQLiteMethodsFailClosedWithoutHandle(t *testing.T) {
	ctx := context.Background()
	service := New(nil)

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

func TestConfigProxyMaxChecksStoredAndPreserved(t *testing.T) {
	ctx := context.Background()
	service := setupSQLiteService(t)

	if err := service.PutSettings(ctx, models.RouterConfiguration{ProxyMaxChecks: checks(120)}); err != nil {
		t.Fatalf("put settings: %v", err)
	}
	got, err := service.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if got.ProxyMaxChecks == nil || *got.ProxyMaxChecks != 120 {
		t.Fatalf("proxy max checks = %v, want 120", got.ProxyMaxChecks)
	}

	// A writer that does not know the field must not reset it.
	if err := service.PutSettings(ctx, models.RouterConfiguration{IsClusterNode: true}); err != nil {
		t.Fatalf("put settings without the field: %v", err)
	}
	got, err = service.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if got.ProxyMaxChecks == nil || *got.ProxyMaxChecks != 120 {
		t.Fatalf("proxy max checks after unrelated write = %v, want 120", got.ProxyMaxChecks)
	}

	// Zero is an explicit value: adaptive mode.
	if err := service.PutSettings(ctx, models.RouterConfiguration{ProxyMaxChecks: checks(0), ProxyEnabled: flag(true), ProxyPoolEnabled: flag(true)}); err != nil {
		t.Fatalf("put settings with zero: %v", err)
	}
	got, err = service.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if got.ProxyMaxChecks == nil || *got.ProxyMaxChecks != 0 {
		t.Fatalf("proxy max checks after zero = %v, want 0", got.ProxyMaxChecks)
	}
}

func TestConfigProxyMaxChecksRangeRejected(t *testing.T) {
	ctx := context.Background()
	service := setupSQLiteService(t)

	for _, value := range []int{-1, models.MaxProxyChecks + 1} {
		if err := service.PutSettings(ctx, models.RouterConfiguration{ProxyMaxChecks: checks(value)}); err == nil {
			t.Fatalf("put settings with proxy_max_checks=%d succeeded, want validation error", value)
		}
	}
}

func TestConfigProxySwitchesStoredAndPreserved(t *testing.T) {
	ctx := context.Background()
	service := setupSQLiteService(t)

	if err := service.PutSettings(ctx, models.RouterConfiguration{ProxyEnabled: flag(false), ProxyPoolEnabled: flag(true)}); err != nil {
		t.Fatalf("put settings: %v", err)
	}
	got, err := service.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if got.ProxyEnabled == nil || *got.ProxyEnabled || got.ProxyPoolEnabled == nil || !*got.ProxyPoolEnabled {
		t.Fatalf("switches = %v / %v, want off / on", got.ProxyEnabled, got.ProxyPoolEnabled)
	}

	// A writer that does not know the switches must not turn the proxies back on.
	if err := service.PutSettings(ctx, models.RouterConfiguration{IsClusterNode: true}); err != nil {
		t.Fatalf("put settings without switches: %v", err)
	}
	got, err = service.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if got.ProxyEnabled == nil || *got.ProxyEnabled {
		t.Fatalf("proxy switch after unrelated write = %v, want it kept off", got.ProxyEnabled)
	}

	if err := service.PutSettings(ctx, models.RouterConfiguration{ProxyPoolEnabled: flag(false)}); err != nil {
		t.Fatalf("put pool switch: %v", err)
	}
	got, err = service.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if got.ProxyEnabled == nil || *got.ProxyEnabled || got.ProxyPoolEnabled == nil || *got.ProxyPoolEnabled {
		t.Fatalf("switches = %v / %v, want off / off", got.ProxyEnabled, got.ProxyPoolEnabled)
	}
}
