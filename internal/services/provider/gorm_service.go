package provider

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	stdErrors "errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// GormService manages provider instances on the SQLite handle. It owns the
// provider registry: built-in Go adapters, the Lua plugin service for
// plugin-backed types, and the change callback. Traditional GORM use: none;
// every statement runs through gorm.G with typed clauses.
type GormService struct {
	database   *db.Database
	luaService *luaplugin.Service

	mutex      sync.RWMutex
	goAdapters map[string]GoAdapter

	onChanged func(providerID string)
	logger    *slog.Logger
}

// NewGorm constructs a GORM-backed provider service on the SQLite handle.
// Construction performs no I/O.
func NewGorm(database *db.Database) *GormService {
	return &GormService{
		database:   database,
		goAdapters: map[string]GoAdapter{},
	}
}

// SetLogger wires structured logging for CRUD.
func (s *GormService) SetLogger(l *slog.Logger) { s.logger = l }

// SetLuaService wires the Lua plugin service for plugin-backed types.
func (s *GormService) SetLuaService(service *luaplugin.Service) { s.luaService = service }

// LuaService returns the wired Lua plugin service, if any.
func (s *GormService) LuaService() *luaplugin.Service { return s.luaService }

// SetOnChanged registers a callback fired after provider create/update/delete.
func (s *GormService) SetOnChanged(function func(providerID string)) { s.onChanged = function }

func (s *GormService) notifyChanged(providerID string) {
	if s.onChanged != nil {
		s.onChanged(providerID)
	}
}

// notifyProviderDisabled fires the plugin deinitialize hook for a provider
// that left the enabled state. It is a no-op without a wired Lua service.
// Update, SystemDisable and Delete are the only callers: every disable
// transition funnels through one of them, so the hook fires exactly once
// per transition and never from a second source.
func (s *GormService) notifyProviderDisabled(snapshot *models.ProviderInstance) {
	if s.luaService == nil {
		return
	}
	s.luaService.NotifyProviderDisabled(snapshot)
}

// CreateOptions holds parameters for creating a provider instance.
type CreateOptions struct {
	Name      string
	TypeKey   string
	Qualifier string
	Config    map[string]any
	IconURL   string
}

// UpdateOptions holds mutable provider fields.
type UpdateOptions struct {
	Name     string
	Config   map[string]any
	IconURL  string
	Disabled *bool
}

// configBaseURL reads the raw base_url from a provider config map.
func configBaseURL(config map[string]any) string {
	if config == nil {
		return ""
	}
	value, _ := config["base_url"].(string)
	return value
}

// orderProvidersByIdentifier sorts provider rows by primary key.
func orderProvidersByIdentifier() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}}}
}

// requireHandle fails closed when either SQLite handle is missing.
func (s *GormService) requireHandle() (*db.Database, error) {
	if s == nil || s.database == nil || s.database.Handle == nil || s.database.Cache == nil {
		return nil, fmt.Errorf("provider sqlite handle is not configured: %w", apierrors.ErrInvalidInput)
	}
	return s.database, nil
}

// Create persists a new provider instance with a generated unique
// identifier. Validation mirrors the previous path: name and type key are
// required, the type key carries no slash or colon, custom rows normalize
// base_url. Singleton plugin types reuse the existing bare row instead of
// creating a duplicate. One transaction holds the reuse check, the
// uniqueness loop, and the insert on the transaction handle only.
func (s *GormService) Create(ctx context.Context, options CreateOptions) (*models.ProviderInstance, error) {
	name := strings.TrimSpace(options.Name)
	typeKey := strings.TrimSpace(options.TypeKey)
	qualifier := strings.TrimSpace(options.Qualifier)
	iconURL := strings.TrimSpace(options.IconURL)
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
	configuration := cloneProviderConfig(options.Config)
	if typeKey == TypeCustom {
		normalized, err := models.NormalizeBaseURL(configBaseURL(configuration))
		if err != nil {
			return nil, err
		}
		configuration[providerConfigBaseURLKey] = normalized
	}
	if _, err := s.requireHandle(); err != nil {
		return nil, err
	}
	var result *models.ProviderInstance
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if typeKey != TypeCustom && qualifier == "" {
			existing, err := gorm.G[record.ProviderInstance](transaction, clause.Eq{Column: "id", Value: typeKey}).First(ctx)
			if err == nil {
				result = providerModelFromRecord(existing)
				return nil
			}
			if !stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
				return db.Translate(err)
			}
		}
		identifier, err := uniqueProviderIDInTransaction(ctx, transaction, typeKey, qualifier, name)
		if err != nil {
			return err
		}
		now := util.Now()
		candidate := &models.ProviderInstance{
			ID:        identifier,
			Name:      name,
			TypeKey:   typeKey,
			Qualifier: qualifier,
			Config:    configuration,
			IconURL:   iconURL,
			CreatedAt: now,
			UpdatedAt: now,
		}
		row := providerRecordFromModel(candidate)
		if err := gorm.G[record.ProviderInstance](transaction).Create(ctx, &row); err != nil {
			return db.Translate(err)
		}
		result = providerModelFromRecord(row)
		return nil
	}); err != nil {
		return nil, err
	}
	if s.logger != nil {
		s.logger.Info("provider created", "provider_id", result.ID, "type", typeKey)
	}
	s.notifyChanged(result.ID)
	return result, nil
}

