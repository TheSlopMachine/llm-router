package virtual

import (
	"context"
	"strings"
	"testing"

	stdErrors "errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

func setupGormVirtual(t *testing.T) (*GormService, *db.Database, context.Context) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()
	seedGormVirtualProvider(t, ctx, database, "demo", "Demo")
	seedGormVirtualProvider(t, ctx, database, "other", "Other")
	return NewGorm(database, nil, nil), database, ctx
}

func seedGormVirtualProvider(t *testing.T, ctx context.Context, database *db.Database, identifier, name string) {
	t.Helper()
	now := util.Now()
	row := record.ProviderInstance{ID: identifier, Name: name, TypeKey: identifier, Settings: map[string]any{}, CreatedAt: now, UpdatedAt: now}
	if err := gorm.G[record.ProviderInstance](database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(ctx, &row); err != nil {
		t.Fatalf("seed provider %q: %v", identifier, err)
	}
}

func gormVirtualModel(name string, modelIDs ...string) *models.VirtualModel {
	entries := make([]models.VirtualModelEntry, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		entries = append(entries, models.VirtualModelEntry{ModelID: models.ModelId(modelID)})
	}
	return &models.VirtualModel{Name: name, Models: entries}
}

func gormVirtualMemberCount(t *testing.T, ctx context.Context, database *db.Database, virtualModelID string) int {
	t.Helper()
	members, err := gorm.G[record.VirtualModelMember](database.Handle, clause.Eq{Column: "virtual_model_id", Value: virtualModelID}).Find(ctx)
	if err != nil {
		t.Fatalf("count members: %v", err)
	}
	return len(members)
}

func TestGormCreateGetRoundTrip(t *testing.T) {
	service, _, ctx := setupGormVirtual(t)
	virtualModel := gormVirtualModel("My Helper", "demo/model-a")
	virtualModel.Description = "helps out"
	if err := service.Create(ctx, virtualModel); err != nil {
		t.Fatalf("create: %v", err)
	}
	if virtualModel.ID != "my-helper" {
		t.Fatalf("identifier: got %q want %q", virtualModel.ID, "my-helper")
	}
	if virtualModel.Version != 1 {
		t.Fatalf("version: got %d want 1", virtualModel.Version)
	}
	if virtualModel.CreatedAt.IsZero() || virtualModel.UpdatedAt.IsZero() {
		t.Fatal("timestamps must stamp")
	}
	fetched, err := service.Get(ctx, "my-helper")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Name != "My Helper" || fetched.Description != "helps out" {
		t.Fatalf("fetched fields: %+v", fetched)
	}
	if len(fetched.Models) != 1 || string(fetched.Models[0].ModelID) != "demo/model-a" {
		t.Fatalf("fetched members: %+v", fetched.Models)
	}
	if fetched.Version != 1 {
		t.Fatalf("fetched version: got %d want 1", fetched.Version)
	}
}

func TestGormCreateMemberOrder(t *testing.T) {
	service, _, ctx := setupGormVirtual(t)
	virtualModel := gormVirtualModel("Ordered", "demo/model-c", "demo/model-a", "other/model-b")
	if err := service.Create(ctx, virtualModel); err != nil {
		t.Fatalf("create: %v", err)
	}
	fetched, err := service.Get(ctx, virtualModel.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	want := []string{"demo/model-c", "demo/model-a", "other/model-b"}
	if len(fetched.Models) != len(want) {
		t.Fatalf("members: got %+v want %v", fetched.Models, want)
	}
	for index, entry := range fetched.Models {
		if string(entry.ModelID) != want[index] {
			t.Fatalf("member %d: got %q want %q", index, entry.ModelID, want[index])
		}
	}
}

func TestGormCreateSlugCollisionSuffixes(t *testing.T) {
	service, _, ctx := setupGormVirtual(t)
	first := gormVirtualModel("My Helper", "demo/model-a")
	second := gormVirtualModel("My-Helper", "demo/model-a")
	if err := service.Create(ctx, first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if err := service.Create(ctx, second); err != nil {
		t.Fatalf("create second: %v", err)
	}
	if first.ID != "my-helper" || second.ID != "my-helper-2" {
		t.Fatalf("identifiers: got %q and %q", first.ID, second.ID)
	}
}

func TestGormCreateReservedSlugSuffixes(t *testing.T) {
	service, _, ctx := setupGormVirtual(t)
	virtualModel := gormVirtualModel("New", "demo/model-a")
	if err := service.Create(ctx, virtualModel); err != nil {
		t.Fatalf("create: %v", err)
	}
	if virtualModel.ID != "new-2" {
		t.Fatalf("reserved slug must suffix: got %q", virtualModel.ID)
	}
}

func TestGormCreateValidation(t *testing.T) {
	service, _, ctx := setupGormVirtual(t)
	cases := []struct {
		name         string
		virtualModel *models.VirtualModel
		wantFragment string
	}{
		{"blank name", &models.VirtualModel{Name: "   ", Models: []models.VirtualModelEntry{{ModelID: "demo/model-a"}}}, "name is required"},
		{"unusable name", &models.VirtualModel{Name: "!!!", Models: []models.VirtualModelEntry{{ModelID: "demo/model-a"}}}, "no usable characters"},
		{"no models", &models.VirtualModel{Name: "Empty"}, "at least one model"},
		{"invalid model id", gormVirtualModel("Bad", "no-slash"), "invalid model ID"},
		{"virtual reference", gormVirtualModel("Loopy", "virtual/other"), "cannot reference other virtual models"},
		{"agents reference", gormVirtualModel("Loopy", "agents/other"), "cannot reference other virtual models"},
		{"duplicate member", gormVirtualModel("Dup", "demo/model-a", "demo/model-a"), "duplicate model ID"},
		{"missing provider", gormVirtualModel("Ghost", "ghost/model-a"), `provider "ghost" not found`},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if err := service.Create(ctx, item.virtualModel); err == nil {
				t.Fatal("expected error")
			} else if !strings.Contains(err.Error(), item.wantFragment) {
				t.Fatalf("error %q must mention %q", err.Error(), item.wantFragment)
			}
		})
	}
}

func TestGormCreateNilFailsInvalidInput(t *testing.T) {
	service, _, ctx := setupGormVirtual(t)
	if err := service.Create(ctx, nil); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Fatalf("nil create: got %v want invalid input", err)
	}
	if err := service.Update(ctx, "missing", nil); !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Fatalf("nil update: got %v want invalid input", err)
	}
}

