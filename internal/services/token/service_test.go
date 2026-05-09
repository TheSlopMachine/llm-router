package token

import (
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func setupTokenService(t *testing.T) *Service {
	t.Helper()
	database := testutil.SetupTestDB(t)
	return New(database)
}

// ─────────────────────────────────────────────
// Create Tests
// ─────────────────────────────────────────────

func TestTokenService_Create_GeneratesToken(t *testing.T) {
	svc := setupTokenService(t)
	
	token, err := svc.Create(CreateOptions{
		Name:  "Test Token",
		Rules: models.TokenRules{AllowedModels: []models.ModelId{"*"}},
	})
	
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	
	if token.ID == "" {
		t.Error("token ID should not be empty")
	}
	if token.Token == "" {
		t.Error("token value should not be empty")
	}
	if token.TokenHash == "" {
		t.Error("token hash should not be empty")
	}
	if token.Name != "Test Token" {
		t.Errorf("name: got %q, want %q", token.Name, "Test Token")
	}
}

func TestTokenService_Create_WithRules(t *testing.T) {
	svc := setupTokenService(t)
	
	rules := models.TokenRules{
		AllowedModels: []models.ModelId{"openai/gpt-4", "anthropic/claude-3"},
	}
	
	token, err := svc.Create(CreateOptions{
		Name:  "Restricted Token",
		Rules: rules,
	})
	
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	
	if len(token.Rules.AllowedModels) != 2 {
		t.Errorf("allowed models: got %d, want 2", len(token.Rules.AllowedModels))
	}
}

// ─────────────────────────────────────────────
// Validate Tests
// ─────────────────────────────────────────────

func TestTokenService_Validate_ValidToken(t *testing.T) {
	svc := setupTokenService(t)
	
	created, err := svc.Create(CreateOptions{
		Name:  "Test Token",
		Rules: models.TokenRules{AllowedModels: []models.ModelId{"*"}},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	
	// Validate using the raw token
	validated, err := svc.Validate(created.Token)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	
	if validated.ID != created.ID {
		t.Errorf("ID: got %q, want %q", validated.ID, created.ID)
	}
	if validated.Name != created.Name {
		t.Errorf("name: got %q, want %q", validated.Name, created.Name)
	}
}

func TestTokenService_Validate_InvalidToken(t *testing.T) {
	svc := setupTokenService(t)
	
	_, err := svc.Validate("invalid-token-value")
	if err == nil {
		t.Error("expected error for invalid token, got nil")
	}
}

func TestTokenService_Validate_EmptyToken(t *testing.T) {
	svc := setupTokenService(t)
	
	_, err := svc.Validate("")
	if err == nil {
		t.Error("expected error for empty token, got nil")
	}
}

// ─────────────────────────────────────────────
// Get Tests
// ─────────────────────────────────────────────

func TestTokenService_Get_Exists(t *testing.T) {
	svc := setupTokenService(t)
	
	created, err := svc.Create(CreateOptions{
		Name:  "Test Token",
		Rules: models.TokenRules{AllowedModels: []models.ModelId{"*"}},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	
	got, err := svc.Get(created.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	
	if got.ID != created.ID {
		t.Errorf("ID: got %q, want %q", got.ID, created.ID)
	}
	if got.Token != "" {
		t.Error("raw token should not be stored")
	}
}

func TestTokenService_Get_NotFound(t *testing.T) {
	svc := setupTokenService(t)
	
	_, err := svc.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent token, got nil")
	}
}

// ─────────────────────────────────────────────
// List Tests
// ─────────────────────────────────────────────

func TestTokenService_List_Empty(t *testing.T) {
	svc := setupTokenService(t)
	
	tokens, err := svc.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	
	if len(tokens) != 0 {
		t.Errorf("expected empty list, got %d tokens", len(tokens))
	}
}

func TestTokenService_List_Multiple(t *testing.T) {
	svc := setupTokenService(t)
	
	// Create multiple tokens
	for i := 0; i < 3; i++ {
		_, err := svc.Create(CreateOptions{
			Name:  "Token " + string(rune('A'+i)),
			Rules: models.TokenRules{AllowedModels: []models.ModelId{"*"}},
		})
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}
	}
	
	tokens, err := svc.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	
	if len(tokens) != 3 {
		t.Errorf("expected 3 tokens, got %d", len(tokens))
	}
}

// ─────────────────────────────────────────────
// Delete Tests
// ─────────────────────────────────────────────

func TestTokenService_Delete_Exists(t *testing.T) {
	svc := setupTokenService(t)
	
	created, err := svc.Create(CreateOptions{
		Name:  "Test Token",
		Rules: models.TokenRules{AllowedModels: []models.ModelId{"*"}},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	
	// Delete the token
	if err := svc.Delete(created.ID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	
	// Verify it's gone
	_, err = svc.Get(created.ID)
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
	
	// Verify validation fails
	_, err = svc.Validate(created.Token)
	if err == nil {
		t.Error("expected validation to fail after delete, got nil")
	}
}

func TestTokenService_Delete_NotFound(t *testing.T) {
	svc := setupTokenService(t)
	
	err := svc.Delete("nonexistent")
	if err == nil {
		t.Error("expected error for deleting nonexistent token, got nil")
	}
}

// ─────────────────────────────────────────────
// UpdateRules Tests
// ─────────────────────────────────────────────

func TestTokenService_UpdateRules(t *testing.T) {
	svc := setupTokenService(t)
	
	created, err := svc.Create(CreateOptions{
		Name:  "Test Token",
		Rules: models.TokenRules{AllowedModels: []models.ModelId{"*"}},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	
	// Update rules
	newRules := models.TokenRules{
		AllowedModels: []models.ModelId{"openai/gpt-4"},
	}
	if err := svc.UpdateRules(created.ID, newRules); err != nil {
		t.Fatalf("update rules failed: %v", err)
	}
	
	// Verify update
	updated, err := svc.Get(created.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	
	if len(updated.Rules.AllowedModels) != 1 {
		t.Errorf("allowed models: got %d, want 1", len(updated.Rules.AllowedModels))
	}
	if updated.Rules.AllowedModels[0] != "openai/gpt-4" {
		t.Errorf("allowed model: got %q, want %q", updated.Rules.AllowedModels[0], "openai/gpt-4")
	}
}

func TestTokenService_UpdateRules_NotFound(t *testing.T) {
	svc := setupTokenService(t)
	
	newRules := models.TokenRules{
		AllowedModels: []models.ModelId{"openai/gpt-4"},
	}
	err := svc.UpdateRules("nonexistent", newRules)
	if err == nil {
		t.Error("expected error for updating nonexistent token, got nil")
	}
}

// ─────────────────────────────────────────────
// Token Rules Tests
// ─────────────────────────────────────────────

func TestTokenService_TokenRules_Allows(t *testing.T) {
	svc := setupTokenService(t)
	
	rules := models.TokenRules{
		AllowedModels: []models.ModelId{"openai/gpt-4", "anthropic/claude-3"},
	}
	
	token, err := svc.Create(CreateOptions{
		Name:  "Restricted Token",
		Rules: rules,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	
	// Test allowed models
	if !token.Rules.Allows("openai/gpt-4") {
		t.Error("should allow openai/gpt-4")
	}
	if !token.Rules.Allows("anthropic/claude-3") {
		t.Error("should allow anthropic/claude-3")
	}
	
	// Test disallowed model
	if token.Rules.Allows("demo/test-model") {
		t.Error("should not allow demo/test-model")
	}
}

func TestTokenService_TokenRules_AllowsAll(t *testing.T) {
	svc := setupTokenService(t)
	
	rules := models.TokenRules{
		AllowedModels: []models.ModelId{},
	}
	
	token, err := svc.Create(CreateOptions{
		Name:  "Unrestricted Token",
		Rules: rules,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	
	// Empty list should allow all models
	if !token.Rules.Allows("openai/gpt-4") {
		t.Error("should allow openai/gpt-4")
	}
	if !token.Rules.Allows("demo/test-model") {
		t.Error("should allow demo/test-model")
	}
}

