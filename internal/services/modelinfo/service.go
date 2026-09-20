// Package modelinfo manages model metadata caching.
//
// This service provides a caching layer between the dashboard UI and provider backends,
// reducing API calls and improving performance.
package modelinfo

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// Service manages model metadata caching
type Service struct {
	providerSvc *provider.Service
	credSvc     *credential.Service
	cacheTTL    time.Duration
	logger      *slog.Logger
	overrides   *repository.Repository[models.ModelOverride]
	records     *repository.Repository[modelInfoRecord]

	mu       sync.RWMutex
	cache    map[string]cacheEntry
	inflight map[string]*sync.WaitGroup
}

type cacheEntry struct {
	models    []models.ModelInfo
	cachedAt  time.Time
	expiresAt time.Time
}

// modelInfoRecord is the persisted form of a cache entry: the model cache
// lives in bbolt and survives restarts.
type modelInfoRecord struct {
	Models   []models.ModelInfo `json:"models"`
	CachedAt time.Time          `json:"cached_at"`
}

// New creates a ModelInfo service
func New(database *db.DB, providerSvc *provider.Service, credSvc *credential.Service, cacheTTL time.Duration) *Service {
	if cacheTTL == 0 {
		cacheTTL = 1 * time.Hour
	}

	return &Service{
		providerSvc: providerSvc,
		credSvc:     credSvc,
		cacheTTL:    cacheTTL,
		overrides:   repository.New[models.ModelOverride](database, db.BucketModelOverrides, "model_override"),
		records:     repository.New[modelInfoRecord](database, db.BucketModelInfos, "model_info_record"),
		cache:       make(map[string]cacheEntry),
		inflight:    make(map[string]*sync.WaitGroup),
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
		return entry.models
	}
	rec, err := s.records.Get(providerID)
	if err != nil || rec == nil {
		return nil
	}
	s.mu.Lock()
	s.cache[providerID] = cacheEntry{
		models:    rec.Models,
		cachedAt:  rec.CachedAt,
		expiresAt: rec.CachedAt.Add(s.cacheTTL),
	}
	s.mu.Unlock()
	return rec.Models
}

// WarmMissing creates model caches for providers that have none. Runs in the
// background at startup; refresh afterwards stays opt-in (models_auto_sync)
// or manual.
func (s *Service) WarmMissing(ctx context.Context) {
	providers, err := s.providerSvc.List()
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("model cache warm: list providers failed", "err", err)
		}
		return
	}
	for _, p := range providers {
		if ctx.Err() != nil {
			return
		}
		if p.Disabled {
			continue
		}
		if len(s.PeekModelInfos(p.ID)) > 0 {
			continue
		}
		if _, err := s.GetModelInfos(ctx, p.ID); err != nil && s.logger != nil {
			s.logger.Warn("model cache warm failed", "provider_id", p.ID, "err", err)
		}
	}
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<invalid-host>"
	}
	return u.Host
}

// GetModelInfos retrieves all model metadata for a provider
func (s *Service) GetModelInfos(ctx context.Context, providerID string) ([]models.ModelInfo, error) {
	s.mu.RLock()
	if entry, exists := s.cache[providerID]; exists && util.Now().Before(entry.expiresAt) {
		s.mu.RUnlock()
		return entry.models, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	wg, exists := s.inflight[providerID]
	if exists {
		s.mu.Unlock()
		wg.Wait()

		s.mu.RLock()
		defer s.mu.RUnlock()
		if entry, exists := s.cache[providerID]; exists {
			return entry.models, nil
		}
		return nil, fmt.Errorf("fetch failed for provider %s", providerID)
	}

	wg = &sync.WaitGroup{}
	wg.Add(1)
	s.inflight[providerID] = wg
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.inflight, providerID)
		s.mu.Unlock()
		wg.Done()
	}()

	return s.fetchAndCache(ctx, providerID)
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
	resolved, err := provider.Resolve(s.providerSvc, providerID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("model discovery: provider lookup failed", "provider_id", providerID, "err", err)
		}
		return nil, fmt.Errorf("provider lookup failed: %w", err)
	}
	p := resolved.Instance
	config := p.Config

	creds, err := s.credSvc.ListByProvider(providerID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("model discovery: list credentials failed", "provider_id", providerID, "err", err)
		}
		return nil, fmt.Errorf("list credentials for provider %s: %w", p.Name, err)
	}
	var modelInfos []models.ModelInfo
	var lastErr error

	fetch := func(cred *models.Credential) ([]models.ModelInfo, error) {
		if resolved.IsLua() {
			return s.providerSvc.LuaService().GetModelInfos(ctx, p.TypeKey, cred, config)
		}
		return safeGetModelInfos(ctx, resolved.Go, cred, config)
	}

	// Credential-free fetch first for backends that do not need credentials
	// for discovery, such as the built-in agents provider or keyless plugins.
	if len(creds) == 0 {
		modelInfos, lastErr = fetch(nil)
		if lastErr == nil {
			if s.logger != nil {
				s.logger.Info("model discovery succeeded (no credential)", "provider_id", providerID, "models", len(modelInfos))
			}
			return s.store(providerID, modelInfos), nil
		}
		if s.logger != nil {
			s.logger.Warn("model discovery failed", "provider_id", providerID, "qualifier", p.Qualifier, "err", lastErr, "attempts", 1)
		}
		return nil, fmt.Errorf("no credentials available for provider %s: %w", p.Name, lastErr)
	}

	for _, cred := range creds {
		modelInfos, lastErr = fetch(cred)
		if lastErr == nil {
			if s.logger != nil {
				s.logger.Info("model discovery succeeded", "provider_id", providerID, "credential_id", cred.ID, "models", len(modelInfos))
			}
			return s.store(providerID, modelInfos), nil
		}
	}

	if s.logger != nil {
		s.logger.Warn("model discovery failed", "provider_id", providerID, "qualifier", p.Qualifier, "err", lastErr, "attempts", len(creds))
	}
	return nil, fmt.Errorf("provider %q discovery: %w", providerID, lastErr)
}

