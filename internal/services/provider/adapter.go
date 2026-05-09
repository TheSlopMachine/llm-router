// Package provider wraps the SDK adapter interface for internal use.
package provider

import (
	sdk "github.com/TheSlopMachine/llm-router-sdk"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
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

// Service manages Provider records in the database.
type Service struct {
	db   *db.DB
	repo *repository.Repository[models.Provider]
}

// NewService constructs a new provider Service.
func NewService(database *db.DB) *Service {
	return &Service{
		db:   database,
		repo: repository.New[models.Provider](database, db.BucketProviders, "provider"),
	}
}

// Get returns a Provider by ID (composite ID: "openai" or "openai:azure").
func (s *Service) Get(id string) (*models.Provider, error) {
	return s.repo.Get(id)
}

// List returns all registered providers.
func (s *Service) List() ([]*models.Provider, error) {
	return s.repo.List()
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
	providerID := adapterType
	if qualifier != "" {
		providerID = adapterType + ":" + qualifier
	}
	return s.Get(providerID)
}

// SyncDefaultProviders ensures all default providers from all adapters exist.
// Called during bootstrap to populate the provider list.
func (s *Service) SyncDefaultProviders() error {
	for adapterType, providerInfos := range sdk.GetAllDefaultProviders() {
		adapter, err := sdk.Lookup(adapterType)
		if err != nil {
			continue
		}

		for _, info := range providerInfos {
			providerID := adapterType
			if info.Qualifier != "" {
				providerID = adapterType + ":" + info.Qualifier
			}

			// Check if provider exists
			_, err := s.Get(providerID)
			if err == nil {
				// Provider exists, skip
				continue
			}

			// Create provider
			p := &models.Provider{
				ID:        providerID,
				Name:      info.Name,
				Type:      adapterType,
				Qualifier: info.Qualifier,
				BaseURL:   info.BaseURL,
				IconURL:   info.IconURL,
				AuthType:  adapter.AuthType(),
			}

			if err := s.repo.Put(providerID, p); err != nil {
				return err
			}
		}
	}

	return nil
}
