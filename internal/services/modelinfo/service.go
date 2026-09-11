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

	mu       sync.RWMutex
	cache    map[string]cacheEntry
	inflight map[string]*sync.WaitGroup
}

type cacheEntry struct {
	models    []models.ModelInfo
	cachedAt  time.Time
	expiresAt time.Time
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
		cache:       make(map[string]cacheEntry),
		inflight:    make(map[string]*sync.WaitGroup),
	}
}

// SetLogger wires structured logging (called once from server.New).
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

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

// InvalidateProvider clears cache for a specific provider
func (s *Service) InvalidateProvider(providerID string) error {
	s.mu.Lock()
	delete(s.cache, providerID)
	s.mu.Unlock()
	return nil
}

// InvalidateAll clears entire cache
func (s *Service) InvalidateAll() error {
	s.mu.Lock()
	s.cache = make(map[string]cacheEntry)
	s.mu.Unlock()
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
func (s *Service) MergedView(ctx context.Context, providerID string) ([]ModelView, error) {
	infos, err := s.GetModelInfos(ctx, providerID)
	if err != nil {
		return nil, err
	}
	ovs, err := s.ListOverrides(providerID)
	if err != nil {
		return nil, err
	}
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
	return out, nil
}

func (s *Service) store(providerID string, modelInfos []models.ModelInfo) []models.ModelInfo {
	for i := range modelInfos {
		modelInfos[i].DeriveCapabilities()
	}
	entry := cacheEntry{
		models:    modelInfos,
		cachedAt:  util.Now(),
		expiresAt: util.Now().Add(s.cacheTTL),
	}

	s.mu.Lock()
	s.cache[providerID] = entry
	s.mu.Unlock()

	return modelInfos
}
