package provider

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	bolt "go.etcd.io/bbolt"
)

// EnsureSeeded creates the virtual-models row and one provider row per
// enabled Lua plugin type key when none exists yet, keeping fresh installs
// routable without manual provider setup. Missing icons on existing rows
// are backfilled from the plugin; user-set icons are never overwritten.
// Seeded rows are marked UI-readonly (set automatically by the core, never
// from Lua or the dashboard); the virtual-models row is additionally UI-hidden.
func (s *Service) EnsureSeeded() error {
	now := time.Now()
	ensure := func(id, name, typeKey, icon string) error {
		if _, err := s.providers.Get(id); err == nil {
			return nil
		}
		hidden := typeKey == TypeVirtual
		inst := &models.ProviderInstance{
			ID: id, Name: name, TypeKey: typeKey,
			Config: map[string]any{}, IconURL: icon,
			IsUIReadonly: true, IsUIHidden: hidden,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := s.providers.Put(id, inst); err != nil {
			return err
		}
		s.notifyChanged(id)
		return nil
	}
	// The legacy "agents" singleton row is dead weight; virtual models live
	// under the "virtual" row now.
	if err := s.providers.DeleteIfExists("agents"); err != nil {
		return fmt.Errorf("drop legacy agents provider row: %w", err)
	}
	// Backfill flags on rows that match the seed pattern (pre-flag installs).
	if err := s.backfillSeedFlags(); err != nil {
		return err
	}
	if err := ensure(TypeVirtual, "Virtual models", TypeVirtual, ""); err != nil {
		return err
	}
	if s.luaService == nil {
		return nil
	}
	records, err := s.luaService.List()
	if err != nil {
		return err
	}
	seen := map[string]string{}
	for _, record := range records {
		for _, key := range record.TypeKeys {
			if _, ok := seen[key]; !ok {
				seen[key] = record.DisplayName
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var icon string
		if s.luaService != nil {
			icon = s.luaService.Icon(key)
		}
		existing, err := s.GetByType(key)
		if err != nil {
			return err
		}
		if len(existing) == 0 {
			name := seen[key]
			if strings.TrimSpace(name) == "" {
				name = key
			}
			if err := ensure(key, name, key, icon); err != nil {
				return err
			}
			continue
		}
		if icon == "" {
			continue
		}
		for _, inst := range existing {
			if inst.IconURL != "" {
				continue
			}
			if err := s.providers.Update(inst.ID, func(provider *models.ProviderInstance) error {
				provider.IconURL = icon
				provider.UpdatedAt = time.Now()
				return nil
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// backfillSeedFlags marks pre-flag rows matching the seed pattern as
// UI-readonly (the bare `agents` row additionally UI-hidden). Rows with
// qualifiers stay editable: only the singleton slot is core-managed.
func (s *Service) backfillSeedFlags() error {
	rows, err := s.providers.List()
	if err != nil {
		return err
	}
	for _, inst := range rows {
		wantReadonly := inst.ID == inst.TypeKey && inst.TypeKey != TypeCustom
		wantHidden := inst.TypeKey == TypeVirtual && inst.ID == TypeVirtual
		if !wantReadonly && !wantHidden {
			continue
		}
		if inst.IsUIReadonly == wantReadonly && inst.IsUIHidden == wantHidden {
			continue
		}
		if err := s.providers.Update(inst.ID, func(provider *models.ProviderInstance) error {
			provider.IsUIReadonly = wantReadonly
			provider.IsUIHidden = wantHidden
			provider.UpdatedAt = time.Now()
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// CleanupOrphanedCredentials deletes credentials whose provider no longer exists.
func (s *Service) CleanupOrphanedCredentials() (int, error) {
	providers, err := s.providers.List()
	if err != nil {
		return 0, err
	}
	known := map[string]bool{}
	for _, existing := range providers {
		known[existing.ID] = true
	}
	count := 0
	err = s.database.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(db.BucketCredentials)
		if bucket == nil {
			return nil
		}
		var toDelete [][]byte
		_ = bucket.ForEach(func(key, value []byte) error {
			var credential models.Credential
			if err := json.Unmarshal(value, &credential); err != nil {
				return nil
			}
			if providerID := strings.TrimSpace(credential.ProviderID); providerID != "" && !known[providerID] {
				toDelete = append(toDelete, append([]byte(nil), key...))
			}
			return nil
		})
		for _, key := range toDelete {
			if err := bucket.Delete(key); err != nil {
				return err
			}
			count = len(toDelete)
		}
		return nil
	})
	if err == nil && count > 0 && s.logger != nil {
		s.logger.Info("cleaned orphaned credentials", "count", count)
	}
	return count, err
}
