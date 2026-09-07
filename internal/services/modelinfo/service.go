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
func New(_ *db.DB, providerSvc *provider.Service, credSvc *credential.Service, cacheTTL time.Duration) *Service {
	if cacheTTL == 0 {
		cacheTTL = 1 * time.Hour
	}

	return &Service{
		providerSvc: providerSvc,
		credSvc:     credSvc,
		cacheTTL:    cacheTTL,
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

func (s *Service) store(providerID string, modelInfos []models.ModelInfo) []models.ModelInfo {
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
