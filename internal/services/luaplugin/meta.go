package luaplugin

import (
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// HandlerMeta carries request identity into handler calls: which provider
// instance and type serve the request, which model was requested, the
// provider config, and the token allow-list of credential IDs the plugin
// may use. A nil allow-list means unrestricted (admin probes); virtual
// fan-out recomputes it per member from the token snapshot in ctx.
type HandlerMeta struct {
	ProviderID         string
	TypeKey            string
	Credential         *models.Credential
	Model              models.ModelId
	ProviderConfig     map[string]any
	AllowedCredentials []string
}

// credentialID returns the attempt credential ID, empty when unpinned.
func (m HandlerMeta) credentialID() string {
	if m.Credential == nil {
		return ""
	}
	return m.Credential.ID
}
