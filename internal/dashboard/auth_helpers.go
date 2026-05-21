package dashboard

import (
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

func providerSupportsAuthFlow(typeKey string) bool {
	adapter, err := provider.Lookup(typeKey)
	if err != nil {
		return false
	}
	return adapter.GetAuthFlow() != nil
}

func buildAutoCredentialLabel(p *models.Provider, data map[string]string) string {
	providerName := "Provider"
	if p != nil && strings.TrimSpace(p.Name) != "" {
		providerName = strings.TrimSpace(p.Name)
	}

	methodName := credentialAuthMethodLabel(p, data)
	timestamp := util.Now().Format("2006-01-02 15:04")
	return providerName + " · " + methodName + " · " + timestamp
}

func credentialAuthMethodLabel(p *models.Provider, data map[string]string) string {
	if data == nil {
		return fallbackAuthMethodLabel(p)
	}

	raw := strings.TrimSpace(data["auth_method"])
	if raw != "" {
		if normalized := normalizeKnownAuthMethod(raw); normalized != "" {
			return normalized
		}
	}

	return fallbackAuthMethodLabel(p)
}

func fallbackAuthMethodLabel(p *models.Provider) string {
	if p == nil {
		return "Authenticated"
	}

	switch p.AuthType {
	case models.AuthTypeAPIKey:
		return "API Key"
	case models.AuthTypeOAuth2:
		return "OAuth"
	case models.AuthTypeBasic:
		return "Basic Auth"
	default:
		return "Authenticated"
	}
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
		return ""
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
