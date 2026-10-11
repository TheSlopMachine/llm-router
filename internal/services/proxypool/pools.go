package proxypool

import (
	"fmt"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// ListCustomPools returns every manually managed proxy pool by name order.
func (s *Service) ListCustomPools() ([]*models.CustomProxyPool, error) {
	all, err := s.pools.List()
	if err != nil {
		return nil, err
	}
	result := make([]*models.CustomProxyPool, 0, len(all))
	for _, customPool := range all {
		if customPool != nil {
			result = append(result, customPool)
		}
	}
	return result, nil
}

// SaveCustomPool creates or replaces a custom pool. IDs are slugified from
// the name when empty; entries with empty URLs are dropped.
func (s *Service) SaveCustomPool(pool *models.CustomProxyPool) (*models.CustomProxyPool, error) {
	if pool == nil {
		return nil, fmt.Errorf("custom proxy pool is required")
	}
	name := strings.TrimSpace(pool.Name)
	if name == "" {
		return nil, fmt.Errorf("custom proxy pool name is required")
	}
	id := strings.TrimSpace(pool.ID)
	if id == "" {
		id = slugify(name)
	}
	entries := make([]models.ProxyEntry, 0, len(pool.Entries))
	for _, entry := range pool.Entries {
		entryURL := strings.TrimSpace(entry.URL)
		if entryURL == "" {
			continue
		}
		entries = append(entries, models.ProxyEntry{URL: entryURL, Country: strings.ToUpper(strings.TrimSpace(entry.Country))})
	}
	now := time.Now().UTC()
	saved := &models.CustomProxyPool{ID: id, Name: name, Entries: entries, CreatedAt: pool.CreatedAt, UpdatedAt: now}
	if saved.CreatedAt.IsZero() {
		if prev, err := s.pools.Get(id); err == nil && prev != nil {
			saved.CreatedAt = prev.CreatedAt
		} else {
			saved.CreatedAt = now
		}
	}
	if err := s.pools.Put(id, saved); err != nil {
		return nil, err
	}
	return saved, nil
}

// DeleteCustomPool removes a custom pool by ID.
func (s *Service) DeleteCustomPool(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("custom proxy pool id is required")
	}
	return s.pools.Delete(id)
}

func (s *Service) findCustomPool(poolReference string) (*models.CustomProxyPool, error) {
	if got, err := s.pools.Get(poolReference); err == nil && got != nil {
		return got, nil
	}
	all, err := s.pools.List()
	if err != nil {
		return nil, err
	}
	for _, customPool := range all {
		if customPool != nil && strings.EqualFold(customPool.Name, poolReference) {
			return customPool, nil
		}
	}
	return nil, fmt.Errorf("proxy pool %q not found", poolReference)
}

func slugify(name string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			builder.WriteRune('-')
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		slug = "pool"
	}
	return slug
}
