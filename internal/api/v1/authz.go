package v1

import (
	"fmt"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// authorizationDeny describes one rejected token-rules check.
type authorizationDeny struct {
	status    int
	code      string
	message   string
	parameter *string
}

// authorizeModel enforces token rules for one request model. Empty models
// and nil tokens skip checks; everything else maps to provider/model denies.
func authorizeModel(token *models.RouterToken, model models.ModelId) *authorizationDeny {
	if token == nil || model == "" {
		return nil
	}
	providerID, _, _ := model.Parse()
	if providerID != "" && !token.Rules.AllowsProvider(providerID) {
		return &authorizationDeny{
			status:    http.StatusForbidden,
			code:      "provider_not_allowed",
			message:   fmt.Sprintf("provider %q is not allowed by your token's rules", providerID),
			parameter: stringPointer("model"),
		}
	}
	if !token.Rules.Allows(model) {
		return &authorizationDeny{
			status:    http.StatusForbidden,
			code:      "model_not_allowed",
			message:   fmt.Sprintf("model %q is not allowed by your token's rules", model),
			parameter: stringPointer("model"),
		}
	}
	return nil
}
