// Package provider owns the unified provider registry.
//
// Every provider is an explicit ProviderInstance database row, regardless of
// whether its TypeKey is served by a Lua plugin or by a built-in Go adapter
// ("custom", "agents").
package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	bolt "go.etcd.io/bbolt"
)

// GoAdapter is implemented by built-in Go backends ("custom", "agents").
// Lua plugin types are served by luaplugin.Service instead.
type GoAdapter interface {
	TypeKey() string
	ValidateCredentials(data map[string]any) error
	Complete(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest, providerConfig map[string]any) (*models.ChatCompletionResponse, error)
	CompleteStream(ctx context.Context, cred *models.Credential, req *models.ChatCompletionRequest, w io.Writer, providerConfig map[string]any) error
	NeedsRefresh(cred *models.Credential) bool
	RefreshCredential(ctx context.Context, cred *models.Credential) (map[string]any, error)
	GetModelInfos(ctx context.Context, cred *models.Credential, providerConfig map[string]any) ([]models.ModelInfo, error)
}

// Built-in type keys served by Go code.
const (
	TypeCustom = "custom"
	TypeAgents = "agents"
)

// Service exposes unified ProviderInstance CRUD.
type Service struct {
	db        *db.DB
	providers *repository.Repository[models.ProviderInstance]
	luaSvc    *luaplugin.Service

	mu         sync.RWMutex
	goAdapters map[string]GoAdapter

	onChanged func(providerID string)
	logger    *slog.Logger
}

// NewService constructs a provider Service and migrates legacy custom
// provider rows into provider_instances.
func NewService(database *db.DB) *Service {
	s := &Service{
		db:         database,
		providers:  repository.New[models.ProviderInstance](database, db.BucketProviderInstances, "provider"),
		goAdapters: map[string]GoAdapter{},
	}
	_ = s.migrateLegacyCustom()
	return s
}

// SetLogger wires structured logging for CRUD.
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

// SetLuaService wires the Lua plugin service for plugin-backed types.
func (s *Service) SetLuaService(svc *luaplugin.Service) { s.luaSvc = svc }

// LuaService returns the wired Lua plugin service, if any.
func (s *Service) LuaService() *luaplugin.Service { return s.luaSvc }

// RegisterGoAdapter registers a built-in Go backend.
func (s *Service) RegisterGoAdapter(a GoAdapter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.goAdapters[a.TypeKey()] = a
}

// GoAdapterFor returns the Go backend for a type key, if one is registered.
func (s *Service) GoAdapterFor(typeKey string) (GoAdapter, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.goAdapters[typeKey]
	return a, ok
}

// GoAdapterTypes returns sorted registered Go type keys.
func (s *Service) GoAdapterTypes() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.goAdapters))
	for k := range s.goAdapters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SetOnChanged registers a callback fired after provider create/update/delete.
func (s *Service) SetOnChanged(fn func(providerID string)) { s.onChanged = fn }

func (s *Service) notifyChanged(providerID string) {
	if s.onChanged != nil {
		s.onChanged(providerID)
	}
}

// ─────────────────────────────────────────────
// CRUD
// ─────────────────────────────────────────────

// CreateOptions holds parameters for creating a provider instance.
type CreateOptions struct {
	Name      string
	TypeKey   string
	Qualifier string
	Config    map[string]any
	IconURL   string
}

