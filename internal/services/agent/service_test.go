package agent

import (
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

func newAgentTestStack(t *testing.T) (*Service, *credential.Service, *token.Service, *provider.Service) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	providerSvc.RegisterGoAdapter(testutil.NewMockAdapter("agents").WithValidateFunc(func(map[string]any) error {
		return nil
	}))
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)
	agentSvc := New(database, providerSvc, modelInfoSvc)
	tokenSvc := token.New(database)
	return agentSvc, credSvc, tokenSvc, providerSvc
}

func TestCreateAssignsSlugID(t *testing.T) {
	agentSvc, _, _, _ := newAgentTestStack(t)

	a := &models.Agent{Name: "My Helper", IsDraft: true}
	if err := agentSvc.Create(a); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if a.ID != "my-helper" {
		t.Fatalf("id: got %q, want %q", a.ID, "my-helper")
	}
	got, err := agentSvc.Get("my-helper")
	if err != nil {
		t.Fatalf("get by slug failed: %v", err)
	}
	if got.Name != "My Helper" {
		t.Fatalf("name: got %q", got.Name)
	}
}

func TestCreateSlugCollisionSuffixes(t *testing.T) {
	agentSvc, _, _, _ := newAgentTestStack(t)

	first := &models.Agent{Name: "My Helper", IsDraft: true}
	second := &models.Agent{Name: "My-Helper", IsDraft: true}
	if err := agentSvc.Create(first); err != nil {
		t.Fatalf("create first failed: %v", err)
	}
	if err := agentSvc.Create(second); err != nil {
		t.Fatalf("create second failed: %v", err)
	}
	if first.ID != "my-helper" || second.ID != "my-helper-2" {
		t.Fatalf("ids: got %q and %q", first.ID, second.ID)
	}
}

func TestCreateReservedSlugSuffixes(t *testing.T) {
	agentSvc, _, _, _ := newAgentTestStack(t)

	a := &models.Agent{Name: "New", IsDraft: true}
	if err := agentSvc.Create(a); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if a.ID != "new-2" {
		t.Fatalf("reserved slug must suffix: got %q", a.ID)
	}
}

func TestCreateUnusableNameFails(t *testing.T) {
	agentSvc, _, _, _ := newAgentTestStack(t)

	if err := agentSvc.Create(&models.Agent{Name: "!!!"}); err == nil {
		t.Fatal("expected error for name with no usable characters")
	}
	if err := agentSvc.Create(&models.Agent{Name: "   "}); err == nil {
		t.Fatal("expected error for blank name")
	}
}

func TestRenameKeepsID(t *testing.T) {
	agentSvc, _, _, _ := newAgentTestStack(t)

	a := &models.Agent{Name: "Alpha", IsDraft: true}
	if err := agentSvc.Create(a); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	a.Name = "Beta"
	if err := agentSvc.Update("alpha", a); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if a.ID != "alpha" {
		t.Fatalf("rename must keep ID: got %q", a.ID)
	}
	if _, err := agentSvc.Get("alpha"); err != nil {
		t.Fatalf("get by old slug failed: %v", err)
	}
}

const migrateTestUUID = "9125971a-66d3-743a-2ac0-ab91c82012dc"

func seedUUIDEraAgent(t *testing.T, agentSvc *Service, credSvc *credential.Service, tokenSvc *token.Service, providerSvc *provider.Service) {
	t.Helper()
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Agents", TypeKey: "agents"}); err != nil {
		t.Fatalf("seed agents provider: %v", err)
	}
	now := util.Now()
	legacy := &models.Agent{
		ID:        migrateTestUUID,
		Name:      "Old One",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := agentSvc.repo.Put(legacy.ID, legacy); err != nil {
		t.Fatalf("seed legacy agent: %v", err)
	}
	if _, err := credSvc.Add(credential.AddOptions{
		ProviderID: "agents",
		Label:      "legacy",
		Data:       map[string]any{"agent_id": migrateTestUUID},
	}); err != nil {
		t.Fatalf("seed legacy credential: %v", err)
	}
	if _, err := tokenSvc.Create(token.CreateOptions{
		Name: "legacy-token",
		Rules: models.TokenRules{
			AllowAllProviders: true,
			AllowedModels:     []models.ModelId{models.ModelId("agents/" + migrateTestUUID)},
			AllowAllModels:    false,
		},
	}); err != nil {
		t.Fatalf("seed legacy token: %v", err)
	}
}

func TestMigrateIDsRewritesReferences(t *testing.T) {
	agentSvc, credSvc, tokenSvc, providerSvc := newAgentTestStack(t)
	seedUUIDEraAgent(t, agentSvc, credSvc, tokenSvc, providerSvc)

	n, err := agentSvc.MigrateIDs(credSvc, tokenSvc)
	if err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("migrated count: got %d, want 1", n)
	}

	got, err := agentSvc.Get("old-one")
	if err != nil {
		t.Fatalf("get by slug failed: %v", err)
	}
	if got.Name != "Old One" {
		t.Fatalf("name: got %q", got.Name)
	}
	if _, err := agentSvc.Get(migrateTestUUID); err == nil {
		t.Fatal("old UUID must not resolve after migration")
	}

	creds, err := credSvc.ListAll()
	if err != nil {
		t.Fatalf("list credentials: %v", err)
	}
	if len(creds) != 1 {
		t.Fatalf("credentials: got %d, want 1", len(creds))
	}
	if creds[0].Data["agent_id"] != "old-one" {
		t.Fatalf("credential agent_id: got %v", creds[0].Data["agent_id"])
	}

	tokens, err := tokenSvc.List()
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("tokens: got %d, want 1", len(tokens))
	}
	rules := tokens[0].Rules
	if len(rules.AllowedModels) != 1 || rules.AllowedModels[0] != "agents/old-one" {
		t.Fatalf("token models: got %v", rules.AllowedModels)
	}

	again, err := agentSvc.MigrateIDs(credSvc, tokenSvc)
	if err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}
	if again != 0 {
		t.Fatalf("second migrate must be a no-op, got %d", again)
	}
}

func TestMigrateIDsNoopWithoutLegacyRows(t *testing.T) {
	agentSvc, credSvc, tokenSvc, _ := newAgentTestStack(t)

	a := &models.Agent{Name: "Fresh", IsDraft: true}
	if err := agentSvc.Create(a); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	n, err := agentSvc.MigrateIDs(credSvc, tokenSvc)
	if err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if n != 0 {
		t.Fatalf("migrated count: got %d, want 0", n)
	}
}
