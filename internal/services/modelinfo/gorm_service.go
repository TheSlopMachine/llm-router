package modelinfo

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	stdErrors "errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

// GormService manages model overrides and the per-provider model metadata
// cache with explicit converters between wire types and override plus
// snapshot rows. Overrides live in the durable database; snapshots and
// cached model rows live in the cache file and reference providers softly.
// The discovery layer (memory cache,
// upstream fetch, merged views) rides the same handles: persistence stamps
// the header verbatim and never expires reads. Traditional GORM use: none;
// every statement runs through gorm.G with typed clauses.
type GormService struct {
	database          *db.Database
	providerService   *provider.GormService
	credentialService *credential.GormService
	cacheTTL          time.Duration
	logger            *slog.Logger

	mu       sync.RWMutex
	cache    map[string]cacheEntry
	inflight map[string]*inflightCall
}

// NewGorm constructs a GORM-backed model info service on the SQLite handle.
// Construction performs no I/O.
func NewGorm(database *db.Database, providerService *provider.GormService, credentialService *credential.GormService, cacheTTL time.Duration) *GormService {
	service := &GormService{database: database}
	service.wireDiscovery(providerService, credentialService, cacheTTL)
	return service
}

// orderOverridesByModelName sorts override rows by model name.
func orderOverridesByModelName() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "model_name"}}}}
}

// orderModelInfosByModelName sorts cached model rows by model name.
func orderModelInfosByModelName() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "model_name"}}}}
}

// orderCapabilities sorts capability child rows by chip.
func orderCapabilities() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "capability"}}}}
}

// orderModalities sorts modality child rows by direction then modality.
func orderModalities() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "direction"}}, {Column: clause.Column{Name: "modality"}}}}
}

// orderEndpoints sorts endpoint child rows by endpoint.
func orderEndpoints() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "endpoint"}}}}
}

// requireHandle fails closed when either SQLite handle is missing.
func (s *GormService) requireHandle() (*db.Database, error) {
	if s == nil || s.database == nil || s.database.Handle == nil || s.database.Cache == nil {
		return nil, fmt.Errorf("model info sqlite handle is not configured: %w", apierrors.ErrInvalidInput)
	}
	return s.database, nil
}

// SetOverride creates or replaces the override for one model. The base row
// upserts on the composite key; capability and modality children replace
// wholesale so removed chips disappear. One transaction holds the upsert,
// the child deletes, and the child inserts on the transaction handle only.
// The provider row must exist: a dangling provider fails through the
// foreign key as invalid input.
func (s *GormService) SetOverride(ctx context.Context, override models.ModelOverride) error {
	if override.ProviderID == "" || override.Name == "" {
		if s.logger != nil {
			s.logger.Warn("Model override rejected", "operation", "set override", "error", "missing provider id or model name")
		}
		return fmt.Errorf("override requires provider id and model name: %w", apierrors.ErrInvalidInput)
	}
	if _, err := s.requireHandle(); err != nil {
		if s.logger != nil {
			s.logger.Error("Model override rejected", "provider_id", override.ProviderID, "model", override.Name, "operation", "set override", "error", err)
		}
		return err
	}
	base := modelOverrideRecordFromModel(override)
	capabilities := modelOverrideCapabilityRowsFromModel(override)
	modalities := modelOverrideModalityRowsFromModel(override)
	err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		bases := []record.ModelOverride{base}
		if err := gorm.G[record.ModelOverride](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "provider_id"}, {Name: "model_name"}}, UpdateAll: true}).CreateInBatches(ctx, &bases, 500); err != nil {
			return db.Translate(err)
		}
		if err := deleteOverrideChildren(ctx, transaction, override.ProviderID, override.Name); err != nil {
			return err
		}
		if len(capabilities) > 0 {
			if err := gorm.G[record.ModelOverrideCapability](transaction).CreateInBatches(ctx, &capabilities, 500); err != nil {
				return db.Translate(err)
			}
		}
		if len(modalities) > 0 {
			if err := gorm.G[record.ModelOverrideModality](transaction).CreateInBatches(ctx, &modalities, 500); err != nil {
				return db.Translate(err)
			}
		}
		return nil
	})
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("Model override store failed", "provider_id", override.ProviderID, "model", override.Name, "operation", "set override", "error", err)
		}
		return err
	}
	if s.logger != nil {
		s.logger.Debug("Model override stored", "provider_id", override.ProviderID, "model", override.Name, "operation", "set override")
	}
	return nil
}

