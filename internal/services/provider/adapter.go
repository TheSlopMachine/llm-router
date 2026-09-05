// Package provider wraps the SDK adapter interface for internal use.
package provider

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	sdk "github.com/TheSlopMachine/llm-router-sdk"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	bolt "go.etcd.io/bbolt"
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
func Lookup(typeKey string) (Adapter, error)            { return sdk.Lookup(typeKey) }
func Registered() []string                              { return sdk.Registered() }
func GetAllDefaultProviders() map[string][]ProviderInfo { return sdk.GetAllDefaultProviders() }

// Service exposes providers synthesized from the runtime adapter registry and custom providers from the database.
type Service struct {
	db              *db.DB
	customProviders *repository.Repository[models.CustomProvider]
	onChanged       func(providerID string)
	logger          *slog.Logger
}

// SetLogger wires structured logging for CRUD.
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

// stripCustomPrefix removes all leading "custom:" prefixes (defensive against double-prefix orphans).
func stripCustomPrefix(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "custom:") {
		s = strings.TrimPrefix(s, "custom:")
	}
	return s
}

// NewService constructs a new provider Service.
func NewService(database *db.DB) *Service {
	return &Service{
		db:              database,
		customProviders: repository.New[models.CustomProvider](database, db.BucketCustomProviders, "custom provider"),
	}
}

// SetOnChanged registers a callback fired after custom provider create/update/delete.
// Wired once in server.New to invalidate model caches — avoids circular import.
func (s *Service) SetOnChanged(fn func(providerID string)) { s.onChanged = fn }

func (s *Service) notifyChanged(providerID string) {
	if s.onChanged != nil {
		s.onChanged(providerID)
	}
}

// Get returns a runtime provider by ID (composite ID: "openai", "openai:azure", or "custom:my-provider").
func (s *Service) Get(id string) (*models.Provider, error) {
	// Check if it's a custom provider (defensive: strip all "custom:" prefixes)
	if strings.HasPrefix(strings.TrimSpace(id), "custom:") {
		nid := stripCustomPrefix(id)
		custom, err := s.customProviders.Get(nid)
		if err != nil {
			return nil, fmt.Errorf("provider %q not found", id)
		}
		return s.customProviderToProvider(custom), nil
	}

	// Check plugin-based providers
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

// List returns all providers (plugin-based + custom).
func (s *Service) List() ([]*models.Provider, error) {
	// Get plugin-based providers
	providers, err := s.runtimeProviders()
	if err != nil {
		return nil, err
	}

	// Get custom providers
	customProviders, err := s.customProviders.List()
	if err != nil {
		return nil, err
	}

	// Convert and append custom providers
	for _, cp := range customProviders {
		providers = append(providers, s.customProviderToProvider(cp))
	}

	return providers, nil
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

// customProviderToProvider converts a CustomProvider to a Provider.
func (s *Service) customProviderToProvider(cp *models.CustomProvider) *models.Provider {
	return &models.Provider{
		ID:        "custom:" + cp.ID,
		Name:      cp.Name,
		Type:      "custom",
		Qualifier: cp.ID,
		BaseURL:   cp.BaseURL,
		IconURL:   cp.IconURL,
		AuthType:  sdk.AuthTypeAPIKey,
	}
}

// CreateCustom creates a new custom provider.
func (s *Service) CreateCustom(name, baseURL, iconURL string) (*models.CustomProvider, error) {
	// Validate inputs (strip accidental "custom:" prefix from name)
	rawName := strings.TrimSpace(name)
	rawName = stripCustomPrefix(rawName)
	name = rawName
	baseURL = strings.TrimSpace(baseURL)
	iconURL = strings.TrimSpace(iconURL)

	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(name) > 100 {
		return nil, fmt.Errorf("name must be 100 characters or less")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("base_url is required")
	}
	if !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "http://") {
		return nil, fmt.Errorf("base_url must be a valid HTTP/HTTPS URL")
	}

	// Generate unique slug ID
	id := slugify(name)
	originalID := id
	counter := 2
	for {
		exists, err := s.customProviders.Exists(id)
		if err != nil {
			return nil, fmt.Errorf("check existence: %w", err)
		}
		if !exists {
			break
		}
		id = fmt.Sprintf("%s-%d", originalID, counter)
		counter++
	}

	// Create custom provider
	now := time.Now()
	cp := &models.CustomProvider{
		ID:        id,
		Name:      name,
		BaseURL:   strings.TrimSuffix(baseURL, "/"),
		IconURL:   iconURL,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.customProviders.Put(id, cp); err != nil {
		return nil, fmt.Errorf("save custom provider: %w", err)
	}

	if s.logger != nil {
		s.logger.Info("custom provider created", "provider_id", "custom:"+id, "base_url", cp.BaseURL)
	}
	s.notifyChanged("custom:" + id)
	return cp, nil
}

// UpdateCustom updates an existing custom provider.
func (s *Service) UpdateCustom(id, name, baseURL, iconURL string) (*models.CustomProvider, error) {
	id = stripCustomPrefix(id)
	// Validate inputs
	name = strings.TrimSpace(name)
	baseURL = strings.TrimSpace(baseURL)
	iconURL = strings.TrimSpace(iconURL)

	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(name) > 100 {
		return nil, fmt.Errorf("name must be 100 characters or less")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("base_url is required")
	}
	if !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "http://") {
		return nil, fmt.Errorf("base_url must be a valid HTTP/HTTPS URL")
	}

	// Update the custom provider
	var updated *models.CustomProvider
	err := s.customProviders.Update(id, func(cp *models.CustomProvider) error {
		cp.Name = name
		cp.BaseURL = strings.TrimSuffix(baseURL, "/")
		cp.IconURL = iconURL
		cp.UpdatedAt = time.Now()
		updated = cp
		return nil
	})

	if err != nil {
		return nil, err
	}

	if s.logger != nil {
		s.logger.Info("custom provider updated", "provider_id", "custom:"+id, "base_url", updated.BaseURL)
	}
	s.notifyChanged("custom:" + id)
	return updated, nil
}

// DeleteCustom deletes a custom provider and cascades its credentials (no orphans).
func (s *Service) DeleteCustom(id string) error {
	nid := stripCustomPrefix(id)
	if err := s.customProviders.Delete(nid); err != nil {
		return err
	}
	// Cascade: delete credentials whose provider slug matches nid (handles "custom:slug", "slug", "custom:custom:slug").
	n := 0
	_ = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketCredentials)
		if b == nil {
			return nil
		}
		var toDelete [][]byte
		_ = b.ForEach(func(k, v []byte) error {
			var c models.Credential
			if err := json.Unmarshal(v, &c); err != nil {
				return nil
			}
			if stripCustomPrefix(c.ProviderID) == nid {
				toDelete = append(toDelete, append([]byte(nil), k...))
			}
			return nil
		})
		for _, k := range toDelete {
			_ = b.Delete(k)
			n++
		}
		return nil
	})
	if s.logger != nil {
		s.logger.Info("custom provider deleted", "provider_id", "custom:"+nid, "cascaded_credentials", n)
	}
	s.notifyChanged("custom:" + nid)
	return nil
}