func safeGetModelInfos(
	ctx context.Context,
	adapter provider.GoAdapter,
	cred *models.Credential,
	providerConfig map[string]any,
) (modelInfos []models.ModelInfo, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("adapter GetModelInfos panicked: %v", r)
		}
	}()

	return adapter.GetModelInfos(ctx, cred, providerConfig)
}

// Refresh forces an upstream fetch and stores it. The previous cache is
// kept when the fetch fails: until a new list arrives the old one counts
// as current.
func (s *Service) Refresh(ctx context.Context, providerID string) ([]models.ModelInfo, error) {
	return s.fetchAndCache(ctx, providerID)
}

// mergeRetained keeps existing cached records by name and appends names
// the fresh fetch introduces. Records are keyed by full model id
// (provider + name): the same name on another provider is a separate
// record verified separately. Known models keep their stored metadata:
// providers add models, they don't rewrite them. Manual and custom models
// live in the overrides bucket and never pass through here.
func (s *Service) mergeRetained(providerID string, fresh []models.ModelInfo) []models.ModelInfo {
	existing := s.PeekModelInfos(providerID)
	if len(existing) == 0 {
		return fresh
	}
	seen := make(map[string]bool, len(existing))
	merged := make([]models.ModelInfo, 0, len(existing)+len(fresh))
	merged = append(merged, existing...)
	for _, mi := range existing {
		seen[mi.Name] = true
	}
	for _, mi := range fresh {
		if !seen[mi.Name] {
			merged = append(merged, mi)
			seen[mi.Name] = true
		}
	}
	return merged
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
	providers, err := s.providerSvc.List()
	if err != nil {
		return err
	}
	for _, p := range providers {
		_ = s.records.DeleteIfExists(p.ID)
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
func (s *Service) SetOverride(ov models.ModelOverride) error {
	if ov.ProviderID == "" || ov.Name == "" {
		return fmt.Errorf("override requires provider id and model name")
	}
	return s.overrides.Put(overrideKey(ov.ProviderID, ov.Name), &ov)
}

// DeleteOverride removes the override for one model. For custom models this
// removes the model itself.
func (s *Service) DeleteOverride(providerID, modelName string) error {
	return s.overrides.Delete(overrideKey(providerID, modelName))
}

// ListOverrides returns all overrides of a provider.
func (s *Service) ListOverrides(providerID string) ([]*models.ModelOverride, error) {
	return s.overrides.ListFiltered(func(ov *models.ModelOverride) bool {
		return ov.ProviderID == providerID
	})
}

// IsModelEnabled reports whether a model may be routed. Models are enabled
// unless an override disables them.
func (s *Service) IsModelEnabled(providerID, modelName string) bool {
	ov, err := s.overrides.Get(overrideKey(providerID, modelName))
	if err != nil || ov == nil {
		return true
	}
	return !ov.Disabled
}

// MergedView merges the cached upstream model list with admin overrides:
// disabled models are flagged, custom models appended, display metadata applied.
// Fetches from the upstream on a cache miss — explicit refresh paths only.
func (s *Service) MergedView(ctx context.Context, providerID string) ([]ModelView, error) {
	infos, err := s.GetModelInfos(ctx, providerID)
	if err != nil {
		return nil, err
	}
	ovs, err := s.ListOverrides(providerID)
	if err != nil {
		return nil, err
	}
	return mergeModelViews(infos, ovs), nil
}

// PeekMergedView is MergedView over the cache (stale entries count) with no
// upstream fetch. Browsing paths use it: upstream sync is opt-in
// (models_auto_sync) or an explicit user action.
func (s *Service) PeekMergedView(providerID string) ([]ModelView, error) {
	ovs, err := s.ListOverrides(providerID)
	if err != nil {
		return nil, err
	}
	return mergeModelViews(s.PeekModelInfos(providerID), ovs), nil
}

func mergeModelViews(infos []models.ModelInfo, ovs []*models.ModelOverride) []ModelView {
	byName := make(map[string]*models.ModelOverride, len(ovs))
	for _, ov := range ovs {
		byName[ov.Name] = ov
	}
	out := make([]ModelView, 0, len(infos)+len(ovs))
	for _, mi := range infos {
		v := ModelView{ModelInfo: mi}
		if ov, ok := byName[mi.Name]; ok {
			v.Disabled = ov.Disabled
			v.Custom = ov.Custom
			if ov.DisplayName != "" {
				v.DisplayName = ov.DisplayName
			}
			if len(ov.Capabilities) > 0 {
				v.Capabilities = ov.Capabilities
			}
			delete(byName, mi.Name)
		}
		out = append(out, v)
	}
	for _, ov := range byName {
		if !ov.Custom {
			continue
		}
		mi := models.ModelInfo{
			Name:         ov.Name,
			DisplayName:  ov.DisplayName,
			Capabilities: ov.Capabilities,
		}
		mi.DeriveCapabilities()
		out = append(out, ModelView{
			ModelInfo: mi,
			Disabled:  ov.Disabled,
			Custom:    true,
		})
	}
	return out
}

func (s *Service) store(providerID string, fresh []models.ModelInfo) []models.ModelInfo {
	modelInfos := s.mergeRetained(providerID, fresh)
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
