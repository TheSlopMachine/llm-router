// Package modelinfo manages model metadata caching.
//
// This service provides a caching layer between the dashboard UI and provider adapters,
// reducing API calls and improving performance.
package modelinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// Service manages model metadata caching
type Service struct {
	db          *db.DB
	providerSvc *provider.Service
	credSvc     *credential.Service
	cacheTTL    time.Duration

	// In-memory cache: providerID -> cacheEntry
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

	s := &Service{
		db:          database,
		providerSvc: providerSvc,
		credSvc:     credSvc,
		cacheTTL:    cacheTTL,
		cache:       make(map[string]cacheEntry),
		inflight:    make(map[string]*sync.WaitGroup),
	}

	// Load cache from database on startup
	s.loadFromDB()

	return s
}

// GetModelInfos retrieves all model metadata for a provider
// Example: GetModelInfos(ctx, "openai:azure")
func (s *Service) GetModelInfos(ctx context.Context, providerID string) ([]models.ModelInfo, error) {
	// Check in-memory cache first
	s.mu.RLock()
	if entry, exists := s.cache[providerID]; exists && util.Now().Before(entry.expiresAt) {
		s.mu.RUnlock()
		return entry.models, nil
	}
	s.mu.RUnlock()

	// Check if fetch is already in progress
	s.mu.Lock()
	wg, exists := s.inflight[providerID]
	if exists {
		s.mu.Unlock()
		wg.Wait()

		// Try cache again after wait
		s.mu.RLock()
		defer s.mu.RUnlock()
		if entry, exists := s.cache[providerID]; exists {
			return entry.models, nil
		}
		return nil, fmt.Errorf("fetch failed for provider %s", providerID)
	}

	// Start new fetch
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
// Example: GetModelInfo(ctx, "openai:azure/gpt-4o")
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
// Example: GetModels(ctx, "openai:azure")
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

// fetchAndCache fetches model metadata from provider and caches it
func (s *Service) fetchAndCache(ctx context.Context, providerID string) ([]models.ModelInfo, error) {
	// Get provider record
	p, err := s.providerSvc.Get(providerID)
	if err != nil {
		return nil, fmt.Errorf("provider lookup failed: %w", err)
	}

	// Get adapter
	adapter, err := provider.Lookup(p.Type)
	if err != nil {
		return nil, fmt.Errorf("adapter lookup failed: %w", err)
	}

	// Get credentials
	creds, err := s.credSvc.ListByProvider(providerID)
	if err != nil || len(creds) == 0 {
		return nil, fmt.Errorf("no credentials available for provider %s", p.Name)
	}

	// Try each credential until one succeeds
	var modelInfos []models.ModelInfo
	var lastErr error

	for _, cred := range creds {
		modelInfos, lastErr = adapter.GetModelInfos(ctx, cred.ToSDK(), p.Qualifier)
		if lastErr == nil {
			break
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all credentials failed: %w", lastErr)
	}

	// Cache in memory
	entry := cacheEntry{
		models:    modelInfos,
		cachedAt:  util.Now(),
		expiresAt: util.Now().Add(s.cacheTTL),
	}

	s.mu.Lock()
	s.cache[providerID] = entry
	s.mu.Unlock()

	// Persist to database
	s.persistCache(providerID, entry)

	return modelInfos, nil
}

// persistCache stores cache entry to database
func (s *Service) persistCache(providerID string, entry cacheEntry) error {
	type dbEntry struct {
		Models    []models.ModelInfo `json:"models"`
		CachedAt  time.Time          `json:"cached_at"`
		ExpiresAt time.Time          `json:"expires_at"`
	}

	data, err := json.Marshal(dbEntry{
		Models:    entry.models,
		CachedAt:  entry.cachedAt,
		ExpiresAt: entry.expiresAt,
	})
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketModelInfo)
		return b.Put([]byte(providerID), data)
	})
}

// loadFromDB loads cache from database on startup
func (s *Service) loadFromDB() error {
	type dbEntry struct {
		Models    []models.ModelInfo `json:"models"`
		CachedAt  time.Time          `json:"cached_at"`
		ExpiresAt time.Time          `json:"expires_at"`
	}

	return s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketModelInfo)
		if b == nil {
			return nil
		}

		return b.ForEach(func(k, v []byte) error {
			var entry dbEntry
			if err := json.Unmarshal(v, &entry); err != nil {
				return nil // Skip corrupted entries
			}

			// Only load non-expired entries
			if util.Now().Before(entry.ExpiresAt) {
				s.mu.Lock()
				s.cache[string(k)] = cacheEntry{
					models:    entry.Models,
					cachedAt:  entry.CachedAt,
					expiresAt: entry.ExpiresAt,
				}
				s.mu.Unlock()
			}

			return nil
		})
	})
}

// InvalidateProvider clears cache for a specific provider
func (s *Service) InvalidateProvider(providerID string) error {
	s.mu.Lock()
	delete(s.cache, providerID)
	s.mu.Unlock()

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketModelInfo)
		if b == nil {
			return nil
		}
		return b.Delete([]byte(providerID))
	})
}

// InvalidateAll clears entire cache
func (s *Service) InvalidateAll() error {
	s.mu.Lock()
	s.cache = make(map[string]cacheEntry)
	s.mu.Unlock()

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketModelInfo)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, _ []byte) error {
			return b.Delete(k)
		})
	})
}