// Get returns a provider instance by identifier. A miss reports the
// not-found sentinel with provider context.
func (s *GormService) Get(ctx context.Context, identifier string) (*models.ProviderInstance, error) {
	if _, err := s.requireHandle(); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(identifier)
	row, err := gorm.G[record.ProviderInstance](s.database.Handle, clause.Eq{Column: "id", Value: trimmed}).First(ctx)
	if err != nil {
		translated := db.Translate(err)
		if stdErrors.Is(translated, apierrors.ErrNotFound) {
			return nil, apierrors.NewNotFoundError("provider", identifier)
		}
		return nil, translated
	}
	return providerModelFromRecord(row), nil
}

// List returns all provider instances sorted by identifier.
func (s *GormService) List(ctx context.Context) ([]*models.ProviderInstance, error) {
	if _, err := s.requireHandle(); err != nil {
		return nil, err
	}
	rows, err := gorm.G[record.ProviderInstance](s.database.Handle, orderProvidersByIdentifier()).Find(ctx)
	if err != nil {
		return nil, db.Translate(err)
	}
	items := make([]*models.ProviderInstance, 0, len(rows))
	for _, row := range rows {
		items = append(items, providerModelFromRecord(row))
	}
	return items, nil
}

// GetByType returns all providers served by a type key, sorted by identifier.
func (s *GormService) GetByType(ctx context.Context, typeKey string) ([]*models.ProviderInstance, error) {
	if _, err := s.requireHandle(); err != nil {
		return nil, err
	}
	rows, err := gorm.G[record.ProviderInstance](s.database.Handle, clause.Eq{Column: "type_key", Value: typeKey}, orderProvidersByIdentifier()).Find(ctx)
	if err != nil {
		return nil, db.Translate(err)
	}
	items := make([]*models.ProviderInstance, 0, len(rows))
	for _, row := range rows {
		items = append(items, providerModelFromRecord(row))
	}
	return items, nil
}

// GetByTypeAndQualifier returns a specific provider by type and qualifier.
// The qualifier runs through the same slugify as identifier generation, so
// lookups use the stored form. Dedup-suffixed duplicates resolve only by
// exact identifier via Get.
func (s *GormService) GetByTypeAndQualifier(ctx context.Context, typeKey, qualifier string) (*models.ProviderInstance, error) {
	identifier := typeKey
	if qualifier != "" {
		identifier = typeKey + ":" + util.SlugifyOrDefault(qualifier, "provider")
	}
	return s.Get(ctx, identifier)
}