// GetOverride returns the override for one model with its child lists. A
// miss reports the not-found sentinel with override context.
func (s *GormService) GetOverride(ctx context.Context, providerID, modelName string) (*models.ModelOverride, error) {
	if _, err := s.requireHandle(); err != nil {
		if s.logger != nil {
			s.logger.Error("Model override load failed", "provider_id", providerID, "model", modelName, "operation", "get override", "error", err)
		}
		return nil, err
	}
	var result *models.ModelOverride
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		base, err := gorm.G[record.ModelOverride](transaction, clause.Eq{Column: "provider_id", Value: providerID}, clause.Eq{Column: "model_name", Value: modelName}).First(ctx)
		if err != nil {
			translated := db.Translate(err)
			if stdErrors.Is(translated, apierrors.ErrNotFound) {
				return apierrors.NewNotFoundError("model override", providerID+"/"+modelName)
			}
			return translated
		}
		capabilities, inputModalities, outputModalities, err := loadOverrideChildren(ctx, transaction, providerID, modelName)
		if err != nil {
			return err
		}
		result = modelOverrideFromRecords(base, capabilities, inputModalities, outputModalities)
		return nil
	}); err != nil {
		if s.logger != nil {
			if stdErrors.Is(err, apierrors.ErrNotFound) {
				s.logger.Debug("Model override miss", "provider_id", providerID, "model", modelName, "operation", "get override")
			} else {
				s.logger.Warn("Model override load failed", "provider_id", providerID, "model", modelName, "operation", "get override", "error", err)
			}
		}
		return nil, err
	}
	if s.logger != nil {
		s.logger.Debug("Model override loaded", "provider_id", providerID, "model", modelName, "operation", "get override")
	}
	return result, nil
}

// DeleteOverride removes the override for one model. Capability and
// modality rows cascade from the composite foreign key; no child scan runs.
// A zero-row delete reports the not-found sentinel through the WithResult
// tracker.
func (s *GormService) DeleteOverride(ctx context.Context, providerID, modelName string) error {
	if _, err := s.requireHandle(); err != nil {
		if s.logger != nil {
			s.logger.Error("Model override delete failed", "provider_id", providerID, "model", modelName, "operation", "delete override", "error", err)
		}
		return err
	}
	err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.ModelOverride](transaction, clause.Eq{Column: "provider_id", Value: providerID}, clause.Eq{Column: "model_name", Value: modelName}, tracker).Delete(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if affected == 0 || tracker.RowsAffected == 0 {
			return fmt.Errorf("model override %q: %w", providerID+"/"+modelName, apierrors.ErrNotFound)
		}
		return nil
	})
	if err != nil {
		if s.logger != nil {
			if stdErrors.Is(err, apierrors.ErrNotFound) {
				s.logger.Debug("Model override delete miss", "provider_id", providerID, "model", modelName, "operation", "delete override")
			} else {
				s.logger.Warn("Model override delete failed", "provider_id", providerID, "model", modelName, "operation", "delete override", "error", err)
			}
		}
		return err
	}
	if s.logger != nil {
		s.logger.Debug("Model override deleted", "provider_id", providerID, "model", modelName, "operation", "delete override")
	}
	return nil
}

