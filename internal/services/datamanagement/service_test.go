package datamanagement

import (
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// dataManagementStack wires the services ImportProviders and PurgeProvider need: a
// Go-backed provider type so credential validation passes without Lua.
func dataManagementStack(t *testing.T) (*Service, *provider.Service, *credential.Service, *modelinfo.Service) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	providerService := provider.NewService(database)
	providerService.RegisterGoAdapter(testutil.NewMockAdapter("dm-type"))
	credentialService := credential.New(database, providerService)
	modelInfoService := modelinfo.New(database, providerService, credentialService, time.Hour)
	service := New(database, providerService, credentialService, nil, nil, nil, nil, modelInfoService)
	return service, providerService, credentialService, modelInfoService
}

func TestPurgeProvider_RemovesOverrides(t *testing.T) {
	service, providerService, _, modelInfoService := dataManagementStack(t)
	instance, err := providerService.Create(provider.CreateOptions{Name: "Purge", TypeKey: "dm-type", Qualifier: "q"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if err := modelInfoService.SetOverride(models.ModelOverride{ProviderID: instance.ID, Name: "m"}); err != nil {
		t.Fatalf("set override: %v", err)
	}
	if err := service.PurgeProvider(instance.ID); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if overrides, err := modelInfoService.ListOverrides(instance.ID); err != nil || len(overrides) != 0 {
		t.Fatalf("overrides after purge = %v, %v; want none", overrides, err)
	}
}

// TestImportProviders_RemapsCollisionIDs proves imports follow the created
// instance, not the bundle: when the bundle's ID is taken, Create mints a
// dedup suffix and credentials plus overrides must point at it.
func TestImportProviders_RemapsCollisionIDs(t *testing.T) {
	service, providerService, credentialService, modelInfoService := dataManagementStack(t)
	if _, err := providerService.Create(provider.CreateOptions{Name: "Occupant", TypeKey: "dm-type", Qualifier: "q"}); err != nil {
		t.Fatalf("create occupant: %v", err)
	}
	bundle := &ProviderBundle{
		Instance: &models.ProviderInstance{
			ID: "dm-type:elsewhere", Name: "X", TypeKey: "dm-type", Qualifier: "q",
		},
		Credentials: []*models.Credential{
			{ID: "cred-1", Label: "k", Data: map[string]any{"api_key": "v"}},
		},
		Overrides: []*models.ModelOverride{
			{ProviderID: "dm-type:elsewhere", Name: "m"},
		},
	}
	if err := service.ImportProviders([]*ProviderBundle{bundle}); err != nil {
		t.Fatalf("import: %v", err)
	}
	instance, err := providerService.GetByTypeAndQualifier("dm-type", "q")
	if err != nil {
		t.Fatalf("lookup occupant: %v", err)
	}
	if instance.ID != "dm-type:q" {
		t.Fatalf("occupant id = %q, want dm-type:q", instance.ID)
	}
	imported, err := providerService.Get("dm-type:q-2")
	if err != nil {
		t.Fatalf("imported instance must own the dedup ID dm-type:q-2: %v", err)
	}
	if imported.TypeKey != "dm-type" {
		t.Fatalf("imported type = %q, want dm-type", imported.TypeKey)
	}
	credentials, err := credentialService.ListByProvider("dm-type:q-2")
	if err != nil || len(credentials) != 1 {
		t.Fatalf("imported credentials = %v, %v; want one under dm-type:q-2", credentials, err)
	}
	overrides, err := modelInfoService.ListOverrides("dm-type:q-2")
	if err != nil || len(overrides) != 1 {
		t.Fatalf("imported overrides = %v, %v; want one under dm-type:q-2", overrides, err)
	}
}
