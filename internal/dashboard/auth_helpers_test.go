package dashboard

import (
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestCredentialAuthMethodLabelUsesKnownMethod(t *testing.T) {
	providerRecord := &models.Provider{
		Name:     "Kiro AI",
		AuthType: models.AuthTypeOAuth2,
	}

	got := credentialAuthMethodLabel(providerRecord, map[string]string{
		"auth_method": "builder-id",
	})

	if got != "Builder ID" {
		t.Fatalf("expected Builder ID, got %q", got)
	}
}

func TestCredentialAuthMethodLabelFallsBackToAuthType(t *testing.T) {
	providerRecord := &models.Provider{
		Name:     "Google AI Studio",
		AuthType: models.AuthTypeAPIKey,
	}

	got := credentialAuthMethodLabel(providerRecord, map[string]string{})

	if got != "API Key" {
		t.Fatalf("expected API Key, got %q", got)
	}
}

func TestBuildAutoCredentialLabelIncludesProviderMethodAndTimestamp(t *testing.T) {
	providerRecord := &models.Provider{
		Name:     "Kiro AI",
		AuthType: models.AuthTypeOAuth2,
	}

	got := buildAutoCredentialLabel(providerRecord, map[string]string{
		"auth_method": "idc",
	})

	if !strings.HasPrefix(got, "Kiro AI · IAM Identity Center · ") {
		t.Fatalf("unexpected label: %q", got)
	}
}