// ListOverrides returns all overrides of a provider ordered by model name
// with child lists rebuilt per row. Empty providers yield an empty slice,
// never an error.
func (s *GormService) ListOverrides(ctx context.Context, providerID string) ([]*models.ModelOverride, error) {
	if _, err := s.requireHandle(); err != nil {
		if s.logger != nil {
			s.logger.Error("Model override list failed", "provider_id", providerID, "operation", "list overrides", "error", err)
		}
		return nil, err
	}
	var result []*models.ModelOverride
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		bases, err := gorm.G[record.ModelOverride](transaction, clause.Eq{Column: "provider_id", Value: providerID}, orderOverridesByModelName()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		capabilityRows, err := gorm.G[record.ModelOverrideCapability](transaction, clause.Eq{Column: "provider_id", Value: providerID}, orderCapabilities()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		modalityRows, err := gorm.G[record.ModelOverrideModality](transaction, clause.Eq{Column: "provider_id", Value: providerID}, orderModalities()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		capabilitiesByModel := map[string][]string{}
		for _, row := range capabilityRows {
			capabilitiesByModel[row.ModelName] = append(capabilitiesByModel[row.ModelName], row.Capability)
		}
		inputByModel := map[string][]string{}
		outputByModel := map[string][]string{}
		for _, row := range modalityRows {
			switch row.Direction {
			case modalityDirectionInput:
				inputByModel[row.ModelName] = append(inputByModel[row.ModelName], row.Modality)
			case modalityDirectionOutput:
				outputByModel[row.ModelName] = append(outputByModel[row.ModelName], row.Modality)
			default:
				return fmt.Errorf("unknown override modality direction %q: %w", row.Direction, apierrors.ErrInvalidInput)
			}
		}
		overrides := make([]*models.ModelOverride, 0, len(bases))
		for _, base := range bases {
			overrides = append(overrides, modelOverrideFromRecords(base, capabilitiesByModel[base.ModelName], inputByModel[base.ModelName], outputByModel[base.ModelName]))
		}
		result = overrides
		return nil
	}); err != nil {
		if s.logger != nil {
			s.logger.Warn("Model override list failed", "provider_id", providerID, "operation", "list overrides", "error", err)
		}
		return nil, err
	}
	if s.logger != nil {
		s.logger.Debug("Model override list loaded", "provider_id", providerID, "operation", "list overrides", "count", len(result))
	}
	return result, nil
}

// IsModelEnabled reports whether a model may be routed. Models are enabled
// unless an override disables them. Missing overrides allow routing;
// storage failures deny routing.
func (s *GormService) IsModelEnabled(ctx context.Context, providerID, modelName string) (bool, error) {
	override, err := s.GetOverride(ctx, providerID, modelName)
	if err != nil {
		if stdErrors.Is(err, apierrors.ErrNotFound) {
			return true, nil
		}
		return false, err
	}
	return !override.Disabled, nil
}

// PutSnapshot replaces the cached model list of one provider. The header
// upserts on the provider key; model rows replace wholesale so dropped
// models disappear, and capability, modality, and endpoint children follow
// their model rows. One transaction holds the header upsert, the model
// delete, and every insert on the cache handle only. Snapshots reference
// the provider row softly across files: a dangling provider persists
// instead of failing, and provider deletion clears its snapshots
// explicitly.
func (s *GormService) PutSnapshot(ctx context.Context, providerID string, modelList []models.ModelInfo, cachedAt time.Time) error {
	if providerID == "" {
		if s.logger != nil {
			s.logger.Warn("Model snapshot rejected", "operation", "put snapshot", "error", "missing provider id")
		}
		return fmt.Errorf("snapshot requires provider id: %w", apierrors.ErrInvalidInput)
	}
	if _, err := s.requireHandle(); err != nil {
		if s.logger != nil {
			s.logger.Error("Model snapshot store failed", "provider_id", providerID, "operation", "put snapshot", "error", err)
		}
		return err
	}
	header := record.ModelInfoSnapshot{ProviderID: providerID, CachedAt: cachedAt.UTC()}
	modelRows := make([]record.ModelInfo, 0, len(modelList))
	capabilityRows := []record.ModelInfoCapability{}
	modalityRows := []record.ModelInfoModality{}
	endpointRows := []record.ModelInfoEndpoint{}
	for _, model := range modelList {
		row, capabilities, modalities, endpoints, err := modelInfoRecordFromModel(providerID, model)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Model snapshot store failed", "provider_id", providerID, "model", model.Name, "operation", "put snapshot", "error", err)
			}
			return err
		}
		modelRows = append(modelRows, row)
		capabilityRows = append(capabilityRows, capabilities...)
		modalityRows = append(modalityRows, modalities...)
		endpointRows = append(endpointRows, endpoints...)
	}
	err := s.database.Cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		headers := []record.ModelInfoSnapshot{header}
		if err := gorm.G[record.ModelInfoSnapshot](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "provider_id"}}, UpdateAll: true}).CreateInBatches(ctx, &headers, 500); err != nil {
			return db.Translate(err)
		}
		if _, err := gorm.G[record.ModelInfo](transaction, clause.Eq{Column: "provider_id", Value: providerID}).Delete(ctx); err != nil {
			return db.Translate(err)
		}
		if len(modelRows) > 0 {
			if err := gorm.G[record.ModelInfo](transaction).CreateInBatches(ctx, &modelRows, 500); err != nil {
				return db.Translate(err)
			}
		}
		if len(capabilityRows) > 0 {
			if err := gorm.G[record.ModelInfoCapability](transaction).CreateInBatches(ctx, &capabilityRows, 500); err != nil {
				return db.Translate(err)
			}
		}
		if len(modalityRows) > 0 {
			if err := gorm.G[record.ModelInfoModality](transaction).CreateInBatches(ctx, &modalityRows, 500); err != nil {
				return db.Translate(err)
			}
		}
		if len(endpointRows) > 0 {
			if err := gorm.G[record.ModelInfoEndpoint](transaction).CreateInBatches(ctx, &endpointRows, 500); err != nil {
				return db.Translate(err)
			}
		}
		return nil
	})
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("Model snapshot store failed", "provider_id", providerID, "operation", "put snapshot", "count", len(modelList), "error", err)
		}
		return err
	}
	if s.logger != nil {
		s.logger.Debug("Model snapshot stored", "provider_id", providerID, "operation", "put snapshot", "count", len(modelList))
	}
	return nil
}