func TestGormNameUniquenessCaseInsensitive(t *testing.T) {
	service, _, ctx := setupGormVirtual(t)
	if err := service.Create(ctx, gormVirtualModel("Chat", "demo/model-a")); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if err := service.Create(ctx, gormVirtualModel("CHAT", "demo/model-a")); !stdErrors.Is(err, apierrors.ErrAlreadyExists) {
		t.Fatalf("duplicate name: got %v want already exists", err)
	}
	second := gormVirtualModel("Other Name", "demo/model-a")
	if err := service.Create(ctx, second); err != nil {
		t.Fatalf("create second: %v", err)
	}
	second.Name = "chat"
	if err := service.Update(ctx, second.ID, second); !stdErrors.Is(err, apierrors.ErrAlreadyExists) {
		t.Fatalf("rename onto existing: got %v want already exists", err)
	}
	second.Name = "Other Name"
	second.Description = "touched"
	if err := service.Update(ctx, second.ID, second); err != nil {
		t.Fatalf("same-name update must succeed: %v", err)
	}
}

func TestGormRenameKeepsID(t *testing.T) {
	service, _, ctx := setupGormVirtual(t)
	virtualModel := gormVirtualModel("Alpha", "demo/model-a")
	if err := service.Create(ctx, virtualModel); err != nil {
		t.Fatalf("create: %v", err)
	}
	virtualModel.Name = "Beta"
	if err := service.Update(ctx, "alpha", virtualModel); err != nil {
		t.Fatalf("update: %v", err)
	}
	if virtualModel.ID != "alpha" || virtualModel.Version != 2 {
		t.Fatalf("rename keeps id and bumps version: %+v", virtualModel)
	}
	if _, err := service.Get(ctx, "alpha"); err != nil {
		t.Fatalf("get by old slug: %v", err)
	}
}