// GetCustom retrieves a custom provider by ID (accepts "slug" or "custom:slug").
func (s *Service) GetCustom(id string) (*models.CustomProvider, error) {
	return s.customProviders.Get(stripCustomPrefix(id))
}

// CleanupOrphanedCredentials deletes credentials whose provider no longer exists.
// Scans only "custom:*" credentials to avoid touching plugin providers.
func (s *Service) CleanupOrphanedCredentials() (int, error) {
	n := 0
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketCredentials)
		if b == nil {
			return nil
		}
		var toDelete [][]byte
		_ = b.ForEach(func(k, v []byte) error {
			var c models.Credential
			if err := json.Unmarshal(v, &c); err != nil {
				return nil
			}
			pid := strings.TrimSpace(c.ProviderID)
			if pid == "" {
				return nil
			}
			// Only consider custom-provider credentials
			if !strings.HasPrefix(pid, "custom:") {
				return nil
			}
			nid := stripCustomPrefix(pid)
			if _, err := s.customProviders.Get(nid); err != nil {
				toDelete = append(toDelete, append([]byte(nil), k...))
			}
			return nil
		})
		for _, k := range toDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
			n = len(toDelete)
		}
		return nil
	})
	if err == nil && n > 0 && s.logger != nil {
		s.logger.Info("cleaned orphaned credentials", "count", n)
	}
	return n, err
}

// slugify converts a name to a URL-safe slug.
func slugify(name string) string {
	// Convert to lowercase
	slug := strings.ToLower(name)

	// Replace spaces and underscores with hyphens
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")

	// Remove non-alphanumeric characters except hyphens
	reg := regexp.MustCompile(`[^a-z0-9-]+`)
	slug = reg.ReplaceAllString(slug, "")

	// Remove leading/trailing hyphens
	slug = strings.Trim(slug, "-")

	// Collapse multiple consecutive hyphens
	reg = regexp.MustCompile(`-+`)
	slug = reg.ReplaceAllString(slug, "-")

	if slug == "" {
		slug = "provider"
	}

	return slug
}