// Create persists a new provider instance with a generated unique ID.
func (s *Service) Create(opts CreateOptions) (*models.ProviderInstance, error) {
	name := strings.TrimSpace(opts.Name)
	typeKey := strings.TrimSpace(opts.TypeKey)
	qualifier := strings.TrimSpace(opts.Qualifier)
	iconURL := strings.TrimSpace(opts.IconURL)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(name) > 100 {
		return nil, fmt.Errorf("name must be 100 characters or less")
	}
	if typeKey == "" {
		return nil, fmt.Errorf("type_key is required")
	}
	if strings.Contains(typeKey, "/") || strings.Contains(typeKey, ":") {
		return nil, fmt.Errorf("type_key must not contain '/' or ':'")
	}
	config := opts.Config
	if config == nil {
		config = map[string]any{}
	}
	if typeKey == TypeCustom {
		baseURL, _ := config["base_url"].(string)
		baseURL = strings.TrimSpace(baseURL)
		if baseURL == "" {
			return nil, fmt.Errorf("base_url is required for custom providers")
		}
		if !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "http://") {
			return nil, fmt.Errorf("base_url must be a valid HTTP/HTTPS URL")
		}
		config["base_url"] = strings.TrimSuffix(baseURL, "/")
	}

	id := s.uniqueID(typeKey, qualifier, name)
	now := time.Now()
	inst := &models.ProviderInstance{
		ID: id, Name: name, TypeKey: typeKey, Qualifier: qualifier,
		Config: config, IconURL: iconURL, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.providers.Put(id, inst); err != nil {
		return nil, fmt.Errorf("save provider: %w", err)
	}
	if s.logger != nil {
		s.logger.Info("provider created", "provider_id", id, "type", typeKey)
	}
	s.notifyChanged(id)
	return inst, nil
}

func (s *Service) uniqueID(typeKey, qualifier, name string) string {
	var base string
	switch {
	case qualifier != "":
		base = typeKey + ":" + slugify(qualifier)
	case typeKey == TypeCustom:
		base = "custom:" + slugify(name)
	default:
		base = typeKey
	}
	id := base
	for counter := 2; ; counter++ {
		exists, err := s.providers.Exists(id)
		if err != nil || !exists {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, counter)
	}
}

// Get returns a provider instance by ID.
func (s *Service) Get(id string) (*models.ProviderInstance, error) {
	inst, err := s.providers.Get(strings.TrimSpace(id))
	if err != nil {
		return nil, fmt.Errorf("provider %q not found", id)
	}
	return inst, nil
}

