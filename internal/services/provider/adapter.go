// Package provider wraps the SDK adapter interface for internal use.
package provider

import (
	"fmt"
	"sort"

	sdk "github.com/TheSlopMachine/llm-router-sdk"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// Re-export SDK types for internal use
type Adapter = sdk.Adapter
type ProviderInfo = sdk.ProviderInfo
type AuthStore = sdk.AuthStore
type AuthFlowHandler = sdk.AuthFlowHandler
type AuthFlowContext = sdk.AuthFlowContext
type AuthFlowState = sdk.AuthFlowState
type ProviderError = sdk.ProviderError
type ErrorType = sdk.ErrorType

const (
	ErrorTypeUnknown       = sdk.ErrorTypeUnknown
	ErrorTypeRateLimit     = sdk.ErrorTypeRateLimit
	ErrorTypeQuotaExceeded = sdk.ErrorTypeQuotaExceeded
	ErrorTypeAuth          = sdk.ErrorTypeAuth
	ErrorTypeUpstream      = sdk.ErrorTypeUpstream
	ErrorTypeTimeout       = sdk.ErrorTypeTimeout
)

var ErrNoRefreshNeeded = sdk.ErrNoRefreshNeeded

// Delegate to SDK registry
func Register(a Adapter)                                { sdk.Register(a) }
func Lookup(typeKey string) (Adapter, error)           { return sdk.Lookup(typeKey) }
func Registered() []string                              { return sdk.Registered() }
func GetAllDefaultProviders() map[string][]ProviderInfo { return sdk.GetAllDefaultProviders() }

// Service exposes providers synthesized from the runtime adapter registry.
type Service struct{}

// NewService constructs a new provider Service.
func NewService(_ *db.DB) *Service { return &Service{} }

// Get returns a runtime provider by ID (composite ID: "openai" or "openai:azure").
func (s *Service) Get(id string) (*models.Provider, error) {
	providers, err := s.runtimeProviders()
	if err != nil {
		return nil, err
	}

	for _, p := range providers {
		if p.ID == id {
			return p, nil
		}
	}

	return nil, fmt.Errorf("provider %q not found", id)
}

// List returns all providers exposed by loaded adapters.
func (s *Service) List() ([]*models.Provider, error) {
	return s.runtimeProviders()
}

// GetByType returns all providers for a specific adapter type.
func (s *Service) GetByType(adapterType string) ([]*models.Provider, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}

	var filtered []*models.Provider
	for _, p := range all {
		if p.Type == adapterType {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// GetByTypeAndQualifier returns a specific provider by type and qualifier.
func (s *Service) GetByTypeAndQualifier(adapterType, qualifier string) (*models.Provider, error) {
	return s.Get(buildProviderID(adapterType, qualifier))
}

// SyncDefaultProviders is retained for backward compatibility but no longer
// persists anything. Providers are derived from the runtime adapter registry.
func (s *Service) SyncDefaultProviders() error {
	_, err := s.runtimeProviders()
	return err
}

func (s *Service) runtimeProviders() ([]*models.Provider, error) {
	defaults := sdk.GetAllDefaultProviders()
	adapterTypes := make([]string, 0, len(defaults))
	for adapterType := range defaults {
		adapterTypes = append(adapterTypes, adapterType)
	}
	sort.Strings(adapterTypes)

	providers := make([]*models.Provider, 0)
	for _, adapterType := range adapterTypes {
		adapter, err := sdk.Lookup(adapterType)
		if err != nil {
			return nil, fmt.Errorf("adapter lookup for type %q failed: %w", adapterType, err)
		}

		providerInfos := append([]ProviderInfo(nil), defaults[adapterType]...)
		sort.Slice(providerInfos, func(i, j int) bool {
			if providerInfos[i].Qualifier != providerInfos[j].Qualifier {
				return providerInfos[i].Qualifier < providerInfos[j].Qualifier
			}
			return providerInfos[i].Name < providerInfos[j].Name
		})

		for _, info := range providerInfos {
			providers = append(providers, &models.Provider{
				ID:        buildProviderID(adapterType, info.Qualifier),
				Name:      info.Name,
				Type:      adapterType,
				Qualifier: info.Qualifier,
				BaseURL:   info.BaseURL,
				IconURL:   info.IconURL,
				AuthType:  adapter.AuthType(),
			})
		}
	}

	return providers, nil
}

func buildProviderID(adapterType, qualifier string) string {
	if qualifier == "" {
		return adapterType
	}
	return adapterType + ":" + qualifier
}
