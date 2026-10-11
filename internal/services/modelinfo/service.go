// Package modelinfo manages model metadata caching.
//
// This service provides a caching layer between the dashboard UI and provider backends,
// reducing API calls and improving performance.
package modelinfo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// Service manages model metadata caching
type Service struct {
	providerService   *provider.Service
	credentialService *credential.Service
	cacheTTL          time.Duration
	logger            *slog.Logger
	overrides         *repository.Repository[models.ModelOverride]
	records           *repository.Repository[modelInfoRecord]

	mu       sync.RWMutex
	cache    map[string]cacheEntry
	inflight map[string]*inflightCall
}

type cacheEntry struct {
	models    []models.ModelInfo
	cachedAt  time.Time
	expiresAt time.Time
}

// inflightCall tracks one in-progress upstream fetch so concurrent waiters
// share the leader's result, including its error, instead of stampeding.
type inflightCall struct {
	waitGroup sync.WaitGroup
	err       error
}

// modelInfoRecord is the persisted form of a cache entry: the model cache
// lives in bbolt and survives restarts.
type modelInfoRecord struct {
	Models   []models.ModelInfo `json:"models"`
	CachedAt time.Time          `json:"cached_at"`
}

// New creates a ModelInfo service
func New(database *db.DB, providerService *provider.Service, credentialService *credential.Service, cacheTTL time.Duration) *Service {
	if cacheTTL == 0 {
		cacheTTL = 1 * time.Hour
	}

	return &Service{
		providerService:   providerService,
		credentialService: credentialService,
		cacheTTL:          cacheTTL,
		overrides:         repository.New[models.ModelOverride](database, db.BucketModelOverrides, "model_override"),
		records:           repository.New[modelInfoRecord](database, db.BucketModelInfos, "model_info_record"),
		cache:             make(map[string]cacheEntry),
		inflight:          make(map[string]*inflightCall),
	}
}

// SetLogger wires structured logging (called once from server.New).
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

// PeekModelInfos returns the cached model list without triggering an upstream
// fetch; stale entries count, and a memory miss hydrates from the persisted
// record. A true miss returns nil — callers that merely display a count must
// not ping the provider (auto-sync is opt-in via config.models_auto_sync;
// everything else syncs on explicit user action or the startup warm).
func (s *Service) PeekModelInfos(providerID string) []models.ModelInfo {
	s.mu.RLock()
	entry, ok := s.cache[providerID]
	s.mu.RUnlock()
	if ok {
		return stripModalityCapabilities(entry.models)
	}
	record, err := s.records.Get(providerID)
	if err != nil || record == nil {
		return nil
	}
	s.mu.Lock()
	s.cache[providerID] = cacheEntry{
		models:    record.Models,
		cachedAt:  record.CachedAt,
		expiresAt: record.CachedAt.Add(s.cacheTTL),
	}
	s.mu.Unlock()
	return stripModalityCapabilities(record.Models)
}

// modalityCapabilities are capability chips duplicated by the modalities column.
var modalityCapabilities = map[string]bool{"vision": true, "audio": true}

// stripModalityCapabilities drops modality-duplicate chips. Capabilities render in
// their own column; modalities render separately. Copies: cached slices
// stay untouched.
func stripModalityCapabilities(modelList []models.ModelInfo) []models.ModelInfo {
	output := make([]models.ModelInfo, 0, len(modelList))
	for _, model := range modelList {
		if len(model.Capabilities) == 0 {
			output = append(output, model)
			continue
		}
		kept := make([]string, 0, len(model.Capabilities))
		for _, c := range model.Capabilities {
			if !modalityCapabilities[c] {
				kept = append(kept, c)
			}
		}
		model.Capabilities = kept
		output = append(output, model)
	}
	return output
}

// WarmMissing creates model caches for providers that have none. Runs in the
// background at startup; refresh afterwards stays opt-in (models_auto_sync)
// or manual.
func (s *Service) WarmMissing(ctx context.Context) {
	providers, err := s.providerService.List()
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("model cache warm: list providers failed", "err", err)
		}
		return
	}
	for _, provider := range providers {
		if ctx.Err() != nil {
			return
		}
		if provider.Disabled {
			continue
		}
		if len(s.PeekModelInfos(provider.ID)) > 0 {
			continue
		}
		if _, err := s.GetModelInfos(ctx, provider.ID); err != nil && s.logger != nil {
			s.logger.Warn("model cache warm failed", "provider_id", provider.ID, "err", err)
		}
	}
}