func TestGormUpdateReplacesMembers(t *testing.T) {
	service, _, ctx := setupGormVirtual(t)
	virtualModel := gormVirtualModel("Swap", "demo/model-a", "demo/model-b")
	if err := service.Create(ctx, virtualModel); err != nil {
		t.Fatalf("create: %v", err)
	}
	virtualModel.Models = []models.VirtualModelEntry{{ModelID: "other/model-z"}, {ModelID: "demo/model-a"}}
	if err := service.Update(ctx, virtualModel.ID, virtualModel); err != nil {
		t.Fatalf("update: %v", err)
	}
	fetched, err := service.Get(ctx, virtualModel.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(fetched.Models) != 2 || string(fetched.Models[0].ModelID) != "other/model-z" || string(fetched.Models[1].ModelID) != "demo/model-a" {
		t.Fatalf("replaced members: %+v", fetched.Models)
	}
	if fetched.Version != 2 {
		t.Fatalf("version: got %d want 2", fetched.Version)
	}
	if !fetched.CreatedAt.Equal(virtualModel.CreatedAt) {
		t.Fatal("update must preserve creation time")
	}
}

func TestGormUpdateOptimisticLocking(t *testing.T) {
	service, _, ctx := setupGormVirtual(t)
	virtualModel := gormVirtualModel("Locked", "demo/model-a")
	if err := service.Create(ctx, virtualModel); err != nil {
		t.Fatalf("create: %v", err)
	}
	stale := gormVirtualModel("Locked v2", "demo/model-a")
	stale.Version = 99
	if err := service.Update(ctx, virtualModel.ID, stale); err == nil {
		t.Fatal("stale version must fail")
	}
	zero := gormVirtualModel("Locked v2", "demo/model-a")
	if err := service.Update(ctx, virtualModel.ID, zero); err != nil {
		t.Fatalf("zero version must pass: %v", err)
	}
	if zero.Version != 2 {
		t.Fatalf("version: got %d want 2", zero.Version)
	}
}

func TestGormMissesReturnNotFound(t *testing.T) {
	service, _, ctx := setupGormVirtual(t)
	if _, err := service.Get(ctx, "missing"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("get miss: got %v want not found", err)
	}
	if err := service.Update(ctx, "missing", gormVirtualModel("Missing", "demo/model-a")); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("update miss: got %v want not found", err)
	}
	if err := service.Delete(ctx, "missing"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("delete miss: got %v want not found", err)
	}
}

func TestGormListOrderedByID(t *testing.T) {
	service, _, ctx := setupGormVirtual(t)
	empty, err := service.List(ctx)
	if err != nil {
		t.Fatalf("empty list: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty list: got %d items", len(empty))
	}
	if err := service.Create(ctx, gormVirtualModel("Bravo", "demo/model-a")); err != nil {
		t.Fatalf("create bravo: %v", err)
	}
	if err := service.Create(ctx, gormVirtualModel("Alpha", "other/model-b")); err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	listed, err := service.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 || listed[0].ID != "alpha" || listed[1].ID != "bravo" {
		t.Fatalf("list sorted by id: %+v", listed)
	}
	if len(listed[1].Models) != 1 || string(listed[1].Models[0].ModelID) != "demo/model-a" {
		t.Fatalf("listed members: %+v", listed[1].Models)
	}
}

func TestGormDeleteCascadesMembers(t *testing.T) {
	service, database, ctx := setupGormVirtual(t)
	virtualModel := gormVirtualModel("Doomed", "demo/model-a", "demo/model-b")
	if err := service.Create(ctx, virtualModel); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := gormVirtualMemberCount(t, ctx, database, virtualModel.ID); got != 2 {
		t.Fatalf("members: got %d want 2", got)
	}
	if err := service.Delete(ctx, virtualModel.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := gormVirtualMemberCount(t, ctx, database, virtualModel.ID); got != 0 {
		t.Fatalf("members after delete: got %d want 0 cascade", got)
	}
	if _, err := service.Get(ctx, virtualModel.ID); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("get after delete: got %v want not found", err)
	}
}

