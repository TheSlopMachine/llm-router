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

// dmStack wires the services ImportProviders and PurgeProvider need: a
// Go-backed provider type so credential validation passes without Lua.
func dmStack(t *testing.T) (*Service, *provider.Service, *credential.Service, *modelinfo.Service) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	providerSvc.RegisterGoAdapter(testutil.NewMockAdapter("dm-type"))
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, time.Hour)
	svc := New(database, providerSvc, credSvc, nil, nil, nil, nil, modelInfoSvc, nil)
	return svc, providerSvc, credSvc, modelInfoSvc
}

func TestPurgeProvider_RemovesOverrides(t *testing.T) {
	svc, providerSvc, _, modelInfoSvc := dmStack(t)
	inst, err := providerSvc.Create(provider.CreateOptions{Name: "Purge", TypeKey: "dm-type", Qualifier: "q"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if err := modelInfoSvc.SetOverride(models.ModelOverride{ProviderID: inst.ID, Name: "m"}); err != nil {
		t.Fatalf("set override: %v", err)
	}
	if err := svc.PurgeProvider(inst.ID); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if ovs, err := modelInfoSvc.ListOverrides(inst.ID); err != nil || len(ovs) != 0 {
		t.Fatalf("overrides after purge = %v, %v; want none", ovs, err)
	}
}

// TestImportProviders_RemapsCollisionIDs proves imports follow the created
// instance, not the bundle: when the bundle's ID is taken, Create mints a
// dedup suffix and credentials plus overrides must point at it.
func TestImportProviders_RemapsCollisionIDs(t *testing.T) {
	svc, providerSvc, credSvc, modelInfoSvc := dmStack(t)
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Occupant", TypeKey: "dm-type", Qualifier: "q"}); err != nil {
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
	if err := svc.ImportProviders([]*ProviderBundle{bundle}); err != nil {
		t.Fatalf("import: %v", err)
	}
	inst, err := providerSvc.GetByTypeAndQualifier("dm-type", "q")
	if err != nil {
		t.Fatalf("lookup occupant: %v", err)
	}
	if inst.ID != "dm-type:q" {
		t.Fatalf("occupant id = %q, want dm-type:q", inst.ID)
	}
	imported, err := providerSvc.Get("dm-type:q-2")
	if err != nil {
		t.Fatalf("imported instance must own the dedup ID dm-type:q-2: %v", err)
	}
	if imported.TypeKey != "dm-type" {
		t.Fatalf("imported type = %q, want dm-type", imported.TypeKey)
	}
	creds, err := credSvc.ListByProvider("dm-type:q-2")
	if err != nil || len(creds) != 1 {
		t.Fatalf("imported credentials = %v, %v; want one under dm-type:q-2", creds, err)
	}
	ovs, err := modelInfoSvc.ListOverrides("dm-type:q-2")
	if err != nil || len(ovs) != 1 {
		t.Fatalf("imported overrides = %v, %v; want one under dm-type:q-2", ovs, err)
	}
}
