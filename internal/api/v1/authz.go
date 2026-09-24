package v1

import (
	"fmt"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// authDeny describes one rejected token-rules check.
type authDeny struct {
	status int
	code   string
	msg    string
	param  *string
}

// authorizeModel enforces token rules for one request model. Empty models
// and nil tokens skip checks; everything else maps to provider/model denies.
func authorizeModel(t *models.RouterToken, model models.ModelId) *authDeny {
	if t == nil || model == "" {
		return nil
	}
	providerID, _, _ := model.Parse()
	if providerID != "" && !t.Rules.AllowsProvider(providerID) {
		return &authDeny{
			status: http.StatusForbidden,
			code:   "provider_not_allowed",
			msg:    fmt.Sprintf("provider %q is not allowed by your token's rules", providerID),
			param:  strPtr("model"),
		}
	}
	if !t.Rules.Allows(model) {
		return &authDeny{
			status: http.StatusForbidden,
			code:   "model_not_allowed",
			msg:    fmt.Sprintf("model %q is not allowed by your token's rules", model),
			param:  strPtr("model"),
		}
	}
	return nil
}
