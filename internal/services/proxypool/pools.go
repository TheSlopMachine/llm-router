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
	out := make([]*models.CustomProxyPool, 0, len(all))
	for _, p := range all {
		if p != nil {
			out = append(out, p)
		}
	}
	return out, nil
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
	for _, e := range pool.Entries {
		u := strings.TrimSpace(e.URL)
		if u == "" {
			continue
		}
		entries = append(entries, models.ProxyEntry{URL: u, Country: strings.ToUpper(strings.TrimSpace(e.Country))})
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

func (s *Service) findCustomPool(ref string) (*models.CustomProxyPool, error) {
	if got, err := s.pools.Get(ref); err == nil && got != nil {
		return got, nil
	}
	all, err := s.pools.List()
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		if p != nil && strings.EqualFold(p.Name, ref) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("proxy pool %q not found", ref)
}

func slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "pool"
	}
	return slug
}
