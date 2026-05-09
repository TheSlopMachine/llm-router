package provider

import (
	"fmt"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// ResolveAdapter looks up a provider by ID and returns both the provider
// record and its registered adapter in a single operation.
//
// This is a common pattern used across multiple services (credential, maintenance,
// modelcache, router) and is extracted here to eliminate duplication.
//
// Returns:
//   - adapter: The registered adapter for the provider's type
//   - provider: The provider record from the database
//   - error: Any error during lookup
func ResolveAdapter(providerSvc *Service, providerID string) (Adapter, *models.Provider, error) {
	p, err := providerSvc.Get(providerID)
	if err != nil {
		return nil, nil, fmt.Errorf("provider lookup failed: %w", err)
	}

	adapter, err := Lookup(p.Type)
	if err != nil {
		return nil, nil, fmt.Errorf("adapter lookup for type %q failed: %w", p.Type, err)
	}

	return adapter, p, nil
}

