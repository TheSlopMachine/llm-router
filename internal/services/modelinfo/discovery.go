package modelinfo

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	stdErrors "errors"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// cacheEntry is one in-memory model list with its fetch times.
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

// ModelView is a model merged with its admin override state.
type ModelView struct {
	models.ModelInfo
	Disabled bool `json:"disabled"`
	Custom   bool `json:"custom"`
}

// wireDiscovery attaches the discovery dependencies of the GORM service:
// the provider registry for backend resolution, the credential pool for
// Go discovery fetches, and the memory cache TTL. Called once from
// NewGorm; construction performs no I/O.
func (s *GormService) wireDiscovery(providerService *provider.GormService, credentialService *credential.GormService, cacheTTL time.Duration) {
	if cacheTTL == 0 {
		cacheTTL = 1 * time.Hour
	}
	s.providerService = providerService
	s.credentialService = credentialService
	s.cacheTTL = cacheTTL
	s.cache = make(map[string]cacheEntry)
	s.inflight = make(map[string]*inflightCall)
}

// SetLogger wires structured logging (called once from server.New).
func (s *GormService) SetLogger(l *slog.Logger) { s.logger = l }

// PeekModelInfos returns the cached model list without triggering an upstream
// fetch; stale entries count, and a memory miss hydrates from the persisted
// snapshot. A true miss returns nil — callers that merely display a count must
// not ping the provider.
func (s *GormService) PeekModelInfos(ctx context.Context, providerID string) []models.ModelInfo {
	s.mu.RLock()
	entry, ok := s.cache[providerID]
	s.mu.RUnlock()
	if ok {
		return stripModalityCapabilities(entry.models)
	}
	modelList, cachedAt, err := s.GetSnapshot(ctx, providerID)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	s.cache[providerID] = cacheEntry{
		models:    modelList,
		cachedAt:  cachedAt,
		expiresAt: cachedAt.Add(s.cacheTTL),
	}
	s.mu.Unlock()
	return stripModalityCapabilities(modelList)
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
func (s *GormService) WarmMissing(ctx context.Context) {
	providers, err := s.providerService.List(ctx)
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
		if len(s.PeekModelInfos(ctx, provider.ID)) > 0 {
			continue
		}
		if _, err := s.GetModelInfos(ctx, provider.ID); err != nil && s.logger != nil {
			s.logger.Warn("model cache warm failed", "provider_id", provider.ID, "err", err)
		}
	}
}

// GetModelInfos retrieves all model metadata for a provider.
func (s *GormService) GetModelInfos(ctx context.Context, providerID string) ([]models.ModelInfo, error) {
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

// GetModelInfo retrieves metadata for a specific model.
func (s *GormService) GetModelInfo(ctx context.Context, modelID models.ModelId) (*models.ModelInfo, error) {
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

// GetModels retrieves just the model names for a provider.
func (s *GormService) GetModels(ctx context.Context, providerID string) ([]string, error) {
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
func (s *GormService) fetchAndCache(ctx context.Context, providerID string) ([]models.ModelInfo, error) {
	resolved, err := provider.Resolve(s.providerService, ctx, providerID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("model discovery: provider lookup failed", "provider_id", providerID, "err", err)
		}
		return nil, fmt.Errorf("provider lookup failed: %w", err)
	}
	instance := resolved.Instance
	config := instance.Config

	credentials, err := s.credentialService.ListByProvider(ctx, providerID)
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
		return s.store(ctx, providerID, modelInfos), nil
	}
	var modelInfos []models.ModelInfo
	var lastErr error

	fetch := func(credential *models.Credential) ([]models.ModelInfo, error) {
		return safeGetModelInfos(ctx, resolved.Go, credential, config)
	}

	// Credential-free fetch first for backends that do not need credentials
	// for discovery, such as keyless plugins.
	if len(credentials) == 0 {
		modelInfos, lastErr = fetch(nil)
		if lastErr == nil {
			if s.logger != nil {
				s.logger.Debug("model discovery succeeded (no credential)", "provider_id", providerID, "models", len(modelInfos))
			}
			return s.store(ctx, providerID, modelInfos), nil
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
			return s.store(ctx, providerID, modelInfos), nil
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
func (s *GormService) Refresh(ctx context.Context, providerID string) ([]models.ModelInfo, error) {
	return s.fetchAndCache(ctx, providerID)
}

// mergeRetained refreshes known models from the fresh fetch and appends
// names the fetch introduces. Names the fresh fetch omits are retained:
// eviction of models the upstream drops is request-driven, not
// fetch-driven. Manual and custom models live in the overrides table and
// never pass through here.
func (s *GormService) mergeRetained(ctx context.Context, providerID string, fresh []models.ModelInfo) []models.ModelInfo {
	existing := s.PeekModelInfos(ctx, providerID)
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

// MergedView merges the cached upstream model list with admin overrides:
// disabled models are flagged, custom models appended, display metadata applied.
// Fetches from the upstream on a cache miss — explicit refresh paths only.
func (s *GormService) MergedView(ctx context.Context, providerID string) ([]ModelView, error) {
	modelList, err := s.GetModelInfos(ctx, providerID)
	if err != nil {
		return nil, err
	}
	overrides, err := s.ListOverrides(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return mergeModelViews(modelList, overrides), nil
}

// PeekMergedView is MergedView over the cache (stale entries count) with no
// upstream fetch. Browsing paths use it: upstream sync is opt-in
// (models_auto_sync) or an explicit user action.
func (s *GormService) PeekMergedView(ctx context.Context, providerID string) ([]ModelView, error) {
	overrides, err := s.ListOverrides(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return mergeModelViews(s.PeekModelInfos(ctx, providerID), overrides), nil
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

func (s *GormService) store(ctx context.Context, providerID string, fresh []models.ModelInfo) []models.ModelInfo {
	modelInfos := stripModalityCapabilities(s.mergeRetained(ctx, providerID, fresh))
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

	if err := s.PutSnapshot(ctx, providerID, modelInfos, now); err != nil && s.logger != nil {
		s.logger.Warn("model cache persist failed", "provider_id", providerID, "err", err)
	}

	return modelInfos
}

// InvalidateProvider clears the memory cache and the persisted snapshot of
// one provider. A missing snapshot succeeds: invalidation is idempotent.
func (s *GormService) InvalidateProvider(ctx context.Context, providerID string) error {
	s.mu.Lock()
	delete(s.cache, providerID)
	s.mu.Unlock()
	if err := s.DeleteSnapshot(ctx, providerID); err != nil {
		if stdErrors.Is(err, apierrors.ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
}

// InvalidateAll clears the memory cache and every persisted snapshot.
func (s *GormService) InvalidateAll(ctx context.Context) error {
	s.mu.Lock()
	s.cache = make(map[string]cacheEntry)
	s.mu.Unlock()
	providers, err := s.providerService.List(ctx)
	if err != nil {
		return err
	}
	for _, provider := range providers {
		if err := s.InvalidateProvider(ctx, provider.ID); err != nil {
			return err
		}
	}
	return nil
}