// GetModelInfos retrieves all model metadata for a provider
func (s *Service) GetModelInfos(ctx context.Context, providerID string) ([]models.ModelInfo, error) {
	s.mu.RLock()
	if entry, exists := s.cache[providerID]; exists && util.Now().Before(entry.expiresAt) {
		s.mu.RUnlock()
		return stripModalityCapabilities(entry.models), nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	call, exists := s.inflight[providerID]
	if exists {
		s.mu.Unlock()
		call.waitGroup.Wait()

		s.mu.RLock()
		defer s.mu.RUnlock()
		if entry, exists := s.cache[providerID]; exists {
			return stripModalityCapabilities(entry.models), nil
		}
		if call.err != nil {
			return nil, call.err
		}
		return nil, fmt.Errorf("fetch failed for provider %s", providerID)
	}

	call = &inflightCall{}
	call.waitGroup.Add(1)
	s.inflight[providerID] = call
	s.mu.Unlock()

	modelInfos, err := s.fetchAndCache(ctx, providerID)
	call.err = err
	s.mu.Lock()
	delete(s.inflight, providerID)
	s.mu.Unlock()
	call.waitGroup.Done()
	return modelInfos, err
}

// GetModelInfo retrieves metadata for a specific model
func (s *Service) GetModelInfo(ctx context.Context, modelID models.ModelId) (*models.ModelInfo, error) {
	providerID, modelName, err := modelID.Parse()
	if err != nil {
		return nil, err
	}

	modelInfos, err := s.GetModelInfos(ctx, providerID)
	if err != nil {
		return nil, err
	}

	for i := range modelInfos {
		if modelInfos[i].Name == modelName {
			return &modelInfos[i], nil
		}
	}

	return nil, fmt.Errorf("model %q not found for provider %s", modelName, providerID)
}

// GetModels retrieves just the model names for a provider
func (s *Service) GetModels(ctx context.Context, providerID string) ([]string, error) {
	modelInfos, err := s.GetModelInfos(ctx, providerID)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(modelInfos))
	for i, m := range modelInfos {
		names[i] = m.Name
	}
	return names, nil
}

// fetchAndCache fetches model metadata from the provider backend and caches it.
func (s *Service) fetchAndCache(ctx context.Context, providerID string) ([]models.ModelInfo, error) {
	resolved, err := provider.Resolve(s.providerService, providerID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("model discovery: provider lookup failed", "provider_id", providerID, "err", err)
		}
		return nil, fmt.Errorf("provider lookup failed: %w", err)
	}
	instance := resolved.Instance
	config := instance.Config

	credentials, err := s.credentialService.ListByProvider(providerID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("model discovery: list credentials failed", "provider_id", providerID, "err", err)
		}
		return nil, fmt.Errorf("list credentials for provider %s: %w", instance.Name, err)
	}
	// Lua plugins select their own discovery credential internally: one
	// call serves every case, credentialed or not. The provider ID scopes
	// credentials.list() to this provider's rows.
	if resolved.IsLua() {
		modelInfos, err := s.providerService.LuaService().GetModelInfos(ctx, providerID, instance.TypeKey, config)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("model discovery failed", "provider_id", providerID, "qualifier", instance.Qualifier, "err", err)
			}
			return nil, fmt.Errorf("provider %q discovery: %w", providerID, err)
		}
		if s.logger != nil {
			s.logger.Debug("model discovery succeeded", "provider_id", providerID, "models", len(modelInfos))
		}
		return s.store(providerID, modelInfos), nil
	}
	var modelInfos []models.ModelInfo
	var lastErr error

	fetch := func(credential *models.Credential) ([]models.ModelInfo, error) {
		return safeGetModelInfos(ctx, resolved.Go, credential, config)
	}

	// Credential-free fetch first for backends that do not need credentials
	// for discovery, such as the built-in agents provider or keyless plugins.
	if len(credentials) == 0 {
		modelInfos, lastErr = fetch(nil)
		if lastErr == nil {
			if s.logger != nil {
				s.logger.Debug("model discovery succeeded (no credential)", "provider_id", providerID, "models", len(modelInfos))
			}
			return s.store(providerID, modelInfos), nil
		}
		if s.logger != nil {
			s.logger.Warn("model discovery failed", "provider_id", providerID, "qualifier", instance.Qualifier, "err", lastErr, "attempts", 1)
		}
		return nil, fmt.Errorf("no credentials available for provider %s: %w", instance.Name, lastErr)
	}

	for _, credential := range credentials {
		modelInfos, lastErr = fetch(credential)
		if lastErr == nil {
			if s.logger != nil {
				s.logger.Debug("model discovery succeeded", "provider_id", providerID, "credential_id", credential.ID, "models", len(modelInfos))
			}
			return s.store(providerID, modelInfos), nil
		}
	}

	if s.logger != nil {
		s.logger.Warn("model discovery failed", "provider_id", providerID, "qualifier", instance.Qualifier, "err", lastErr, "attempts", len(credentials))
	}
	return nil, fmt.Errorf("provider %q discovery: %w", providerID, lastErr)
}

