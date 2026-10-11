package credential

import (
	"context"
	"testing"
	"time"

	stdErrors "errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

func setupGormCredential(t *testing.T) (*GormService, context.Context) {
	t.Helper()
	boltDatabase := testutil.SetupTestDB(t)
	sqliteDatabase := testutil.SetupTestSQLite(t)

	providerService := provider.NewService(boltDatabase)
	providerService.RegisterGoAdapter(testutil.NewMockAdapter("mock"))
	created, err := providerService.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"})
	if err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	if created.ID != "mock" {
		t.Fatalf("mock provider identifier: got %q want %q", created.ID, "mock")
	}
	now := util.Now()
	providerRow := record.ProviderInstance{
		ID:        "mock",
		Name:      "Mock",
		TypeKey:   "mock",
		Settings:  map[string]any{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	ctx := context.Background()
	if err := gorm.G[record.ProviderInstance](sqliteDatabase.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(ctx, &providerRow); err != nil {
		t.Fatalf("seed sqlite provider: %v", err)
	}
	return NewGorm(sqliteDatabase, providerService), ctx
}

func addGormKey(t *testing.T, service *GormService, ctx context.Context, label string) *models.Credential {
	t.Helper()
	credential, err := service.Add(ctx, AddOptions{
		ProviderID: "mock",
		Label:      label,
		Data:       map[string]any{"api_key": "key-" + label},
	})
	if err != nil {
		t.Fatalf("add %s: %v", label, err)
	}
	return credential
}

func TestGormAddGetRoundTrip(t *testing.T) {
	service, ctx := setupGormCredential(t)
	credential, err := service.Add(ctx, AddOptions{
		ProviderID: "mock",
		Label:      "Test Credential",
		Data:       map[string]any{"api_key": "test-key"},
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if credential.ID == "" {
		t.Fatal("credential identifier must stamp")
	}
	if credential.ProviderID != "mock" || credential.Label != "Test Credential" {
		t.Fatalf("added fields: %+v", credential)
	}
	if credential.CreatedAt.IsZero() || credential.UpdatedAt.IsZero() {
		t.Fatal("timestamps must stamp")
	}
	fetched, err := service.Get(ctx, credential.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Data["api_key"] != "test-key" || fetched.Label != "Test Credential" {
		t.Fatalf("fetched: %+v", fetched)
	}
	if _, err := service.Get(ctx, "missing"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("get miss: got %v want not found", err)
	}
}

func TestGormAddValidation(t *testing.T) {
	service, ctx := setupGormCredential(t)
	if _, err := service.Add(ctx, AddOptions{ProviderID: "mock", Label: "Invalid", Data: map[string]any{}}); err == nil {
		t.Error("empty data must fail validation")
	}
	if _, err := service.Add(ctx, AddOptions{ProviderID: "nonexistent", Label: "Test", Data: map[string]any{"api_key": "key"}}); err == nil {
		t.Error("unknown provider must fail")
	}
}

func TestGormCredentialForeignKey(t *testing.T) {
	service, ctx := setupGormCredential(t)
	now := util.Now()
	dangling := record.Credential{ID: "dangling", ProviderID: "missing-provider", Label: "dangling", CreatedAt: now, UpdatedAt: now}
	database := service.database
	if err := gorm.G[record.Credential](database.Handle).Create(ctx, &dangling); !stdErrors.Is(db.Translate(err), apierrors.ErrInvalidInput) {
		t.Fatalf("dangling provider insert: got %v want invalid input", err)
	}
}

func TestGormListUsableFilters(t *testing.T) {
	service, ctx := setupGormCredential(t)
	available := addGormKey(t, service, ctx, "available")
	expired := addGormKey(t, service, ctx, "expired")
	past := time.Now().Add(-time.Hour)
	if err := service.Update(ctx, expired.ID, map[string]any{"api_key": "key-expired"}, &past); err != nil {
		t.Fatalf("expire: %v", err)
	}
	disabled := addGormKey(t, service, ctx, "disabled")
	off := true
	if err := service.UpdateDetails(ctx, disabled.ID, nil, &off, nil); err != nil {
		t.Fatalf("disable: %v", err)
	}
	usable, err := service.ListUsable(ctx, "mock")
	if err != nil {
		t.Fatalf("list usable: %v", err)
	}
	if len(usable) != 1 || usable[0].ID != available.ID {
		t.Fatalf("usable set: %+v", usable)
	}
	byProvider, err := service.ListByProvider(ctx, "mock")
	if err != nil {
		t.Fatalf("list by provider: %v", err)
	}
	if len(byProvider) != 3 {
		t.Fatalf("provider set: got %d want 3", len(byProvider))
	}
	everything, err := service.ListAll(ctx)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(everything) != 3 {
		t.Fatalf("full set: got %d want 3", len(everything))
	}
}

func TestGormAllDeniesEmpty(t *testing.T) {
	service, ctx := setupGormCredential(t)
	usable, err := service.ListUsable(ctx, "mock")
	if err != nil {
		t.Fatalf("empty pool must not error: %v", err)
	}
	if len(usable) != 0 {
		t.Fatalf("empty pool must yield empty slice, got %v", usable)
	}
	if _, err := service.All(ctx, "mock"); !stdErrors.Is(err, apierrors.ErrNoCredential) {
		t.Fatalf("all on empty pool: got %v want no credential", err)
	}
	addGormKey(t, service, ctx, "one")
	addGormKey(t, service, ctx, "two")
	available, err := service.All(ctx, "mock")
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(available) != 2 {
		t.Fatalf("all set: got %d want 2", len(available))
	}
}

func TestGormUpdatePersists(t *testing.T) {
	service, ctx := setupGormCredential(t)
	credential := addGormKey(t, service, ctx, "old")
	future := time.Now().Add(time.Hour)
	refreshed := map[string]any{"api_key": "key-old", "access_token": "token"}
	if err := service.Update(ctx, credential.ID, refreshed, &future); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, err := service.Get(ctx, credential.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if updated.Data["access_token"] != "token" {
		t.Fatalf("refreshed data not persisted: %v", updated.Data)
	}
	if updated.ExpiresAt == nil {
		t.Fatal("expiry must stamp")
	}
	if err := service.Update(ctx, "missing", refreshed, nil); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("update miss: got %v want not found", err)
	}
}

func TestGormUpdateDetails(t *testing.T) {
	service, ctx := setupGormCredential(t)
	credential := addGormKey(t, service, ctx, "old")
	label := "new"
	if err := service.UpdateDetails(ctx, credential.ID, &label, nil, map[string]any{"api_key": "rotated"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := service.Get(ctx, credential.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Label != "new" || got.Data["api_key"] != "rotated" {
		t.Fatalf("updated fields: %+v", got)
	}
	if err := service.UpdateDetails(ctx, credential.ID, nil, nil, map[string]any{}); err == nil {
		t.Error("empty replacement data must fail validation")
	}
	off := true
	if err := service.UpdateDetails(ctx, credential.ID, nil, &off, nil); err != nil {
		t.Fatalf("admin disable: %v", err)
	}
	got, _ = service.Get(ctx, credential.ID)
	if !got.Disabled || got.DisabledBy != models.DisabledByAdmin || got.DisabledReason != "disabled by admin" || got.DisabledAt == nil {
		t.Fatalf("admin disable state: %+v", got)
	}
	on := false
	if err := service.UpdateDetails(ctx, credential.ID, nil, &on, nil); err != nil {
		t.Fatalf("admin enable: %v", err)
	}
	got, _ = service.Get(ctx, credential.ID)
	if got.Disabled || got.DisabledBy != "" || got.DisabledReason != "" || got.DisabledAt != nil {
		t.Fatalf("admin enable must clear fully: %+v", got)
	}
	if err := service.UpdateDetails(ctx, "missing", &label, nil, nil); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("update details miss: got %v want not found", err)
	}
}

func TestGormDisableUnhealthyFirstWins(t *testing.T) {
	service, ctx := setupGormCredential(t)
	credential := addGormKey(t, service, ctx, "candidate")
	if err := service.DisableUnhealthy(ctx, credential.ID, "health check reported unhealthy"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := service.Get(ctx, credential.ID)
	if err != nil || !got.Disabled || got.DisabledBy != models.DisabledByHealthcheck || got.DisabledReason != "health check reported unhealthy" || got.DisabledAt == nil {
		t.Fatalf("unhealthy disable: %+v %v", got, err)
	}
	if err := service.DisableUnhealthy(ctx, credential.ID, "second"); err != nil {
		t.Fatalf("second disable: %v", err)
	}
	got, _ = service.Get(ctx, credential.ID)
	if got.DisabledReason != "health check reported unhealthy" {
		t.Fatalf("first reason wins, got %q", got.DisabledReason)
	}
	if err := service.DisableUnhealthy(ctx, "missing", "reason"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("disable miss: got %v want not found", err)
	}
}

func TestGormDisableByPluginFirstWins(t *testing.T) {
	service, ctx := setupGormCredential(t)
	credential := addGormKey(t, service, ctx, "candidate")
	if err := service.DisableByPlugin(ctx, credential.ID, "api key rejected"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := service.Get(ctx, credential.ID)
	if err != nil || !got.Disabled || got.DisabledBy != models.DisabledByPlugin || got.DisabledReason != "api key rejected" || got.DisabledAt == nil {
		t.Fatalf("plugin disable: %+v %v", got, err)
	}
	if err := service.DisableByPlugin(ctx, credential.ID, "second"); err != nil {
		t.Fatalf("second disable: %v", err)
	}
	got, _ = service.Get(ctx, credential.ID)
	if got.DisabledReason != "api key rejected" {
		t.Fatalf("first reason wins, got %q", got.DisabledReason)
	}
	off := true
	if err := service.UpdateDetails(ctx, credential.ID, nil, &off, nil); err != nil {
		t.Fatalf("admin disable: %v", err)
	}
	if err := service.EnableByPlugin(ctx, credential.ID); err != nil {
		t.Fatalf("enable: %v", err)
	}
	got, _ = service.Get(ctx, credential.ID)
	if !got.Disabled || got.DisabledBy != models.DisabledByAdmin {
		t.Fatalf("admin cause must survive plugin enable: %+v", got)
	}
}

func TestGormEnableByPluginClearsOnlyPluginCause(t *testing.T) {
	service, ctx := setupGormCredential(t)
	credential := addGormKey(t, service, ctx, "candidate")
	if err := service.EnableByPlugin(ctx, credential.ID); err != nil {
		t.Fatalf("enable enabled: %v", err)
	}
	if err := service.DisableByPlugin(ctx, credential.ID, "dead"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := service.EnableByPlugin(ctx, credential.ID); err != nil {
		t.Fatalf("enable: %v", err)
	}
	got, _ := service.Get(ctx, credential.ID)
	if got.Disabled || got.DisabledBy != "" || got.DisabledReason != "" || got.DisabledAt != nil {
		t.Fatalf("plugin cause must clear fully: %+v", got)
	}
	if err := service.EnableByPlugin(ctx, "missing"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("enable miss: got %v want not found", err)
	}
	if err := service.DisableByPlugin(ctx, "missing", "reason"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("disable miss: got %v want not found", err)
	}
}

func TestGormParkRoundTrip(t *testing.T) {
	service, ctx := setupGormCredential(t)
	credential := addGormKey(t, service, ctx, "candidate")
	if got, err := service.Parked(ctx, credential.ID); err != nil || got != nil {
		t.Fatalf("unparked must read nil: %+v %v", got, err)
	}
	if err := service.Park(ctx, credential.ID, time.Hour, "quota"); err != nil {
		t.Fatalf("park: %v", err)
	}
	got, err := service.Parked(ctx, credential.ID)
	if err != nil || got == nil || got.Reason != "quota" || got.CredentialID != credential.ID {
		t.Fatalf("parked: %+v %v", got, err)
	}
	if !got.Until.After(time.Now()) || got.ParkedAt.IsZero() {
		t.Fatalf("park times: %+v", got)
	}
	if err := service.Unpark(ctx, credential.ID); err != nil {
		t.Fatalf("unpark: %v", err)
	}
	if got, err := service.Parked(ctx, credential.ID); err != nil || got != nil {
		t.Fatalf("unparked must read nil: %+v %v", got, err)
	}
	if err := service.Unpark(ctx, credential.ID); err != nil {
		t.Fatalf("second unpark: %v", err)
	}
	if err := service.Park(ctx, "missing", time.Hour, "reason"); err == nil {
		t.Fatal("park of unknown identifier must fail")
	}
	if err := service.Park(ctx, credential.ID, 0, "reason"); err == nil {
		t.Fatal("non-positive ttl must fail")
	}
	if err := service.Park(ctx, credential.ID, 100*24*time.Hour, "reason"); err == nil {
		t.Fatal("overlong ttl must fail")
	}
}

func TestGormParkedExpiryDeletesOnRead(t *testing.T) {
	service, ctx := setupGormCredential(t)
	credential := addGormKey(t, service, ctx, "candidate")
	past := util.Now().Add(-time.Minute)
	stale := record.CredentialPark{CredentialID: credential.ID, Reason: "stale", Until: past, ParkedAt: past}
	if err := gorm.G[record.CredentialPark](service.database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "credential_id"}}, UpdateAll: true}).Create(ctx, &stale); err != nil {
		t.Fatalf("seed expired park: %v", err)
	}
	if got, err := service.Parked(ctx, credential.ID); err != nil || got != nil {
		t.Fatalf("lapsed park must read nil: %+v %v", got, err)
	}
	if _, err := gorm.G[record.CredentialPark](service.database.Handle, clause.Eq{Column: "credential_id", Value: credential.ID}).First(ctx); !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
		t.Fatalf("lapsed park must delete on read, got %v", err)
	}
}

func TestGormPurgeExpiredParks(t *testing.T) {
	service, ctx := setupGormCredential(t)
	live := addGormKey(t, service, ctx, "live")
	stale := addGormKey(t, service, ctx, "stale")
	if err := service.Park(ctx, live.ID, time.Hour, "quota"); err != nil {
		t.Fatalf("park live: %v", err)
	}
	past := util.Now().Add(-time.Minute)
	expired := record.CredentialPark{CredentialID: stale.ID, Reason: "stale", Until: past, ParkedAt: past}
	if err := gorm.G[record.CredentialPark](service.database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "credential_id"}}, UpdateAll: true}).Create(ctx, &expired); err != nil {
		t.Fatalf("seed expired park: %v", err)
	}
	removed, err := service.PurgeExpiredParks(ctx)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed count: got %d want 1", removed)
	}
	if got, err := service.Parked(ctx, live.ID); err != nil || got == nil {
		t.Fatalf("live park must survive purge: %+v %v", got, err)
	}
	if got, err := service.Parked(ctx, stale.ID); err != nil || got != nil {
		t.Fatalf("expired park must purge: %+v %v", got, err)
	}
	removed, err = service.PurgeExpiredParks(ctx)
	if err != nil {
		t.Fatalf("second purge: %v", err)
	}
	if removed != 0 {
		t.Fatalf("second purge count: got %d want 0", removed)
	}
}

func TestGormDelete(t *testing.T) {
	service, ctx := setupGormCredential(t)
	credential := addGormKey(t, service, ctx, "candidate")
	if err := service.Park(ctx, credential.ID, time.Hour, "quota"); err != nil {
		t.Fatalf("park: %v", err)
	}
	if err := service.Delete(ctx, credential.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := service.Get(ctx, credential.ID); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("get after delete: got %v want not found", err)
	}
	if _, err := gorm.G[record.CredentialPark](service.database.Handle, clause.Eq{Column: "credential_id", Value: credential.ID}).First(ctx); !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
		t.Fatalf("park must cascade on credential delete, got %v", err)
	}
	if err := service.Delete(ctx, credential.ID); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("second delete: got %v want not found", err)
	}
}

func TestGormDeleteByProvider(t *testing.T) {
	service, ctx := setupGormCredential(t)
	addGormKey(t, service, ctx, "one")
	addGormKey(t, service, ctx, "two")
	removed, err := service.DeleteByProvider(ctx, "mock")
	if err != nil {
		t.Fatalf("delete by provider: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed count: got %d want 2", removed)
	}
	remaining, err := service.ListByProvider(ctx, "mock")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected no remaining credentials, got %d", len(remaining))
	}
	removed, err = service.DeleteByProvider(ctx, "mock")
	if err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if removed != 0 {
		t.Fatalf("second delete count: got %d want 0", removed)
	}
}

func TestGormCredentialRecordRoundTrip(t *testing.T) {
	now := util.Now()
	expiresAt := now.Add(time.Hour)
	lastUsedAt := now.Add(-time.Minute)
	disabledAt := now
	original := &models.Credential{
		ID:             "round-trip",
		ProviderID:     "mock",
		Label:          "Round",
		Data:           map[string]any{"api_key": "key", "expiry": float64(3600)},
		ExpiresAt:      &expiresAt,
		LastUsedAt:     &lastUsedAt,
		RequestCount:   3,
		SuccessCount:   2,
		FailureCount:   1,
		Disabled:       true,
		DisabledBy:     models.DisabledByPlugin,
		DisabledReason: "dead key",
		DisabledAt:     &disabledAt,
		Order:          2,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	row := credentialRecordFromModel(original)
	if row.SortOrder != 2 {
		t.Fatalf("sort order: got %d want 2", row.SortOrder)
	}
	if row.Data["api_key"] != "key" || row.Data["expiry"] != float64(3600) {
		t.Fatalf("data passthrough: %+v", row.Data)
	}
	rebuilt := credentialModelFromRecord(row)
	if rebuilt.Order != 2 || rebuilt.RequestCount != 3 || rebuilt.SuccessCount != 2 || rebuilt.FailureCount != 1 {
		t.Fatalf("counters and order: %+v", rebuilt)
	}
	if rebuilt.Data["api_key"] != "key" {
		t.Fatalf("data roundtrip: %+v", rebuilt.Data)
	}
	if !rebuilt.Disabled || rebuilt.DisabledBy != models.DisabledByPlugin || rebuilt.DisabledReason != "dead key" || rebuilt.DisabledAt == nil {
		t.Fatalf("disabled state: %+v", rebuilt)
	}
	parkedAt := now
	entry := &models.ParkEntry{CredentialID: "round-trip", Reason: "quota", Until: now.Add(time.Hour), ParkedAt: parkedAt}
	parkRow := parkRecordFromModel(entry)
	rebuiltPark := parkModelFromRecord(parkRow)
	if rebuiltPark.CredentialID != "round-trip" || rebuiltPark.Reason != "quota" {
		t.Fatalf("park roundtrip: %+v", rebuiltPark)
	}
	if !rebuiltPark.Until.Equal(entry.Until.UTC()) || !rebuiltPark.ParkedAt.Equal(entry.ParkedAt.UTC()) {
		t.Fatalf("park times: %+v want %+v", rebuiltPark, entry)
	}
}

func TestGormMethodsFailClosedWithoutHandle(t *testing.T) {
	service := NewGorm(nil, nil)
	ctx := context.Background()
	if _, err := service.Add(ctx, AddOptions{ProviderID: "mock", Data: map[string]any{"api_key": "key"}}); err == nil {
		t.Error("add without handle must fail")
	}
	if _, err := service.Get(ctx, "identifier"); err == nil {
		t.Error("get without handle must fail")
	}
	if _, err := service.ListUsable(ctx, "mock"); err == nil {
		t.Error("list usable without handle must fail")
	}
	if err := service.Delete(ctx, "identifier"); err == nil {
		t.Error("delete without handle must fail")
	}
	if _, err := service.Parked(ctx, "identifier"); err == nil {
		t.Error("parked without handle must fail")
	}
}