func TestGormManagedMarkerRoundTrip(t *testing.T) {
	service, database, ctx := setupGormVirtual(t)
	virtualModel := gormVirtualModel("Demo Chat", "demo/model-a")
	virtualModel.ManagedBy = ProviderMarker("demo", "chat")
	if err := service.Create(ctx, virtualModel); err != nil {
		t.Fatalf("create: %v", err)
	}
	row, err := gorm.G[record.VirtualModel](database.Handle, clause.Eq{Column: "id", Value: virtualModel.ID}).First(ctx)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row.ManagedProviderID == nil || *row.ManagedProviderID != "demo" {
		t.Fatalf("managed provider column: %+v", row.ManagedProviderID)
	}
	if row.ManagedEndpointSlug == nil || *row.ManagedEndpointSlug != "chat" {
		t.Fatalf("managed slug column: %+v", row.ManagedEndpointSlug)
	}
	fetched, err := service.Get(ctx, virtualModel.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.ManagedBy != "provider:demo:chat" {
		t.Fatalf("managed marker: got %q", fetched.ManagedBy)
	}
}

func TestGormProviderDeleteCascadesManaged(t *testing.T) {
	service, database, ctx := setupGormVirtual(t)
	managed := gormVirtualModel("Demo Chat", "demo/model-a")
	managed.ManagedBy = ProviderMarker("demo", "chat")
	if err := service.Create(ctx, managed); err != nil {
		t.Fatalf("create managed: %v", err)
	}
	manual := gormVirtualModel("Manual", "other/model-b")
	if err := service.Create(ctx, manual); err != nil {
		t.Fatalf("create manual: %v", err)
	}
	if _, err := gorm.G[record.ProviderInstance](database.Handle, clause.Eq{Column: "id", Value: "demo"}).Delete(ctx); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	if _, err := service.Get(ctx, managed.ID); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("managed model must cascade, got %v", err)
	}
	if got := gormVirtualMemberCount(t, ctx, database, managed.ID); got != 0 {
		t.Fatalf("managed members: got %d want 0 cascade", got)
	}
	kept, err := service.Get(ctx, manual.ID)
	if err != nil {
		t.Fatalf("manual model must survive: %v", err)
	}
	if len(kept.Models) != 1 || string(kept.Models[0].ModelID) != "other/model-b" {
		t.Fatalf("manual members: %+v", kept.Models)
	}
}

func TestGormConverterRoundTrip(t *testing.T) {
	original := gormVirtualModel("Round", "demo/model-a", "other/model-b")
	original.Description = "desc"
	original.Instruction = "be nice"
	original.Disabled = true
	original.Version = 3
	original.CreatedAt = util.Now()
	original.UpdatedAt = util.Now()
	original.ManagedBy = ProviderMarker("demo", "chat")
	row, members, err := virtualRecordFromModel(original, "round")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if row.NameKey != "round" || row.Description != "desc" || row.Version != 3 || !row.Disabled {
		t.Fatalf("row fields: %+v", row)
	}
	if len(members) != 2 || members[0].Position != 0 || members[1].Position != 1 {
		t.Fatalf("member positions: %+v", members)
	}
	if members[0].ProviderID != "demo" || members[0].ModelName != "model-a" {
		t.Fatalf("member split: %+v", members[0])
	}
	rebuilt := virtualModelFromRecords(row, members)
	if rebuilt.Name != original.Name || rebuilt.ManagedBy != original.ManagedBy || rebuilt.Disabled != original.Disabled {
		t.Fatalf("rebuilt fields: %+v", rebuilt)
	}
	if len(rebuilt.Models) != 2 || string(rebuilt.Models[1].ModelID) != "other/model-b" {
		t.Fatalf("rebuilt members: %+v", rebuilt.Models)
	}
}

func TestGormConverterVirtualMemberSplit(t *testing.T) {
	original := gormVirtualModel("Nested", "virtual/other")
	_, members, err := virtualRecordFromModel(original, "nested")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(members) != 1 || members[0].ProviderID != "virtual" || members[0].ModelName != "other" {
		t.Fatalf("virtual member must split onto the virtual provider row: %+v", members)
	}
}

func TestGormConverterFailures(t *testing.T) {
	badMarker := gormVirtualModel("Bad", "demo/model-a")
	badMarker.ManagedBy = "provider:"
	if _, _, err := virtualRecordFromModel(badMarker, "bad"); err == nil {
		t.Fatal("invalid marker must fail")
	}
	badMember := gormVirtualModel("Bad", "no-slash")
	if _, _, err := virtualRecordFromModel(badMember, "bad"); err == nil {
		t.Fatal("invalid member must fail")
	}
}

func TestGormMethodsFailClosedWithoutHandle(t *testing.T) {
	service := NewGorm(nil, nil, nil)
	ctx := context.Background()
	if err := service.Create(ctx, gormVirtualModel("Name", "demo/model-a")); err == nil {
		t.Error("create without handle must fail")
	}
	if _, err := service.Get(ctx, "identifier"); err == nil {
		t.Error("get without handle must fail")
	}
	if _, err := service.List(ctx); err == nil {
		t.Error("list without handle must fail")
	}
	if err := service.Update(ctx, "identifier", gormVirtualModel("Name", "demo/model-a")); err == nil {
		t.Error("update without handle must fail")
	}
	if err := service.Delete(ctx, "identifier"); err == nil {
		t.Error("delete without handle must fail")
	}
}
