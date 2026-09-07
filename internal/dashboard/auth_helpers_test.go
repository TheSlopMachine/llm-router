package dashboard

import (
	"strings"
	"testing"
)

func TestBuildAutoCredentialLabelUsesKnownMethod(t *testing.T) {
	got := buildAutoCredentialLabel("Kiro AI", map[string]any{
		"auth_method": "builder-id",
	})

	if !strings.HasPrefix(got, "Kiro AI · Builder ID · ") {
		t.Fatalf("unexpected label: %q", got)
	}
}

func TestBuildAutoCredentialLabelFallsBack(t *testing.T) {
	got := buildAutoCredentialLabel("Google AI Studio", map[string]any{})

	if !strings.HasPrefix(got, "Google AI Studio · Authenticated · ") {
		t.Fatalf("unexpected label: %q", got)
	}
}

func TestBuildAutoCredentialLabelIncludesTimestamp(t *testing.T) {
	got := buildAutoCredentialLabel("Kiro AI", map[string]any{
		"auth_method": "idc",
	})

	if !strings.HasPrefix(got, "Kiro AI · IAM Identity Center · ") {
		t.Fatalf("unexpected label: %q", got)
	}
}
