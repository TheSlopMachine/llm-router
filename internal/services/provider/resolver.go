package provider

import (
	"fmt"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
)

// Resolved is the dispatch bundle for one provider: its instance plus
// either a Lua plugin record or a built-in Go adapter.
type Resolved struct {
	Instance *models.ProviderInstance
	Plugin   *luaplugin.PluginRecord
	Go       GoAdapter
}

// IsLua reports whether the provider is served by a Lua plugin.
func (r *Resolved) IsLua() bool { return r.Plugin != nil }

// Resolve looks up a provider by ID and its backend in one operation.
func Resolve(svc *Service, providerID string) (*Resolved, error) {
	inst, err := svc.Get(providerID)
	if err != nil {
		return nil, fmt.Errorf("provider lookup failed: %w", err)
	}
	if goAdapter, ok := svc.GoAdapterFor(inst.TypeKey); ok {
		return &Resolved{Instance: inst, Go: goAdapter}, nil
	}
	luaSvc := svc.LuaService()
	if luaSvc == nil {
		return nil, fmt.Errorf("no backend for provider type %q: plugin service not wired", inst.TypeKey)
	}
	rec, err := luaSvc.Lookup(inst.TypeKey)
	if err != nil {
		return nil, fmt.Errorf("plugin lookup for type %q failed: %w", inst.TypeKey, err)
	}
	return &Resolved{Instance: inst, Plugin: rec}, nil
}