// Update replaces a provider mutable fields. Custom rows normalize base_url
// when a configuration reads present. The disabled toggle sets the admin
// cause on disable and clears it on re-enable. One transaction holds the
// read-modify-write on the transaction handle only.
func (s *GormService) Update(ctx context.Context, identifier string, options UpdateOptions) (*models.ProviderInstance, error) {
	name := strings.TrimSpace(options.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(name) > 100 {
		return nil, fmt.Errorf("name must be 100 characters or less")
	}
	if _, err := s.requireHandle(); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(identifier)
	var result *models.ProviderInstance
	wasEnabled := false
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		row, err := gorm.G[record.ProviderInstance](transaction, clause.Eq{Column: "id", Value: trimmed}).First(ctx)
		if err != nil {
			return db.Translate(err)
		}
		wasEnabled = !row.Disabled
		configuration := options.Config
		if row.TypeKey == TypeCustom && configuration != nil {
			normalized, err := models.NormalizeBaseURL(configBaseURL(configuration))
			if err != nil {
				return err
			}
			configuration = cloneProviderConfig(configuration)
			configuration[providerConfigBaseURLKey] = normalized
		}
		row.Name = name
		if configuration != nil {
			baseURL, proxyPoolID, disableFailed, settings := splitProviderConfig(configuration)
			row.BaseURL = baseURL
			row.ProxyPoolID = proxyPoolID
			row.DisableFailedCredentials = disableFailed
			row.Settings = settings
		}
		if options.Disabled != nil {
			if *options.Disabled {
				now := util.Now()
				row.Disabled = true
				row.DisabledBy = models.DisabledByAdmin
				row.DisabledReason = "disabled by admin"
				row.DisabledAt = &now
			} else {
				row.Disabled = false
				row.DisabledBy = ""
				row.DisabledReason = ""
				row.DisabledAt = nil
			}
		}
		row.IconURL = strings.TrimSpace(options.IconURL)
		row.UpdatedAt = util.Now()
		rows := []record.ProviderInstance{row}
		if err := gorm.G[record.ProviderInstance](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return db.Translate(err)
		}
		result = providerModelFromRecord(row)
		return nil
	}); err != nil {
		return nil, err
	}
	if s.logger != nil {
		s.logger.Info("provider updated", "provider_id", trimmed, "type", result.TypeKey, "disabled", result.Disabled, "disabled_by", result.DisabledBy)
	}
	s.notifyChanged(trimmed)
	if options.Disabled != nil && *options.Disabled && wasEnabled {
		s.notifyProviderDisabled(result)
	}
	return result, nil
}

// SystemDisable disables a provider on explicit system request. First wins:
// an already-disabled provider keeps its original cause. One transaction
// holds the read-modify-write on the transaction handle only.
func (s *GormService) SystemDisable(ctx context.Context, identifier, reason string) error {
	if reason == "" {
		reason = "disabled by system"
	}
	if len(reason) > 1024 {
		reason = reason[:1024] + "…[truncated]"
	}
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(identifier)
	var disabledSnapshot *models.ProviderInstance
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		row, err := gorm.G[record.ProviderInstance](transaction, clause.Eq{Column: "id", Value: trimmed}).First(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if row.Disabled {
			return nil
		}
		now := util.Now()
		row.Disabled = true
		row.DisabledBy = models.DisabledBySystem
		row.DisabledReason = reason
		row.DisabledAt = &now
		row.UpdatedAt = now
		rows := []record.ProviderInstance{row}
		if err := gorm.G[record.ProviderInstance](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return db.Translate(err)
		}
		disabledSnapshot = providerModelFromRecord(row)
		return nil
	}); err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Info("provider disabled by system", "provider_id", trimmed, "reason", reason)
	}
	s.notifyChanged(trimmed)
	if disabledSnapshot != nil {
		s.notifyProviderDisabled(disabledSnapshot)
	}
	return nil
}

// Delete removes a provider in one delete. Credential, model override,
// managed virtual model, and virtual member rows cascade from the foreign
// keys; cached park, health, and model info rows reference the provider
// softly across files and clear explicitly after the durable delete
// succeeds. A zero-row delete reports the not-found sentinel through the
// WithResult tracker.
func (s *GormService) Delete(ctx context.Context, identifier string) error {
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(identifier)
	var deletedSnapshot *models.ProviderInstance
	var credentialIDs []string
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		rows, err := gorm.G[record.Credential](transaction, clause.Eq{Column: "provider_id", Value: trimmed}).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		for _, row := range rows {
			credentialIDs = append(credentialIDs, row.ID)
		}
		row, err := gorm.G[record.ProviderInstance](transaction, clause.Eq{Column: "id", Value: trimmed}).First(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if !row.Disabled {
			deletedSnapshot = providerModelFromRecord(row)
		}
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.ProviderInstance](transaction, clause.Eq{Column: "id", Value: trimmed}, tracker).Delete(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if affected == 0 || tracker.RowsAffected == 0 {
			return fmt.Errorf("provider %q: %w", trimmed, apierrors.ErrNotFound)
		}
		return nil
	}); err != nil {
		return err
	}
	s.clearProviderCache(ctx, trimmed, credentialIDs)
	if s.logger != nil {
		s.logger.Info("provider deleted", "provider_id", trimmed)
	}
	s.notifyChanged(trimmed)
	if deletedSnapshot != nil {
		s.notifyProviderDisabled(deletedSnapshot)
	}
	return nil
}