// GetSnapshot returns the cached model list of one provider ordered by
// model name with child lists rebuilt per row, plus the stamped header
// time. A missing header reports the not-found sentinel; the TTL check
// stays with the caller. Corrupt reasoning payloads surface an error.
func (s *GormService) GetSnapshot(ctx context.Context, providerID string) ([]models.ModelInfo, time.Time, error) {
	if _, err := s.requireHandle(); err != nil {
		if s.logger != nil {
			s.logger.Error("Model snapshot load failed", "provider_id", providerID, "operation", "get snapshot", "error", err)
		}
		return nil, time.Time{}, err
	}
	var modelList []models.ModelInfo
	var cachedAt time.Time
	if err := s.database.Cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		header, err := gorm.G[record.ModelInfoSnapshot](transaction, clause.Eq{Column: "provider_id", Value: providerID}).First(ctx)
		if err != nil {
			translated := db.Translate(err)
			if stdErrors.Is(translated, apierrors.ErrNotFound) {
				return apierrors.NewNotFoundError("model info snapshot", providerID)
			}
			return translated
		}
		rows, err := gorm.G[record.ModelInfo](transaction, clause.Eq{Column: "provider_id", Value: providerID}, orderModelInfosByModelName()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		capabilityRows, err := gorm.G[record.ModelInfoCapability](transaction, clause.Eq{Column: "provider_id", Value: providerID}, orderCapabilities()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		modalityRows, err := gorm.G[record.ModelInfoModality](transaction, clause.Eq{Column: "provider_id", Value: providerID}, orderModalities()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		endpointRows, err := gorm.G[record.ModelInfoEndpoint](transaction, clause.Eq{Column: "provider_id", Value: providerID}, orderEndpoints()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		capabilitiesByModel := map[string][]string{}
		for _, row := range capabilityRows {
			capabilitiesByModel[row.ModelName] = append(capabilitiesByModel[row.ModelName], row.Capability)
		}
		inputByModel := map[string][]string{}
		outputByModel := map[string][]string{}
		for _, row := range modalityRows {
			switch row.Direction {
			case modalityDirectionInput:
				inputByModel[row.ModelName] = append(inputByModel[row.ModelName], row.Modality)
			case modalityDirectionOutput:
				outputByModel[row.ModelName] = append(outputByModel[row.ModelName], row.Modality)
			default:
				return fmt.Errorf("unknown model modality direction %q: %w", row.Direction, apierrors.ErrInvalidInput)
			}
		}
		endpointsByModel := map[string][]string{}
		for _, row := range endpointRows {
			endpointsByModel[row.ModelName] = append(endpointsByModel[row.ModelName], row.Endpoint)
		}
		models := make([]models.ModelInfo, 0, len(rows))
		for _, row := range rows {
			model, err := modelInfoFromRecord(row, capabilitiesByModel[row.ModelName], inputByModel[row.ModelName], outputByModel[row.ModelName], endpointsByModel[row.ModelName])
			if err != nil {
				return err
			}
			models = append(models, model)
		}
		modelList = models
		cachedAt = header.CachedAt.UTC()
		return nil
	}); err != nil {
		if s.logger != nil {
			if stdErrors.Is(err, apierrors.ErrNotFound) {
				s.logger.Debug("Model snapshot miss", "provider_id", providerID, "operation", "get snapshot")
			} else {
				s.logger.Warn("Model snapshot load failed", "provider_id", providerID, "operation", "get snapshot", "error", err)
			}
		}
		return nil, time.Time{}, err
	}
	if s.logger != nil {
		s.logger.Debug("Model snapshot loaded", "provider_id", providerID, "operation", "get snapshot", "count", len(modelList))
	}
	return modelList, cachedAt, nil
}

// DeleteSnapshot removes the cached model list of one provider. Model rows
// cascade from the header delete with no model scan; capability, modality,
// and endpoint rows cascade from their model rows. A zero-row delete
// reports the not-found sentinel through the WithResult tracker.
func (s *GormService) DeleteSnapshot(ctx context.Context, providerID string) error {
	if _, err := s.requireHandle(); err != nil {
		if s.logger != nil {
			s.logger.Error("Model snapshot delete failed", "provider_id", providerID, "operation", "delete snapshot", "error", err)
		}
		return err
	}
	err := s.database.Cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.ModelInfoSnapshot](transaction, clause.Eq{Column: "provider_id", Value: providerID}, tracker).Delete(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if affected == 0 || tracker.RowsAffected == 0 {
			return fmt.Errorf("model info snapshot %q: %w", providerID, apierrors.ErrNotFound)
		}
		return nil
	})
	if err != nil {
		if s.logger != nil {
			if stdErrors.Is(err, apierrors.ErrNotFound) {
				s.logger.Debug("Model snapshot delete miss", "provider_id", providerID, "operation", "delete snapshot")
			} else {
				s.logger.Warn("Model snapshot delete failed", "provider_id", providerID, "operation", "delete snapshot", "error", err)
			}
		}
		return err
	}
	if s.logger != nil {
		s.logger.Debug("Model snapshot deleted", "provider_id", providerID, "operation", "delete snapshot")
	}
	return nil
}

