package models

import (
	"testing"
	"time"
)

// ─────────────────────────────────────────────
// ModelId Tests
// ─────────────────────────────────────────────

func TestModelId_Parse_Valid(t *testing.T) {
	tests := []struct {
		input      ModelId
		wantProv   string
		wantModel  string
	}{
		{"openai/gpt-4", "openai", "gpt-4"},
		{"anthropic/claude-3", "anthropic", "claude-3"},
		{"openai:azure/gpt-4", "openai:azure", "gpt-4"},
		{"demo/test-model", "demo", "test-model"},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			prov, model, err := tt.input.Parse()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if prov != tt.wantProv {
				t.Errorf("provider: got %q, want %q", prov, tt.wantProv)
			}
			if model != tt.wantModel {
				t.Errorf("model: got %q, want %q", model, tt.wantModel)
			}
		})
	}
}

func TestModelId_Parse_Invalid(t *testing.T) {
	tests := []struct {
		input ModelId
	}{
		{"invalid"},
		{"no-slash-here"},
		{""},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			_, _, err := tt.input.Parse()
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestModelId_ParseFull_WithoutQualifier(t *testing.T) {
	input := ModelId("openai/gpt-4")
	adapterType, qualifier, model, err := input.ParseFull()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapterType != "openai" {
		t.Errorf("adapterType: got %q, want %q", adapterType, "openai")
	}
	if qualifier != "" {
		t.Errorf("qualifier: got %q, want empty", qualifier)
	}
	if model != "gpt-4" {
		t.Errorf("model: got %q, want %q", model, "gpt-4")
	}
}

func TestModelId_ParseFull_WithQualifier(t *testing.T) {
	input := ModelId("openai:azure/gpt-4")
	adapterType, qualifier, model, err := input.ParseFull()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapterType != "openai" {
		t.Errorf("adapterType: got %q, want %q", adapterType, "openai")
	}
	if qualifier != "azure" {
		t.Errorf("qualifier: got %q, want %q", qualifier, "azure")
	}
	if model != "gpt-4" {
		t.Errorf("model: got %q, want %q", model, "gpt-4")
	}
}

// ─────────────────────────────────────────────
// TokenRules Tests
// ─────────────────────────────────────────────

func TestTokenRules_Allows_EmptyList(t *testing.T) {
	rules := TokenRules{AllowedModels: []ModelId{}}
	if !rules.Allows("openai/gpt-4") {
		t.Error("empty allowed list should allow all models")
	}
}

func TestTokenRules_Allows_Wildcard(t *testing.T) {
	rules := TokenRules{AllowedModels: nil}
	if !rules.Allows("openai/gpt-4") {
		t.Error("nil allowed list should allow all models")
	}
}

func TestTokenRules_Allows_Specific(t *testing.T) {
	rules := TokenRules{
		AllowedModels: []ModelId{"openai/gpt-4", "anthropic/claude-3"},
	}
	
	if !rules.Allows("openai/gpt-4") {
		t.Error("should allow openai/gpt-4")
	}
	if !rules.Allows("anthropic/claude-3") {
		t.Error("should allow anthropic/claude-3")
	}
}

func TestTokenRules_Allows_Denied(t *testing.T) {
	rules := TokenRules{
		AllowedModels: []ModelId{"openai/gpt-4"},
	}
	
	if rules.Allows("anthropic/claude-3") {
		t.Error("should not allow anthropic/claude-3")
	}
}

// ─────────────────────────────────────────────
// Credential Tests
// ─────────────────────────────────────────────

func TestCredential_IsExpired_NotExpired(t *testing.T) {
	future := time.Now().Add(1 * time.Hour)
	cred := &Credential{ExpiresAt: &future}
	if cred.IsExpired() {
		t.Error("credential should not be expired")
	}
}

func TestCredential_IsExpired_Expired(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour)
	cred := &Credential{ExpiresAt: &past}
	if !cred.IsExpired() {
		t.Error("credential should be expired")
	}
}

func TestCredential_IsExpired_NoExpiry(t *testing.T) {
	cred := &Credential{ExpiresAt: nil}
	if cred.IsExpired() {
		t.Error("credential with no expiry should not be expired")
	}
}

func TestCredential_IsQuotaExceeded_NotExceeded(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour)
	cred := &Credential{QuotaResetAt: &past}
	if cred.IsQuotaExceeded() {
		t.Error("quota should not be exceeded (reset time passed)")
	}
}

