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
	providerService := provider.NewService(database)
	providerService.RegisterGoAdapter(testutil.NewMockAdapter(provider.TypeVirtual).WithValidateFunc(func(map[string]any) error {
		return nil
	}))
	credentialService := credential.New(database, providerService)
	modelInfoService := modelinfo.New(database, providerService, credentialService, 1*time.Hour)
	if _, err := providerService.Create(provider.CreateOptions{Name: "Demo", TypeKey: "demo"}); err != nil {
		t.Fatalf("seed demo provider: %v", err)
	}
	return New(database, providerService, modelInfoService)
}

func virtualModelWithModel(name string) *models.VirtualModel {
	return &models.VirtualModel{
		Name:   name,
		Models: []models.VirtualModelEntry{{ModelID: "demo/model-a"}},
	}
}

func TestCreateAssignsSlugID(t *testing.T) {
	service := newVirtualTestStack(t)

	virtualModel := virtualModelWithModel("My Helper")
	if err := service.Create(virtualModel); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if virtualModel.ID != "my-helper" {
		t.Fatalf("id: got %q, want %q", virtualModel.ID, "my-helper")
	}
	got, err := service.Get("my-helper")
	if err != nil {
		t.Fatalf("get by slug failed: %v", err)
	}
	if got.Name != "My Helper" {
		t.Fatalf("name: got %q", got.Name)
	}
}

func TestCreateSlugCollisionSuffixes(t *testing.T) {
	service := newVirtualTestStack(t)

	first := virtualModelWithModel("My Helper")
	second := virtualModelWithModel("My-Helper")
	if err := service.Create(first); err != nil {
		t.Fatalf("create first failed: %v", err)
	}
	if err := service.Create(second); err != nil {
		t.Fatalf("create second failed: %v", err)
	}
	if first.ID != "my-helper" || second.ID != "my-helper-2" {
		t.Fatalf("ids: got %q and %q", first.ID, second.ID)
	}
}

func TestCreateReservedSlugSuffixes(t *testing.T) {
	service := newVirtualTestStack(t)

	virtualModel := virtualModelWithModel("New")
	if err := service.Create(virtualModel); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if virtualModel.ID != "new-2" {
		t.Fatalf("reserved slug must suffix: got %q", virtualModel.ID)
	}
}

func TestCreateUnusableNameFails(t *testing.T) {
	service := newVirtualTestStack(t)

	if err := service.Create(&models.VirtualModel{Name: "!!!"}); err == nil {
		t.Fatal("expected error for name with no usable characters")
	}
	if err := service.Create(&models.VirtualModel{Name: "   "}); err == nil {
		t.Fatal("expected error for blank name")
	}
}

func TestCreateRequiresModel(t *testing.T) {
	service := newVirtualTestStack(t)

	if err := service.Create(&models.VirtualModel{Name: "Empty"}); err == nil {
		t.Fatal("expected error for virtual model without models")
	}
}

func TestCreateRejectsVirtualReference(t *testing.T) {
	service := newVirtualTestStack(t)

	virtualModel := &models.VirtualModel{
		Name:   "Loopy",
		Models: []models.VirtualModelEntry{{ModelID: "virtual/other"}},
	}
	if err := service.Create(virtualModel); err == nil {
		t.Fatal("expected error for virtual model referencing a virtual model")
	}
}

func TestRenameKeepsID(t *testing.T) {
	service := newVirtualTestStack(t)

	virtualModel := virtualModelWithModel("Alpha")
	if err := service.Create(virtualModel); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	virtualModel.Name = "Beta"
	if err := service.Update("alpha", virtualModel); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if virtualModel.ID != "alpha" {
		t.Fatalf("rename must keep ID: got %q", virtualModel.ID)
	}
	if _, err := service.Get("alpha"); err != nil {
		t.Fatalf("get by old slug failed: %v", err)
	}
}