// List returns all provider instances sorted by ID.
func (s *Service) List() ([]*models.ProviderInstance, error) {
	items, err := s.providers.List()
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// GetByType returns all providers served by a type key.
func (s *Service) GetByType(typeKey string) ([]*models.ProviderInstance, error) {
	return s.providers.ListFiltered(func(p *models.ProviderInstance) bool {
		return p.TypeKey == typeKey
	})
}

// GetByTypeAndQualifier returns a specific provider by type and qualifier.
func (s *Service) GetByTypeAndQualifier(typeKey, qualifier string) (*models.ProviderInstance, error) {
	id := typeKey
	if qualifier != "" {
		id = typeKey + ":" + qualifier
	}
	return s.Get(id)
}

// UpdateOptions holds mutable provider fields.
type UpdateOptions struct {
	Name    string
	Config  map[string]any
	IconURL string
}

// Update replaces a provider's mutable fields.
func (s *Service) Update(id string, opts UpdateOptions) (*models.ProviderInstance, error) {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(name) > 100 {
		return nil, fmt.Errorf("name must be 100 characters or less")
	}
	var updated *models.ProviderInstance
	err := s.providers.Update(strings.TrimSpace(id), func(p *models.ProviderInstance) error {
		if p.TypeKey == TypeCustom && opts.Config != nil {
			baseURL, _ := opts.Config["base_url"].(string)
			baseURL = strings.TrimSpace(baseURL)
			if baseURL == "" {
				return fmt.Errorf("base_url is required for custom providers")
			}
			if !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "http://") {
				return fmt.Errorf("base_url must be a valid HTTP/HTTPS URL")
			}
			opts.Config["base_url"] = strings.TrimSuffix(baseURL, "/")
		}
		p.Name = name
		if opts.Config != nil {
			p.Config = opts.Config
		}
		p.IconURL = strings.TrimSpace(opts.IconURL)
		p.UpdatedAt = time.Now()
		updated = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.logger != nil {
		s.logger.Info("provider updated", "provider_id", id)
	}
	s.notifyChanged(strings.TrimSpace(id))
	return updated, nil
}

// Delete removes a provider and cascades its credentials.
func (s *Service) Delete(id string) error {
	id = strings.TrimSpace(id)
	if _, err := s.providers.Get(id); err != nil {
		return fmt.Errorf("provider %q not found", id)
	}
	if err := s.providers.Delete(id); err != nil {
		return err
	}
	n := 0
	_ = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketCredentials)
		if b == nil {
			return nil
		}
		var toDelete [][]byte
		_ = b.ForEach(func(k, v []byte) error {
			var c models.Credential
			if err := jsonUnmarshal(v, &c); err != nil {
				return nil
			}
			if strings.TrimSpace(c.ProviderID) == id {
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
		s.logger.Info("provider deleted", "provider_id", id, "cascaded_credentials", n)
	}
	s.notifyChanged(id)
	return nil
}

// EnsureSeeded creates the built-in agents provider row and one provider
// row per enabled Lua plugin type key when none exists yet, keeping fresh
// installs routable without manual provider setup. Missing icons on existing
// rows are backfilled from the plugin; user-set icons are never overwritten.
// Seeded rows are marked UI-readonly (set automatically by the core, never
// from Lua or the dashboard); the agents row is additionally UI-hidden.
func (s *Service) EnsureSeeded() error {
	now := time.Now()
	ensure := func(id, name, typeKey, icon string) error {
		if _, err := s.providers.Get(id); err == nil {
			return nil
		}
		hidden := typeKey == TypeAgents
		inst := &models.ProviderInstance{
			ID: id, Name: name, TypeKey: typeKey,
			Config: map[string]any{}, IconURL: icon,
			IsUIReadonly: true, IsUIHidden: hidden,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := s.providers.Put(id, inst); err != nil {
			return err
		}
		s.notifyChanged(id)
		return nil
	}
	// Backfill flags on rows that match the seed pattern (pre-flag installs).
	if err := s.backfillSeedFlags(); err != nil {
		return err
	}
	if err := ensure("agents", "Agents", TypeAgents, ""); err != nil {
		return err
	}
	if s.luaSvc == nil {
		return nil
	}
	records, err := s.luaSvc.List()
	if err != nil {
		return err
	}
	seen := map[string]string{}
	for _, rec := range records {
		if !rec.Enabled {
			continue
		}
		for _, key := range rec.TypeKeys {
			if _, ok := seen[key]; !ok {
				seen[key] = rec.DisplayName
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var icon string
		if s.luaSvc != nil {
			icon = s.luaSvc.Icon(key)
		}
		existing, err := s.GetByType(key)
		if err != nil {
			return err
		}
		if len(existing) == 0 {
			name := seen[key]
			if strings.TrimSpace(name) == "" {
				name = key
			}
			if err := ensure(key, name, key, icon); err != nil {
				return err
			}
			continue
		}
		if icon == "" {
			continue
		}
		for _, inst := range existing {
			if inst.IconURL != "" {
				continue
			}
			if err := s.providers.Update(inst.ID, func(p *models.ProviderInstance) error {
				p.IconURL = icon
				p.UpdatedAt = time.Now()
				return nil
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// backfillSeedFlags marks pre-flag rows matching the seed pattern as
// UI-readonly (the bare `agents` row additionally UI-hidden). Rows with
// qualifiers stay editable: only the singleton slot is core-managed.
func (s *Service) backfillSeedFlags() error {
	rows, err := s.providers.List()
	if err != nil {
		return err
	}
	for _, inst := range rows {
		wantReadonly := inst.ID == inst.TypeKey && inst.TypeKey != TypeCustom
		wantHidden := inst.TypeKey == TypeAgents && inst.ID == TypeAgents
		if !wantReadonly && !wantHidden {
			continue
		}
		if inst.IsUIReadonly == wantReadonly && inst.IsUIHidden == wantHidden {
			continue
		}
		if err := s.providers.Update(inst.ID, func(p *models.ProviderInstance) error {
			p.IsUIReadonly = wantReadonly
			p.IsUIHidden = wantHidden
			p.UpdatedAt = time.Now()
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// CleanupOrphanedCredentials deletes credentials whose provider no longer exists.
func (s *Service) CleanupOrphanedCredentials() (int, error) {
	providers, err := s.providers.List()
	if err != nil {
		return 0, err
	}
	known := map[string]bool{}
	for _, p := range providers {
		known[p.ID] = true
	}
	n := 0
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketCredentials)
		if b == nil {
			return nil
		}
		var toDelete [][]byte
		_ = b.ForEach(func(k, v []byte) error {
			var c models.Credential
			if err := jsonUnmarshal(v, &c); err != nil {
				return nil
			}
			if pid := strings.TrimSpace(c.ProviderID); pid != "" && !known[pid] {
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

// ─────────────────────────────────────────────
// Type-level helpers
// ─────────────────────────────────────────────

// TypeKeys returns every known provider type: Go backends plus enabled
// Lua plugin type keys.
func (s *Service) TypeKeys() []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range s.GoAdapterTypes() {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	if s.luaSvc != nil {
		for _, k := range s.luaSvc.Registered() {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

// SupportsAuthFlow reports whether a type offers a multi-step auth wizard.
func (s *Service) SupportsAuthFlow(typeKey string) bool {
	if _, ok := s.GoAdapterFor(typeKey); ok {
		return false
	}
	if s.luaSvc == nil {
		return false
	}
	return s.luaSvc.HasHandler(typeKey, "auth_initiate")
}

// IsCreatableTypeKey reports whether users may create provider rows of a
// type through the UI. The agents singleton is core-managed and excluded;
// every other known type stays creatable (qualifier rows included).
func IsCreatableTypeKey(typeKey string) bool {
	return typeKey != TypeAgents
}

// ConfigSchema returns the config UI tree for a type key.
// Go types serve static trees; Lua types delegate to config_schema.
// ErrHandlerNotFound-equivalent (nil, nil) means "raw JSON fallback".
func (s *Service) ConfigSchema(typeKey string) ([]*models.UINode, error) {
	if typeKey == TypeCustom {
		return []*models.UINode{
			{Type: "text", Text: "OpenAI-compatible endpoint details."},
			{Type: "input", Name: "base_url", Label: "Base URL", Required: true, Placeholder: "https://api.example.com/v1"},
		}, nil
	}
	if typeKey == TypeAgents {
		return nil, nil
	}
	if s.luaSvc == nil {
		return nil, fmt.Errorf("no plugin service wired")
	}
	nodes, err := s.luaSvc.Schema(typeKey, "config_schema")
	if err != nil {
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return nodes, nil
}

// CredentialSchema returns the credential UI tree for a type key.
func (s *Service) CredentialSchema(typeKey string) ([]*models.UINode, error) {
	if typeKey == TypeCustom {
		return []*models.UINode{
			{Type: "text", Text: "Enter the API key for this provider."},
			{Type: "input", Name: "api_key", InputType: "password", Label: "API Key", Required: true},
			{Type: "button", Text: "Save", FormAction: "submit"},
		}, nil
	}
	if typeKey == TypeAgents {
		return []*models.UINode{
			{Type: "text", Text: "Bind this credential to an agent."},
			{Type: "input", Name: "agent_id", Label: "Agent ID", Required: true},
			{Type: "button", Text: "Save", FormAction: "submit"},
		}, nil
	}
	if s.luaSvc == nil {
		return nil, fmt.Errorf("no plugin service wired")
	}
	nodes, err := s.luaSvc.Schema(typeKey, "credential_schema")
	if err != nil {
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return nodes, nil
}

// ─────────────────────────────────────────────
// Legacy compatibility
// ─────────────────────────────────────────────

// CreateCustom creates a custom provider. Kept for existing callers.
func (s *Service) CreateCustom(name, baseURL, iconURL string) (*models.ProviderInstance, error) {
	return s.Create(CreateOptions{
		Name: name, TypeKey: TypeCustom,
		Config: map[string]any{"base_url": baseURL}, IconURL: iconURL,
	})
}

// UpdateCustom updates a custom provider by slug or full ID.
func (s *Service) UpdateCustom(id, name, baseURL, iconURL string) (*models.ProviderInstance, error) {
	return s.Update(normalizeCustomID(id), UpdateOptions{
		Name: name, Config: map[string]any{"base_url": baseURL}, IconURL: iconURL,
	})
}

// DeleteCustom deletes a custom provider by slug or full ID.
func (s *Service) DeleteCustom(id string) error {
	return s.Delete(normalizeCustomID(id))
}

// GetCustom retrieves a custom provider by slug or full ID.
func (s *Service) GetCustom(id string) (*models.ProviderInstance, error) {
	inst, err := s.Get(normalizeCustomID(id))
	if err != nil {
		return nil, err
	}
	if inst.TypeKey != TypeCustom {
		return nil, fmt.Errorf("provider %q is not a custom provider", id)
	}
	return inst, nil
}

// SyncDefaultProviders is a no-op kept for backward compatibility.
// Providers are explicit database rows; nothing is synthesized.
func (s *Service) SyncDefaultProviders() error { return s.EnsureSeeded() }

func normalizeCustomID(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "custom:") {
		return id
	}
	return "custom:" + slugify(id)
}

func stripCustomPrefix(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "custom:") {
		s = strings.TrimPrefix(s, "custom:")
	}
	return s
}

// migrateLegacyCustom moves BucketCustomProviders rows into provider_instances.
func (s *Service) migrateLegacyCustom() error {
	type legacyCustomProvider struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		BaseURL   string    `json:"base_url"`
		IconURL   string    `json:"icon_url"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	var legacy []legacyCustomProvider
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketCustomProviders)
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			var cp legacyCustomProvider
			if err := jsonUnmarshal(v, &cp); err != nil {
				return nil
			}
			legacy = append(legacy, cp)
			return nil
		})
	})
	if err != nil {
		return err
	}
	for _, cp := range legacy {
		id := "custom:" + slugify(cp.ID)
		if exists, _ := s.providers.Exists(id); exists {
			continue
		}
		now := time.Now()
		created := cp.CreatedAt
		if created.IsZero() {
			created = now
		}
		updated := cp.UpdatedAt
		if updated.IsZero() {
			updated = now
		}
		inst := &models.ProviderInstance{
			ID: id, Name: cp.Name, TypeKey: TypeCustom, Qualifier: slugify(cp.ID),
			Config:  map[string]any{"base_url": strings.TrimSuffix(strings.TrimSpace(cp.BaseURL), "/")},
			IconURL: cp.IconURL, CreatedAt: created, UpdatedAt: updated,
		}
		if strings.TrimSpace(inst.Name) == "" {
			inst.Name = cp.ID
		}
		_ = s.providers.Put(id, inst)
		// Migrate credentials pointing at legacy slug forms to the full ID.
		_ = s.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(db.BucketCredentials)
			if b == nil {
				return nil
			}
			var keys [][]byte
			var vals [][]byte
			_ = b.ForEach(func(k, v []byte) error {
				var c models.Credential
				if err := jsonUnmarshal(v, &c); err != nil {
					return nil
				}
				if stripCustomPrefix(c.ProviderID) == stripCustomPrefix(cp.ID) && strings.TrimSpace(c.ProviderID) != id {
					c.ProviderID = id
					enc, err := jsonMarshal(&c)
					if err != nil {
						return nil
					}
					keys = append(keys, append([]byte(nil), k...))
					vals = append(vals, enc)
				}
				return nil
			})
			for i, k := range keys {
				_ = b.Put(k, vals[i])
			}
			return nil
		})
	}
	return nil
}

// slugify converts a name to a URL-safe slug.
func slugify(name string) string {
	slug := strings.ToLower(name)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	reg := regexp.MustCompile(`[^a-z0-9-]+`)
	slug = reg.ReplaceAllString(slug, "")
	slug = strings.Trim(slug, "-")
	reg = regexp.MustCompile(`-+`)
	slug = reg.ReplaceAllString(slug, "-")
	if slug == "" {
		slug = "provider"
	}
	return slug
}