func safeGetModelInfos(
	ctx context.Context,
	adapter provider.GoAdapter,
	credential *models.Credential,
	providerConfig map[string]any,
) (modelInfos []models.ModelInfo, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("adapter GetModelInfos panicked: %v", r)
		}
	}()

	return adapter.GetModelInfos(ctx, credential, providerConfig)
}

// Refresh forces an upstream fetch and stores it. The previous cache is
// kept when the fetch fails: until a new list arrives the old one counts
// as current.
func (s *Service) Refresh(ctx context.Context, providerID string) ([]models.ModelInfo, error) {
	return s.fetchAndCache(ctx, providerID)
}

// mergeRetained refreshes known models from the fresh fetch and appends
// names the fetch introduces. Records are keyed by full model id
// (provider + name): the same name on another provider is a separate
// record verified separately. Known models take the fresh row: corrected
// plugin metadata (modalities, endpoints, capabilities) must reach the
// cache; admin intent lives in the overrides bucket, never in cached rows.
// Names the fresh fetch omits are retained: eviction of models the upstream
// drops is request-driven, not fetch-driven (the router removes a model via
// RemoveModel when the backend reports ErrorTypeNotFound, so transiently
// omitted names survive one flap while confirmed removals disappear on next
// use). Manual and custom models live in the overrides bucket and never
// pass through here.
func (s *Service) mergeRetained(providerID string, fresh []models.ModelInfo) []models.ModelInfo {
	existing := s.PeekModelInfos(providerID)
	if len(existing) == 0 {
		return fresh
	}
	byName := make(map[string]models.ModelInfo, len(fresh))
	for _, model := range fresh {
		byName[model.Name] = model
	}
	merged := make([]models.ModelInfo, 0, len(existing)+len(fresh))
	seen := make(map[string]bool, len(existing))
	for _, model := range existing {
		if updated, ok := byName[model.Name]; ok {
			merged = append(merged, updated)
		} else {
			merged = append(merged, model)
		}
		seen[model.Name] = true
	}
	for _, model := range fresh {
		if !seen[model.Name] {
			merged = append(merged, model)
			seen[model.Name] = true
		}
	}
	return merged
}

// RemoveModel drops one model from the cached list after the upstream
// reports it does not exist. A missing provider record is a no-op.
// Reports whether anything was removed.
func (s *Service) RemoveModel(providerID, name string) (bool, error) {
	record, err := s.records.Get(providerID)
	if err != nil || record == nil {
		return false, nil
	}
	kept := make([]models.ModelInfo, 0, len(record.Models))
	removed := false
	for _, model := range record.Models {
		if model.Name == name {
			removed = true
			continue
		}
		kept = append(kept, model)
	}
	if !removed {
		return false, nil
	}
	s.mu.Lock()
	if e, ok := s.cache[providerID]; ok {
		e.models = append([]models.ModelInfo(nil), kept...)
		s.cache[providerID] = e
	}
	s.mu.Unlock()
	if err := s.records.Put(providerID, &modelInfoRecord{Models: kept, CachedAt: record.CachedAt}); err != nil {
		return false, err
	}
	return true, nil
}

// InvalidateProvider clears cache for a specific provider
func (s *Service) InvalidateProvider(providerID string) error {
	s.mu.Lock()
	delete(s.cache, providerID)
	s.mu.Unlock()
	return s.records.DeleteIfExists(providerID)
}

// InvalidateAll clears entire cache
func (s *Service) InvalidateAll() error {
	s.mu.Lock()
	s.cache = make(map[string]cacheEntry)
	s.mu.Unlock()
	providers, err := s.providerService.List()
	if err != nil {
		return err
	}
	for _, provider := range providers {
		_ = s.records.DeleteIfExists(provider.ID)
	}
	return nil
}

// ─────────────────────────────────────────────
// Model overrides: enable/disable, custom models, display metadata
// ─────────────────────────────────────────────

func overrideKey(providerID, modelName string) string {
	return providerID + "/" + modelName
}

