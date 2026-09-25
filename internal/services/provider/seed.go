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

// EnsureSeeded creates the built-in agents provider row and one provider
// row per enabled Lua plugin type key when none exists yet, keeping fresh
// installs routable without manual provider setup. Missing icons on existing
// rows are backfilled from the plugin; user-set icons are never overwritten.
// Seeded rows are marked UI-readonly (set automatically by the core, never
// from Lua or the dashboard); the virtual-models row is additionally UI-hidden.
//
// Legacy custom rows migrate first: seeding must see the post-migration
// state, and migration failures surface here instead of hiding in the
// constructor.
func (s *Service) EnsureSeeded() error {
	if err := s.migrateLegacyCustom(); err != nil {
		return err
	}
	now := time.Now()
	ensure := func(id, name, typeKey, icon string) error {
		if _, err := s.providers.Get(id); err == nil {
			return nil
		}
		hidden := typeKey == TypeVirtual
		config := map[string]any{}
		if s.luaSvc != nil && typeKey != TypeVirtual {
			if mode := s.luaSvc.DefaultProxyMode(typeKey); mode != models.ProxyModeDisabled {
				config["proxy"] = map[string]any{"mode": mode}
			}
		}
		inst := &models.ProviderInstance{
			ID: id, Name: name, TypeKey: typeKey,
			Config: config, IconURL: icon,
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
	if s.luaSvc == nil {
		return nil
	}
	records, err := s.luaSvc.List()
	if err != nil {
		return err
	}
	seen := map[string]string{}
	for _, rec := range records {
		for _, key := range rec.TypeKeys {
			if _, ok := seen[key]; !ok {
				seen[key] = rec.DisplayName
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
		if s.luaSvc != nil {
			icon = s.luaSvc.Icon(key)
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
			if err := s.providers.Update(inst.ID, func(p *models.ProviderInstance) error {
				p.IconURL = icon
				p.UpdatedAt = time.Now()
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
		if err := s.providers.Update(inst.ID, func(p *models.ProviderInstance) error {
			p.IsUIReadonly = wantReadonly
			p.IsUIHidden = wantHidden
			p.UpdatedAt = time.Now()
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
	for _, p := range providers {
		known[p.ID] = true
	}
	n := 0
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketCredentials)
		if b == nil {
			return nil
		}
		var toDelete [][]byte
		_ = b.ForEach(func(k, v []byte) error {
			var c models.Credential
			if err := json.Unmarshal(v, &c); err != nil {
				return nil
			}
			if pid := strings.TrimSpace(c.ProviderID); pid != "" && !known[pid] {
				toDelete = append(toDelete, append([]byte(nil), k...))
			}
			return nil
		})
		for _, k := range toDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
			n = len(toDelete)
		}
		return nil
	})
	if err == nil && n > 0 && s.logger != nil {
		s.logger.Info("cleaned orphaned credentials", "count", n)
	}
	return n, err
}
