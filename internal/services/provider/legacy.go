package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
	bolt "go.etcd.io/bbolt"
)

// CreateCustom creates a custom provider. Kept for existing callers.
func (s *Service) CreateCustom(name, baseURL, iconURL string) (*models.ProviderInstance, error) {
	return s.Create(CreateOptions{
		Name: name, TypeKey: TypeCustom,
		Config: map[string]any{"base_url": baseURL}, IconURL: iconURL,
	})
}

// UpdateCustom updates a custom provider by slug or full ID.
func (s *Service) UpdateCustom(id, name, baseURL, iconURL string) (*models.ProviderInstance, error) {
	return s.Update(normalizeCustomID(id), UpdateOptions{
		Name: name, Config: map[string]any{"base_url": baseURL}, IconURL: iconURL,
	})
}

// DeleteCustom deletes a custom provider by slug or full ID.
func (s *Service) DeleteCustom(id string) error {
	return s.Delete(normalizeCustomID(id))
}

// GetCustom retrieves a custom provider by slug or full ID.
func (s *Service) GetCustom(id string) (*models.ProviderInstance, error) {
	inst, err := s.Get(normalizeCustomID(id))
	if err != nil {
		return nil, err
	}
	if inst.TypeKey != TypeCustom {
		return nil, fmt.Errorf("provider %q is not a custom provider", id)
	}
	return inst, nil
}

// SyncDefaultProviders is a no-op kept for backward compatibility.
// Providers are explicit database rows; nothing is synthesized.
func (s *Service) SyncDefaultProviders() error { return s.EnsureSeeded() }

func normalizeCustomID(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "custom:") {
		return id
	}
	return "custom:" + slugify(id)
}

func stripCustomPrefix(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "custom:") {
		s = strings.TrimPrefix(s, "custom:")
	}
	return s
}

// migrateLegacyCustom moves BucketCustomProviders rows into provider_instances.
func (s *Service) migrateLegacyCustom() error {
	type legacyCustomProvider struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		BaseURL   string    `json:"base_url"`
		IconURL   string    `json:"icon_url"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	var legacy []legacyCustomProvider
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketCustomProviders)
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			var cp legacyCustomProvider
			if err := json.Unmarshal(v, &cp); err != nil {
				return nil
			}
			legacy = append(legacy, cp)
			return nil
		})
	})
	if err != nil {
		return err
	}
	for _, cp := range legacy {
		id := "custom:" + slugify(cp.ID)
		if exists, _ := s.providers.Exists(id); exists {
			continue
		}
		now := time.Now()
		created := cp.CreatedAt
		if created.IsZero() {
			created = now
		}
		updated := cp.UpdatedAt
		if updated.IsZero() {
			updated = now
		}
		inst := &models.ProviderInstance{
			ID: id, Name: cp.Name, TypeKey: TypeCustom, Qualifier: slugify(cp.ID),
			Config:  map[string]any{"base_url": strings.TrimSuffix(strings.TrimSpace(cp.BaseURL), "/")},
			IconURL: cp.IconURL, CreatedAt: created, UpdatedAt: updated,
		}
		if strings.TrimSpace(inst.Name) == "" {
			inst.Name = cp.ID
		}
		_ = s.providers.Put(id, inst)
		// Migrate credentials pointing at legacy slug forms to the full ID.
		_ = s.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(db.BucketCredentials)
			if b == nil {
				return nil
			}
			var keys [][]byte
			var vals [][]byte
			_ = b.ForEach(func(k, v []byte) error {
				var c models.Credential
				if err := json.Unmarshal(v, &c); err != nil {
					return nil
				}
				if stripCustomPrefix(c.ProviderID) == stripCustomPrefix(cp.ID) && strings.TrimSpace(c.ProviderID) != id {
					c.ProviderID = id
					enc, err := json.Marshal(&c)
					if err != nil {
						return nil
					}
					keys = append(keys, append([]byte(nil), k...))
					vals = append(vals, enc)
				}
				return nil
			})
			for i, k := range keys {
				_ = b.Put(k, vals[i])
			}
			return nil
		})
	}
	return nil
}

// slugify converts a name to a URL-safe slug.
func slugify(name string) string {
	if slug := util.Slugify(name); slug != "" {
		return slug
	}
	return "provider"
}