// RemoveModel drops one model from the cached list of one provider.
// Capability, modality, and endpoint rows cascade from the model delete. A
// missing provider or model is a no-op reporting false. The memory entry
// filters in step so peeks observe the removal without a refetch.
func (s *GormService) RemoveModel(ctx context.Context, providerID, modelName string) (bool, error) {
	if _, err := s.requireHandle(); err != nil {
		if s.logger != nil {
			s.logger.Error("Model remove failed", "provider_id", providerID, "model", modelName, "operation", "remove model", "error", err)
		}
		return false, err
	}
	var removed bool
	if err := s.database.Cache.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.ModelInfo](transaction, clause.Eq{Column: "provider_id", Value: providerID}, clause.Eq{Column: "model_name", Value: modelName}, tracker).Delete(ctx)
		if err != nil {
			return db.Translate(err)
		}
		removed = affected > 0 && tracker.RowsAffected > 0
		return nil
	}); err != nil {
		if s.logger != nil {
			s.logger.Warn("Model remove failed", "provider_id", providerID, "model", modelName, "operation", "remove model", "error", err)
		}
		return false, err
	}
	if removed {
		s.mu.Lock()
		if entry, ok := s.cache[providerID]; ok {
			kept := make([]models.ModelInfo, 0, len(entry.models))
			for _, model := range entry.models {
				if model.Name != modelName {
					kept = append(kept, model)
				}
			}
			entry.models = kept
			s.cache[providerID] = entry
		}
		s.mu.Unlock()
		if s.logger != nil {
			s.logger.Debug("Model removed", "provider_id", providerID, "model", modelName, "operation", "remove model")
		}
	} else if s.logger != nil {
		s.logger.Debug("Model remove miss", "provider_id", providerID, "model", modelName, "operation", "remove model")
	}
	return removed, nil
}

