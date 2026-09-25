package luaplugin

import (
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// HandlerMeta carries request identity into handler calls: which provider
// instance and type serve the request, which credential attempt runs, which
// model was requested, and the provider config. Threaded from the router
// through the pool into every handler invocation, so identity never has to
// be re-derived from scattered arguments.
type HandlerMeta struct {
	ProviderID     string
	TypeKey        string
	Credential     *models.Credential
	Model          models.ModelId
	ProviderConfig map[string]any
}

// credentialID returns the attempt credential ID, empty when unpinned.
func (m HandlerMeta) credentialID() string {
	if m.Credential == nil {
		return ""
	}
	return m.Credential.ID
}
