package provider

import (
	"context"
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
func Resolve(service *GormService, ctx context.Context, providerID string) (*Resolved, error) {
	inst, err := service.Get(ctx, providerID)
	if err != nil {
		if service != nil && service.logger != nil {
			service.logger.Error("Provider resolve failed", "provider_id", providerID, "error", err)
		}
		return nil, fmt.Errorf("provider lookup failed: %w", err)
	}
	if goAdapter, ok := service.GoAdapterFor(inst.TypeKey); ok {
		if service.logger != nil {
			service.logger.Debug("Provider resolved through Go adapter", "provider_id", inst.ID, "type", inst.TypeKey)
		}
		return &Resolved{Instance: inst, Go: goAdapter}, nil
	}
	luaService := service.LuaService()
	if luaService == nil {
		if service.logger != nil {
			service.logger.Error("Provider resolve failed", "provider_id", inst.ID, "type", inst.TypeKey, "reason", "plugin service not wired")
		}
		return nil, fmt.Errorf("no backend for provider type %q: plugin service not wired", inst.TypeKey)
	}
	record, err := luaService.Lookup(inst.TypeKey)
	if err != nil {
		if service.logger != nil {
			service.logger.Error("Provider resolve failed", "provider_id", inst.ID, "type", inst.TypeKey, "error", err)
		}
		return nil, fmt.Errorf("plugin lookup for type %q failed: %w", inst.TypeKey, err)
	}
	if service.logger != nil {
		service.logger.Debug("Provider resolved through plugin", "provider_id", inst.ID, "type", inst.TypeKey)
	}
	return &Resolved{Instance: inst, Plugin: record}, nil
}
