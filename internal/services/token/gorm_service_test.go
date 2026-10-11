package token

import (
	"context"
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

func setupGormTokenService(t *testing.T) (*GormService, *db.Database) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	return NewGorm(database), database
}

func testContext() context.Context {
	return context.Background()
}

func TestGormService_Create_Get_Validate_List_Delete(t *testing.T) {
	service, _ := setupGormTokenService(t)
	ctx := testContext()

	created, err := service.Create(ctx, CreateOptions{
		Name:  "GORM Token",
		Rules: models.TokenRules{AllowAllProviders: true, AllowAllModels: true, AllowAllCredentials: true},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created.ID == "" || created.Token == "" || created.TokenHash == "" {
		t.Fatalf("create returned incomplete token: %+v", created)
	}

	fetched, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if fetched.Token != "" {
		t.Error("raw secret must not persist")
	}
	if !fetched.Rules.AllowAllProviders || !fetched.Rules.AllowAllModels || !fetched.Rules.AllowAllCredentials {
		t.Errorf("allow flags did not survive roundtrip: %+v", fetched.Rules)
	}

	validated, err := service.Validate(ctx, created.Token)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if validated.ID != created.ID {
		t.Errorf("validated identifier: got %q, want %q", validated.ID, created.ID)
	}

	listed, err := service.List(ctx)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("list length: got %d, want 1", len(listed))
	}

	if err := service.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := service.Get(ctx, created.ID); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Errorf("get after delete: want not-found, got %v", err)
	}
	if _, err := service.Validate(ctx, created.Token); !stdErrors.Is(err, apierrors.ErrUnauthorized) {
		t.Errorf("validate after delete: want unauthorized, got %v", err)
	}
}

