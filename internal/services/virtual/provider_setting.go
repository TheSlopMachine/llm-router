package virtual

import (
	"context"
	"fmt"
	"strings"

	stdErrors "errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// ProviderVirtualSettings is the switch for a provider's managed virtual models.
type ProviderVirtualSettings struct {
	Enabled bool `json:"enabled"`
}

// providerSetting reads the switch row of a provider; nil means switched on.
func providerSetting(ctx context.Context, handle *gorm.DB, providerID string) (*record.ProviderVirtualSetting, error) {
	row, err := gorm.G[record.ProviderVirtualSetting](handle, clause.Eq{Column: "provider_id", Value: providerID}).First(ctx)
	if err != nil {
		translated := db.Translate(err)
		if stdErrors.Is(translated, apierrors.ErrNotFound) {
			return nil, nil
		}
		return nil, translated
	}
	return &row, nil
}

// ProviderSettings reports whether the provider's managed virtual models are on.
func (s *GormService) ProviderSettings(ctx context.Context, providerID string) (ProviderVirtualSettings, error) {
	if _, err := s.requireHandle(); err != nil {
		return ProviderVirtualSettings{}, err
	}
	trimmed := strings.TrimSpace(providerID)
	if _, err := s.providerService.Get(ctx, trimmed); err != nil {
		return ProviderVirtualSettings{}, err
	}
	setting, err := providerSetting(ctx, s.database.Handle.WithContext(ctx), trimmed)
	if err != nil {
		s.log().Error("Provider virtual settings lookup fails", "provider_id", trimmed, "error", err)
		return ProviderVirtualSettings{}, err
	}
	return ProviderVirtualSettings{Enabled: setting == nil || !setting.Disabled}, nil
}

// SetProviderVirtualModelsEnabled switches a provider's managed virtual
// models. Off records which endpoints had their model enabled and deletes the
// managed models, so they are neither listed nor routable and sync creates
// none. On rebuilds them from the cache with those endpoints enabled again.
// Returns the provider's endpoint groups after the switch.
func (s *GormService) SetProviderVirtualModelsEnabled(ctx context.Context, providerID string, enabled bool) ([]ProviderVMGroup, error) {
	if _, err := s.requireHandle(); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(providerID)
	if _, err := s.providerService.Get(ctx, trimmed); err != nil {
		return nil, err
	}
	if enabled {
		if err := s.switchProviderOn(ctx, trimmed); err != nil {
			return nil, err
		}
	} else if err := s.switchProviderOff(ctx, trimmed); err != nil {
		return nil, err
	}
	s.log().Info("Provider virtual models switched", "provider_id", trimmed, "enabled", enabled)
	return s.GroupsForProvider(ctx, trimmed)
}

// switchProviderOff stores the enabled endpoints and deletes the managed
// models in one transaction. A provider already off keeps its stored state.
func (s *GormService) switchProviderOff(ctx context.Context, providerID string) error {
	err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		existing, err := providerSetting(ctx, transaction, providerID)
		if err != nil {
			return err
		}
		if existing != nil && existing.Disabled {
			return nil
		}
		managed, err := gorm.G[record.VirtualModel](transaction, clause.Eq{Column: "managed_provider_id", Value: providerID}).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		enabledEndpoints := []string{}
		for _, row := range managed {
			if !row.Disabled && row.ManagedEndpointSlug != nil {
				enabledEndpoints = append(enabledEndpoints, *row.ManagedEndpointSlug)
			}
		}
		setting := record.ProviderVirtualSetting{ProviderID: providerID, Disabled: true, EnabledEndpoints: enabledEndpoints, UpdatedAt: util.Now()}
		if err := gorm.G[record.ProviderVirtualSetting](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "provider_id"}}, UpdateAll: true}).Create(ctx, &setting); err != nil {
			return db.Translate(err)
		}
		if _, err := gorm.G[record.VirtualModel](transaction, clause.Eq{Column: "managed_provider_id", Value: providerID}).Delete(ctx); err != nil {
			return db.Translate(err)
		}
		return nil
	})
	if err != nil {
		s.log().Error("Provider virtual models switch fails", "provider_id", providerID, "operation", "switch_off", "error", err)
		return fmt.Errorf("switch off virtual models of provider %q: %w", providerID, err)
	}
	return nil
}

// switchProviderOn clears the off flag, rebuilds the managed models with the
// stored endpoints enabled, then drops the stored state. A failed rebuild
// keeps the state, so switching on again still restores it.
func (s *GormService) switchProviderOn(ctx context.Context, providerID string) error {
	handle := s.database.Handle.WithContext(ctx)
	existing, err := providerSetting(ctx, handle, providerID)
	if err != nil {
		s.log().Error("Provider virtual models switch fails", "provider_id", providerID, "operation", "read_setting", "error", err)
		return err
	}
	if existing == nil {
		return nil
	}
	if existing.Disabled {
		if _, err := gorm.G[record.ProviderVirtualSetting](handle, clause.Eq{Column: "provider_id", Value: providerID}).Update(ctx, "disabled", false); err != nil {
			s.log().Error("Provider virtual models switch fails", "provider_id", providerID, "operation", "clear_flag", "error", err)
			return db.Translate(err)
		}
	}
	if _, err := s.SyncProviderModels(ctx, providerID); err != nil {
		return fmt.Errorf("switch on virtual models of provider %q: %w", providerID, err)
	}
	if _, err := gorm.G[record.ProviderVirtualSetting](handle, clause.Eq{Column: "provider_id", Value: providerID}).Delete(ctx); err != nil {
		s.log().Error("Provider virtual models switch fails", "provider_id", providerID, "operation", "drop_setting", "error", err)
		return db.Translate(err)
	}
	return nil
}
