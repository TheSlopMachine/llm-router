package provider

import (
	"context"
	"sort"
	"strings"

	"gorm.io/gorm"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// EnsureSeeded creates the virtual-models row and one provider row per
// enabled Lua plugin type key when none exists yet, keeping fresh installs
// routable without manual provider setup. Missing icons on existing rows
// are backfilled from the plugin; user-set icons are never overwritten.
// Seeded rows are marked UI-readonly; the virtual-models row is
// additionally UI-hidden.
func (s *GormService) EnsureSeeded(ctx context.Context) error {
	ensure := func(identifier, name, typeKey, icon string) error {
		if _, err := s.Get(ctx, identifier); err == nil {
			return nil
		}
		now := util.Now()
		candidate := &models.ProviderInstance{
			ID:           identifier,
			Name:         name,
			TypeKey:      typeKey,
			Config:       map[string]any{},
			IconURL:      icon,
			IsUIReadonly: true,
			IsUIHidden:   typeKey == TypeVirtual,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := s.createSeedRow(ctx, candidate); err != nil {
			return err
		}
		s.notifyChanged(identifier)
		return nil
	}
	if err := ensure(TypeVirtual, "Virtual models", TypeVirtual, ""); err != nil {
		return err
	}
	if s.luaService == nil {
		return nil
	}
	records, err := s.luaService.ListSQLite(ctx)
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
		existing, err := s.GetByType(ctx, key)
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
			if _, err := s.Update(ctx, inst.ID, UpdateOptions{Name: inst.Name, Config: inst.Config, IconURL: icon}); err != nil {
				return err
			}
		}
	}
	return nil
}

// SyncDefaultProviders seeds the built-in provider rows. Runtime plugin
// changes call it so newly installed types gain their default rows.
func (s *GormService) SyncDefaultProviders(ctx context.Context) error {
	return s.EnsureSeeded(ctx)
}

// createSeedRow inserts a seed row with its UI flags verbatim. The public
// Create path cannot serve seeds: it never stamps the core-managed flags.
func (s *GormService) createSeedRow(ctx context.Context, instance *models.ProviderInstance) error {
	if _, err := s.requireHandle(); err != nil {
		return err
	}
	row := providerRecordFromModel(instance)
	if err := gorm.G[record.ProviderInstance](s.database.Handle).Create(ctx, &row); err != nil {
		return db.Translate(err)
	}
	return nil
}