func TestCredential_IsQuotaExceeded_Exceeded(t *testing.T) {
	future := time.Now().Add(1 * time.Hour)
	cred := &Credential{QuotaResetAt: &future}
	if !cred.IsQuotaExceeded() {
		t.Error("quota should be exceeded")
	}
}

func TestCredential_IsQuotaExceeded_NoQuota(t *testing.T) {
	cred := &Credential{QuotaResetAt: nil}
	if cred.IsQuotaExceeded() {
		t.Error("credential with no quota should not be exceeded")
	}
}

func TestCredential_Priority_NeverUsed(t *testing.T) {
	cred := &Credential{LastUsedAt: nil}
	if cred.Priority() != 0 {
		t.Errorf("never used credential should have priority 0, got %d", cred.Priority())
	}
}

func TestCredential_Priority_Normal(t *testing.T) {
	now := time.Now()
	cred := &Credential{LastUsedAt: &now}
	if cred.Priority() != 1 {
		t.Errorf("normal credential should have priority 1, got %d", cred.Priority())
	}
}

func TestCredential_Priority_QuotaExceeded(t *testing.T) {
	now := time.Now()
	future := time.Now().Add(1 * time.Hour)
	cred := &Credential{
		LastUsedAt:   &now,
		QuotaResetAt: &future,
	}
	if cred.Priority() != 2 {
		t.Errorf("quota exceeded credential should have priority 2, got %d", cred.Priority())
	}
}

func TestCredential_Priority_Expired(t *testing.T) {
	now := time.Now()
	past := time.Now().Add(-1 * time.Hour)
	cred := &Credential{
		LastUsedAt: &now,
		ExpiresAt:  &past,
	}
	if cred.Priority() != 3 {
		t.Errorf("expired credential should have priority 3, got %d", cred.Priority())
	}
}

func TestCredential_Priority_BothExpiredAndQuota(t *testing.T) {
	now := time.Now()
	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)
	cred := &Credential{
		LastUsedAt:   &now,
		ExpiresAt:    &past,
		QuotaResetAt: &future,
	}
	if cred.Priority() != 3 {
		t.Errorf("expired credential should have priority 3 even with quota exceeded, got %d", cred.Priority())
	}
}

func TestCredential_IncrementUsage_Success(t *testing.T) {
	cred := &Credential{}
	cred.IncrementUsage(true)
	
	if cred.RequestCount != 1 {
		t.Errorf("request count: got %d, want 1", cred.RequestCount)
	}
	if cred.SuccessCount != 1 {
		t.Errorf("success count: got %d, want 1", cred.SuccessCount)
	}
	if cred.FailureCount != 0 {
		t.Errorf("failure count: got %d, want 0", cred.FailureCount)
	}
	if cred.LastUsedAt == nil {
		t.Error("last used should be set")
	}
}

func TestCredential_IncrementUsage_Failure(t *testing.T) {
	cred := &Credential{}
	cred.IncrementUsage(false)
	
	if cred.RequestCount != 1 {
		t.Errorf("request count: got %d, want 1", cred.RequestCount)
	}
	if cred.SuccessCount != 0 {
		t.Errorf("success count: got %d, want 0", cred.SuccessCount)
	}
	if cred.FailureCount != 1 {
		t.Errorf("failure count: got %d, want 1", cred.FailureCount)
	}
	if cred.LastUsedAt == nil {
		t.Error("last used should be set")
	}
}

func TestCredential_ExpiresIn_Future(t *testing.T) {
	future := time.Now().Add(1 * time.Hour)
	cred := &Credential{ExpiresAt: &future}
	duration := cred.ExpiresIn()
	if duration <= 0 {
		t.Errorf("expires in should be positive, got %v", duration)
	}
}

func TestCredential_ExpiresIn_Past(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour)
	cred := &Credential{ExpiresAt: &past}
	duration := cred.ExpiresIn()
	if duration >= 0 {
		t.Errorf("expires in should be negative, got %v", duration)
	}
}

func TestCredential_ExpiresIn_NoExpiry(t *testing.T) {
	cred := &Credential{ExpiresAt: nil}
	duration := cred.ExpiresIn()
	if duration != 0 {
		t.Errorf("expires in should be 0 for no expiry, got %v", duration)
	}
}