// ModelView is a model merged with its admin override state.
type ModelView struct {
	models.ModelInfo
	Disabled bool `json:"disabled"`
	Custom   bool `json:"custom"`
}

// SetOverride creates or replaces the override for one model.
func (s *Service) SetOverride(override models.ModelOverride) error {
	if override.ProviderID == "" || override.Name == "" {
		return fmt.Errorf("override requires provider id and model name")
	}
	return s.overrides.Put(overrideKey(override.ProviderID, override.Name), &override)
}

// DeleteOverride removes the override for one model. For custom models this
// removes the model itself.
func (s *Service) DeleteOverride(providerID, modelName string) error {
	return s.overrides.Delete(overrideKey(providerID, modelName))
}

// ListOverrides returns all overrides of a provider.
func (s *Service) ListOverrides(providerID string) ([]*models.ModelOverride, error) {
	return s.overrides.ListFiltered(func(override *models.ModelOverride) bool {
		return override.ProviderID == providerID
	})
}

// IsModelEnabled reports whether a model may be routed. Models are enabled
// unless an override disables them. Missing overrides allow routing;
// storage failures deny routing.
func (s *Service) IsModelEnabled(providerID, modelName string) (bool, error) {
	override, err := s.overrides.Get(overrideKey(providerID, modelName))
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return true, nil
		}
		return false, err
	}
	if override == nil {
		return true, nil
	}
	return !override.Disabled, nil
}

// MergedView merges the cached upstream model list with admin overrides:
// disabled models are flagged, custom models appended, display metadata applied.
// Fetches from the upstream on a cache miss — explicit refresh paths only.
func (s *Service) MergedView(ctx context.Context, providerID string) ([]ModelView, error) {
	modelList, err := s.GetModelInfos(ctx, providerID)
	if err != nil {
		return nil, err
	}
	overrides, err := s.ListOverrides(providerID)
	if err != nil {
		return nil, err
	}
	return mergeModelViews(modelList, overrides), nil
}

// PeekMergedView is MergedView over the cache (stale entries count) with no
// upstream fetch. Browsing paths use it: upstream sync is opt-in
// (models_auto_sync) or an explicit user action.
func (s *Service) PeekMergedView(providerID string) ([]ModelView, error) {
	overrides, err := s.ListOverrides(providerID)
	if err != nil {
		return nil, err
	}
	return mergeModelViews(s.PeekModelInfos(providerID), overrides), nil
}

func mergeModelViews(modelList []models.ModelInfo, overrides []*models.ModelOverride) []ModelView {
	byName := make(map[string]*models.ModelOverride, len(overrides))
	for _, override := range overrides {
		byName[override.Name] = override
	}
	views := make([]ModelView, 0, len(modelList)+len(overrides))
	for _, model := range modelList {
		view := ModelView{ModelInfo: model}
		if override, ok := byName[model.Name]; ok {
			view.Disabled = override.Disabled
			view.Custom = override.Custom
			if override.DisplayName != "" {
				view.DisplayName = override.DisplayName
			}
			if len(override.Capabilities) > 0 {
				view.Capabilities = override.Capabilities
			}
			if len(override.InputModalities) > 0 {
				view.InputModalities = override.InputModalities
			}
			if len(override.OutputModalities) > 0 {
				view.OutputModalities = override.OutputModalities
			}
			delete(byName, model.Name)
		}
		views = append(views, view)
	}
	for _, override := range byName {
		if !override.Custom {
			continue
		}
		model := models.ModelInfo{
			Name:         override.Name,
			DisplayName:  override.DisplayName,
			Capabilities: override.Capabilities,
		}
		model.DeriveCapabilities()
		views = append(views, ModelView{
			ModelInfo: model,
			Disabled:  override.Disabled,
			Custom:    true,
		})
	}
	return views
}

func (s *Service) store(providerID string, fresh []models.ModelInfo) []models.ModelInfo {
	modelInfos := stripModalityCapabilities(s.mergeRetained(providerID, fresh))
	for i := range modelInfos {
		modelInfos[i].DeriveCapabilities()
	}
	now := util.Now()
	entry := cacheEntry{
		models:    modelInfos,
		cachedAt:  now,
		expiresAt: now.Add(s.cacheTTL),
	}

	s.mu.Lock()
	s.cache[providerID] = entry
	s.mu.Unlock()

	if err := s.records.Put(providerID, &modelInfoRecord{Models: modelInfos, CachedAt: now}); err != nil && s.logger != nil {
		s.logger.Warn("model cache persist failed", "provider_id", providerID, "err", err)
	}

	return modelInfos
}
