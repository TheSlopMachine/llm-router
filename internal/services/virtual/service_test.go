package virtual

import (
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func newVirtualTestStack(t *testing.T) *Service {
	t.Helper()
	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	providerSvc.RegisterGoAdapter(testutil.NewMockAdapter(provider.TypeVirtual).WithValidateFunc(func(map[string]any) error {
		return nil
	}))
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Demo", TypeKey: "demo"}); err != nil {
		t.Fatalf("seed demo provider: %v", err)
	}
	return New(database, providerSvc, modelInfoSvc)
}

func vmWithModel(name string) *models.VirtualModel {
	return &models.VirtualModel{
		Name:   name,
		Models: []models.VirtualModelEntry{{ModelID: "demo/model-a"}},
	}
}

func TestCreateAssignsSlugID(t *testing.T) {
	svc := newVirtualTestStack(t)

	vm := vmWithModel("My Helper")
	if err := svc.Create(vm); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if vm.ID != "my-helper" {
		t.Fatalf("id: got %q, want %q", vm.ID, "my-helper")
	}
	got, err := svc.Get("my-helper")
	if err != nil {
		t.Fatalf("get by slug failed: %v", err)
	}
	if got.Name != "My Helper" {
		t.Fatalf("name: got %q", got.Name)
	}
}

func TestCreateSlugCollisionSuffixes(t *testing.T) {
	svc := newVirtualTestStack(t)

	first := vmWithModel("My Helper")
	second := vmWithModel("My-Helper")
	if err := svc.Create(first); err != nil {
		t.Fatalf("create first failed: %v", err)
	}
	if err := svc.Create(second); err != nil {
		t.Fatalf("create second failed: %v", err)
	}
	if first.ID != "my-helper" || second.ID != "my-helper-2" {
		t.Fatalf("ids: got %q and %q", first.ID, second.ID)
	}
}

func TestCreateReservedSlugSuffixes(t *testing.T) {
	svc := newVirtualTestStack(t)

	vm := vmWithModel("New")
	if err := svc.Create(vm); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if vm.ID != "new-2" {
		t.Fatalf("reserved slug must suffix: got %q", vm.ID)
	}
}

func TestCreateUnusableNameFails(t *testing.T) {
	svc := newVirtualTestStack(t)

	if err := svc.Create(&models.VirtualModel{Name: "!!!"}); err == nil {
		t.Fatal("expected error for name with no usable characters")
	}
	if err := svc.Create(&models.VirtualModel{Name: "   "}); err == nil {
		t.Fatal("expected error for blank name")
	}
}

func TestCreateRequiresModel(t *testing.T) {
	svc := newVirtualTestStack(t)

	if err := svc.Create(&models.VirtualModel{Name: "Empty"}); err == nil {
		t.Fatal("expected error for virtual model without models")
	}
}

func TestCreateRejectsVirtualReference(t *testing.T) {
	svc := newVirtualTestStack(t)

	vm := &models.VirtualModel{
		Name:   "Loopy",
		Models: []models.VirtualModelEntry{{ModelID: "virtual/other"}},
	}
	if err := svc.Create(vm); err == nil {
		t.Fatal("expected error for virtual model referencing a virtual model")
	}
}

func TestRenameKeepsID(t *testing.T) {
	svc := newVirtualTestStack(t)

	vm := vmWithModel("Alpha")
	if err := svc.Create(vm); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	vm.Name = "Beta"
	if err := svc.Update("alpha", vm); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if vm.ID != "alpha" {
		t.Fatalf("rename must keep ID: got %q", vm.ID)
	}
	if _, err := svc.Get("alpha"); err != nil {
		t.Fatalf("get by old slug failed: %v", err)
	}
}