// clearProviderCache removes cached park, health, and model info rows of a
// deleted provider. The durable delete already succeeded, so failures log
// and continue: orphaned cache rows never match a provider lookup and the
// next delete or prune pass converges them.
func (s *GormService) clearProviderCache(ctx context.Context, providerID string, credentialIDs []string) {
	if len(credentialIDs) > 0 {
		values := make([]any, 0, len(credentialIDs))
		for _, identifier := range credentialIDs {
			values = append(values, identifier)
		}
		condition := clause.IN{Column: "credential_id", Values: values}
		if _, err := gorm.G[record.CredentialPark](s.database.Cache, condition).Delete(ctx); err != nil && s.logger != nil {
			s.logger.Warn("provider cache park cleanup failed", "provider_id", providerID, "err", err)
		}
		if _, err := gorm.G[record.CredentialHealth](s.database.Cache, condition).Delete(ctx); err != nil && s.logger != nil {
			s.logger.Warn("provider cache health cleanup failed", "provider_id", providerID, "err", err)
		}
	}
	provider := clause.Eq{Column: "provider_id", Value: providerID}
	if _, err := gorm.G[record.ModelInfoCapability](s.database.Cache, provider).Delete(ctx); err != nil && s.logger != nil {
		s.logger.Warn("provider cache model capability cleanup failed", "provider_id", providerID, "err", err)
	}
	if _, err := gorm.G[record.ModelInfoModality](s.database.Cache, provider).Delete(ctx); err != nil && s.logger != nil {
		s.logger.Warn("provider cache model modality cleanup failed", "provider_id", providerID, "err", err)
	}
	if _, err := gorm.G[record.ModelInfoEndpoint](s.database.Cache, provider).Delete(ctx); err != nil && s.logger != nil {
		s.logger.Warn("provider cache model endpoint cleanup failed", "provider_id", providerID, "err", err)
	}
	if _, err := gorm.G[record.ModelInfo](s.database.Cache, provider).Delete(ctx); err != nil && s.logger != nil {
		s.logger.Warn("provider cache model cleanup failed", "provider_id", providerID, "err", err)
	}
	if _, err := gorm.G[record.ModelInfoSnapshot](s.database.Cache, provider).Delete(ctx); err != nil && s.logger != nil {
		s.logger.Warn("provider cache model snapshot cleanup failed", "provider_id", providerID, "err", err)
	}
}

// uniqueProviderIDInTransaction generates the next free provider identifier
// on the transaction handle. The scheme mirrors the previous uniqueID: the
// qualifier slug when present, the custom name slug for custom rows, else
// the bare type key, with a numeric suffix on collision.
func uniqueProviderIDInTransaction(ctx context.Context, transaction *gorm.DB, typeKey, qualifier, name string) (string, error) {
	var base string
	switch {
	case qualifier != "":
		base = typeKey + ":" + util.SlugifyOrDefault(qualifier, "provider")
	case typeKey == TypeCustom:
		base = "custom:" + util.SlugifyOrDefault(name, "provider")
	default:
		base = typeKey
	}
	identifier := base
	for counter := 2; ; counter++ {
		_, err := gorm.G[record.ProviderInstance](transaction, clause.Eq{Column: "id", Value: identifier}).First(ctx)
		if err != nil {
			if stdErrors.Is(db.Translate(err), apierrors.ErrNotFound) {
				return identifier, nil
			}
			return "", db.Translate(err)
		}
		identifier = fmt.Sprintf("%s-%d", base, counter)
	}
}

// cloneProviderConfig copies a configuration map so normalization never
// mutates the caller map. A missing map reads as an empty map.
func cloneProviderConfig(configuration map[string]any) map[string]any {
	cloned := make(map[string]any, len(configuration))
	for key, value := range configuration {
		cloned[key] = value
	}
	return cloned
}