// deleteOverrideChildren removes the capability and modality rows of one
// override on the transaction handle. Every delete carries both key
// columns.
func deleteOverrideChildren(ctx context.Context, transaction *gorm.DB, providerID, modelName string) error {
	if _, err := gorm.G[record.ModelOverrideCapability](transaction, clause.Eq{Column: "provider_id", Value: providerID}, clause.Eq{Column: "model_name", Value: modelName}).Delete(ctx); err != nil {
		return db.Translate(err)
	}
	if _, err := gorm.G[record.ModelOverrideModality](transaction, clause.Eq{Column: "provider_id", Value: providerID}, clause.Eq{Column: "model_name", Value: modelName}).Delete(ctx); err != nil {
		return db.Translate(err)
	}
	return nil
}

// loadOverrideChildren rebuilds the capability and modality lists of one
// override on the transaction handle. Unknown directions fail loudly
// instead of dropping the row.
func loadOverrideChildren(ctx context.Context, transaction *gorm.DB, providerID, modelName string) ([]string, []string, []string, error) {
	capabilityRows, err := gorm.G[record.ModelOverrideCapability](transaction, clause.Eq{Column: "provider_id", Value: providerID}, clause.Eq{Column: "model_name", Value: modelName}, orderCapabilities()).Find(ctx)
	if err != nil {
		return nil, nil, nil, db.Translate(err)
	}
	modalityRows, err := gorm.G[record.ModelOverrideModality](transaction, clause.Eq{Column: "provider_id", Value: providerID}, clause.Eq{Column: "model_name", Value: modelName}, orderModalities()).Find(ctx)
	if err != nil {
		return nil, nil, nil, db.Translate(err)
	}
	var capabilities []string
	for _, row := range capabilityRows {
		capabilities = append(capabilities, row.Capability)
	}
	var inputModalities []string
	var outputModalities []string
	for _, row := range modalityRows {
		switch row.Direction {
		case modalityDirectionInput:
			inputModalities = append(inputModalities, row.Modality)
		case modalityDirectionOutput:
			outputModalities = append(outputModalities, row.Modality)
		default:
			return nil, nil, nil, fmt.Errorf("unknown override modality direction %q: %w", row.Direction, apierrors.ErrInvalidInput)
		}
	}
	return capabilities, inputModalities, outputModalities, nil
}
