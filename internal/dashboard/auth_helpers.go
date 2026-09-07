package dashboard

import (
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/util"
)

func buildAutoCredentialLabel(providerID string, data map[string]any) string {
	name := strings.TrimSpace(providerID)
	if name == "" {
		name = "Provider"
	}
	method := "Authenticated"
	if data != nil {
		if raw, _ := data["auth_method"].(string); strings.TrimSpace(raw) != "" {
			method = normalizeKnownAuthMethod(raw)
		}
	}
	timestamp := util.Now().Format("2006-01-02 15:04")
	return name + " · " + method + " · " + timestamp
}

func normalizeKnownAuthMethod(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "builder-id", "builder_id", "builderid":
		return "Builder ID"
	case "idc", "iam-identity-center", "iam_identity_center", "identity-center", "identity_center":
		return "IAM Identity Center"
	case "api-key", "api_key", "apikey":
		return "API Key"
	case "oauth", "oauth2":
		return "OAuth"
	case "basic", "basic-auth", "basic_auth":
		return "Basic Auth"
	case "github":
		return "GitHub"
	case "google":
		return "Google"
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	if len(parts) == 0 {
		return "Authenticated"
	}

	for i, part := range parts {
		lower := strings.ToLower(strings.TrimSpace(part))
		switch lower {
		case "":
			continue
		case "id":
			parts[i] = "ID"
		case "api":
			parts[i] = "API"
		case "oauth":
			parts[i] = "OAuth"
		case "iam":
			parts[i] = "IAM"
		default:
			parts[i] = strings.ToUpper(lower[:1]) + lower[1:]
		}
	}

	return strings.Join(parts, " ")
}