func TestGormService_AllowedModels_SoftReferenceRoundtrip(t *testing.T) {
	service, _ := setupGormTokenService(t)
	ctx := testContext()

	created, err := service.Create(ctx, CreateOptions{
		Name: "Model Token",
		Rules: models.TokenRules{
			AllowAllProviders: true,
			AllowedModels:     []models.ModelId{"openai/gpt-4", "anthropic/claude-3"},
		},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	fetched, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(fetched.Rules.AllowedModels) != 2 {
		t.Fatalf("allowed models: got %d, want 2 (%+v)", len(fetched.Rules.AllowedModels), fetched.Rules)
	}
	if fetched.Rules.AllowedModels[0] != "anthropic/claude-3" || fetched.Rules.AllowedModels[1] != "openai/gpt-4" {
		t.Errorf("models sort order: got %q", fetched.Rules.AllowedModels)
	}
}

func TestGormService_UpdateRules_ReplacesJoins(t *testing.T) {
	service, _ := setupGormTokenService(t)
	ctx := testContext()

	created, err := service.Create(ctx, CreateOptions{
		Name:  "Mutable Token",
		Rules: models.TokenRules{AllowAllProviders: true, AllowAllModels: true, AllowAllCredentials: true},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	next := models.TokenRules{AllowedModels: []models.ModelId{"openai/gpt-4"}}
	if err := service.UpdateRules(ctx, created.ID, next); err != nil {
		t.Fatalf("update rules failed: %v", err)
	}
	updated, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if updated.Rules.AllowAllModels || updated.Rules.AllowAllProviders {
		t.Errorf("allow flags should clear: %+v", updated.Rules)
	}
	if len(updated.Rules.AllowedModels) != 1 || updated.Rules.AllowedModels[0] != "openai/gpt-4" {
		t.Errorf("models after update: got %q", updated.Rules.AllowedModels)
	}
	if err := service.UpdateRules(ctx, "missing-token", next); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Errorf("update missing: want not-found, got %v", err)
	}
}

func TestGormService_Regenerate_KeepsRules(t *testing.T) {
	service, _ := setupGormTokenService(t)
	ctx := testContext()

	created, err := service.Create(ctx, CreateOptions{
		Name:  "Rotate Token",
		Rules: models.TokenRules{AllowAllProviders: true, AllowAllModels: true, AllowAllCredentials: true},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	rotated, err := service.Regenerate(ctx, created.ID)
	if err != nil {
		t.Fatalf("regenerate failed: %v", err)
	}
	if rotated.Token == "" || rotated.Token == created.Token {
		t.Fatalf("regenerate must mint a fresh secret")
	}
	if _, err := service.Validate(ctx, created.Token); !stdErrors.Is(err, apierrors.ErrUnauthorized) {
		t.Errorf("old secret: want unauthorized, got %v", err)
	}
	if _, err := service.Validate(ctx, rotated.Token); err != nil {
		t.Errorf("fresh secret validate failed: %v", err)
	}
	if _, err := service.Regenerate(ctx, "missing-token"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Errorf("regenerate missing: want not-found, got %v", err)
	}
}

func TestGormService_Errors(t *testing.T) {
	service, _ := setupGormTokenService(t)
	ctx := testContext()

	if _, err := service.Validate(ctx, "no-such-secret"); !stdErrors.Is(err, apierrors.ErrUnauthorized) {
		t.Errorf("validate unknown: want unauthorized, got %v", err)
	}
	if _, err := service.Get(ctx, "missing-token"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Errorf("get missing: want not-found, got %v", err)
	}
	if err := service.Delete(ctx, "missing-token"); !stdErrors.Is(err, apierrors.ErrNotFound) {
		t.Errorf("delete missing: want not-found, got %v", err)
	}
}

func TestGormService_AllowedProviders_ForeignKey(t *testing.T) {
	service, database := setupGormTokenService(t)
	ctx := testContext()

	_, err := service.Create(ctx, CreateOptions{
		Name:  "Dangling Provider Token",
		Rules: models.TokenRules{AllowedProviders: []string{"missing-provider"}},
	})
	if !stdErrors.Is(err, apierrors.ErrInvalidInput) {
		t.Fatalf("dangling provider: want invalid-input, got %v", err)
	}

	provider := record.ProviderInstance{
		ID:        "missing-provider",
		Name:      "Missing Provider",
		TypeKey:   "missing-provider",
		CreatedAt: util.Now(),
		UpdatedAt: util.Now(),
	}
	if err := gorm.G[record.ProviderInstance](database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(ctx, &provider); err != nil {
		t.Fatalf("seed provider failed: %v", err)
	}
	created, err := service.Create(ctx, CreateOptions{
		Name:  "Pinned Provider Token",
		Rules: models.TokenRules{AllowedProviders: []string{"missing-provider"}},
	})
	if err != nil {
		t.Fatalf("create with provider failed: %v", err)
	}
	fetched, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(fetched.Rules.AllowedProviders) != 1 || fetched.Rules.AllowedProviders[0] != "missing-provider" {
		t.Errorf("providers roundtrip: got %q", fetched.Rules.AllowedProviders)
	}
}

func TestGormService_CredentialScopes_Roundtrip(t *testing.T) {
	service, database := setupGormTokenService(t)
	ctx := testContext()

	provider := record.ProviderInstance{
		ID:        "scope-provider",
		Name:      "Scope Provider",
		TypeKey:   "scope-provider",
		CreatedAt: util.Now(),
		UpdatedAt: util.Now(),
	}
	if err := gorm.G[record.ProviderInstance](database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(ctx, &provider); err != nil {
		t.Fatalf("seed provider failed: %v", err)
	}
	credential := record.Credential{
		ID:         "scope-credential-one",
		ProviderID: "scope-provider",
		Label:      "Scope Credential",
		CreatedAt:  util.Now(),
		UpdatedAt:  util.Now(),
	}
	if err := gorm.G[record.Credential](database.Handle, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(ctx, &credential); err != nil {
		t.Fatalf("seed credential failed: %v", err)
	}

	created, err := service.Create(ctx, CreateOptions{
		Name: "Scoped Token",
		Rules: models.TokenRules{
			AllowAllProviders: true,
			AllowedModels:     []models.ModelId{"scope-provider/scoped-model"},
			CredentialScopes: []models.CredentialScope{
				{ProviderID: "scope-provider", CredentialIDs: []string{"scope-credential-one"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	fetched, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(fetched.Rules.CredentialScopes) != 1 {
		t.Fatalf("scopes: got %d, want 1 (%+v)", len(fetched.Rules.CredentialScopes), fetched.Rules)
	}
	scope := fetched.Rules.CredentialScopes[0]
	if scope.ProviderID != "scope-provider" || scope.All {
		t.Errorf("scope header: got %+v", scope)
	}
	if len(scope.CredentialIDs) != 1 || scope.CredentialIDs[0] != "scope-credential-one" {
		t.Errorf("scope credentials: got %q", scope.CredentialIDs)
	}
	if !fetched.Rules.AllowsCredential("scope-provider", "scope-credential-one") {
		t.Error("reloaded scope should allow the pinned credential")
	}
}

func TestTokenRulesToJoinRows_MergesDuplicateScopes(t *testing.T) {
	rules := models.TokenRules{
		AllowedProviders:   []string{"beta", "alpha", "beta"},
		AllowedModels:      []models.ModelId{"openai/z", "openai/a", "openai/a"},
		AllowedCredentials: []string{"credential-b", "credential-a", "credential-a"},
		CredentialScopes: []models.CredentialScope{
			{ProviderID: "openai", CredentialIDs: []string{"credential-b"}},
			{ProviderID: "openai", All: true, CredentialIDs: []string{"credential-a", "credential-b"}},
		},
	}
	providers, modelRows, credentials, scopes, scopeCredentials, err := tokenRulesToJoinRows("token-one", rules)
	if err != nil {
		t.Fatalf("convert failed: %v", err)
	}
	if len(providers) != 2 || providers[0].ProviderID != "alpha" || providers[1].ProviderID != "beta" {
		t.Errorf("providers dedup sort: got %+v", providers)
	}
	if len(modelRows) != 2 || modelRows[0].ModelID != "openai/a" {
		t.Errorf("models dedup sort: got %+v", modelRows)
	}
	if len(credentials) != 2 || credentials[0].CredentialID != "credential-a" {
		t.Errorf("credentials dedup sort: got %+v", credentials)
	}
	if len(scopes) != 1 || !scopes[0].AllCredentials || scopes[0].ProviderID != "openai" {
		t.Fatalf("scopes merge: got %+v", scopes)
	}
	if len(scopeCredentials) != 2 {
		t.Errorf("scope credentials union: got %+v", scopeCredentials)
	}
	rebuilt := tokenRulesFromJoinRows(
		record.RouterToken{ID: "token-one"},
		providers, modelRows, credentials, scopes, scopeCredentials,
	)
	if len(rebuilt.CredentialScopes) != 1 || !rebuilt.CredentialScopes[0].All {
		t.Errorf("rebuilt scopes: got %+v", rebuilt.CredentialScopes)
	}
}
