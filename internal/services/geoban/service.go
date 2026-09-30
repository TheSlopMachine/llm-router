// Package geoban owns indefinite geo-block flags: (plugin, provider type,
// proxy) triples whose exit the upstream refuses for that provider.
// Provider is the adapter type key here, deliberately shared by every
// instance of the type: the block is a property of the upstream region
// policy, unlike exhausted quota marks which isolate per instance.
// No expiry: a flag lives until an admin clears it.
// Expired-state self-healing does not apply; a wrongly banned proxy is
// recovered by explicit clear, never by timer.
package geoban

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
)

// Key builds the canonical flag key: fixed dimension order.
func Key(plugin, provider, proxy string) string {
	return strings.Join([]string{"p=" + plugin, "pr=" + provider, "x=" + proxy}, "\x00")
}

// Service persists GeoBanEntry rows keyed by the canonical key.
type Service struct {
	repo   *repository.Repository[models.GeoBanEntry]
	logger *slog.Logger
}

// New constructs the geoban Service.
func New(database *db.DB) *Service {
	return &Service{repo: repository.New[models.GeoBanEntry](database, db.BucketGeoBans, "geoban")}
}

// SetLogger wires structured logging.
func (s *Service) SetLogger(l *slog.Logger) { s.logger = l }

// Mark records the (plugin, provider, proxy) triple as banned, overwriting
// any entry. First mark wins on reason: re-marking an existing ban keeps
// the original reason and timestamp so late duplicate geo errors cannot
// rewrite the admin-visible cause.
func (s *Service) Mark(plugin, provider, proxy, reason string) error {
	if plugin == "" || provider == "" || proxy == "" {
		return errors.New("geoban: plugin, provider and proxy are required")
	}
	key := Key(plugin, provider, proxy)
	existing, err := s.repo.Get(key)
	if err == nil && existing != nil {
		if s.logger != nil {
			s.logger.Debug("geoban: mark skipped, already banned", "plugin_id", plugin, "type", provider, "proxy_id", proxy)
		}
		return nil
	}
	if err != nil && !errors.Is(err, apierrors.ErrNotFound) {
		return err
	}
	if err := s.repo.Put(key, &models.GeoBanEntry{
		Key: key, Plugin: plugin, Provider: provider, Proxy: proxy,
		Reason: reason, BannedAt: time.Now(),
	}); err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Debug("geoban: marked proxy", "plugin_id", plugin, "type", provider, "proxy_id", proxy)
	}
	return nil
}

// IsBanned reports whether the triple is flagged.
func (s *Service) IsBanned(plugin, provider, proxy string) (bool, error) {
	if plugin == "" || provider == "" || proxy == "" {
		return false, nil
	}
	_, err := s.repo.Get(Key(plugin, provider, proxy))
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Clear removes one flag. Missing flags still succeed.
func (s *Service) Clear(plugin, provider, proxy string) error {
	if err := s.repo.DeleteIfExists(Key(plugin, provider, proxy)); err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Debug("geoban: cleared proxy", "plugin_id", plugin, "type", provider, "proxy_id", proxy)
	}
	return nil
}

// ClearProvider removes every flag of one (plugin, provider) pair and
// returns the removed count.
func (s *Service) ClearProvider(plugin, provider string) (int, error) {
	all, err := s.repo.List()
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, e := range all {
		if e.Plugin == plugin && e.Provider == provider {
			if err := s.repo.DeleteIfExists(e.Key); err != nil {
				return removed, err
			}
			removed++
		}
	}
	if s.logger != nil {
		s.logger.Debug("geoban: cleared provider bans", "plugin_id", plugin, "type", provider, "count", removed)
	}
	return removed, nil
}

// ListProvider returns every flag of one (plugin, provider) pair.
func (s *Service) ListProvider(plugin, provider string) ([]*models.GeoBanEntry, error) {
	all, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	var out []*models.GeoBanEntry
	for _, e := range all {
		if e.Plugin == plugin && e.Provider == provider {
			out = append(out, e)
		}
	}
	return out, nil
}

// BannedRegions returns the distinct regions of banned proxies for one
// (plugin, provider) pair, resolved through regionOf. Unknown regions
// (empty string) are skipped: they ban the proxy, never the region.
func (s *Service) BannedRegions(plugin, provider string, regionOf func(proxyID string) string) (map[string]bool, error) {
	bans, err := s.ListProvider(plugin, provider)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, b := range bans {
		if r := regionOf(b.Proxy); r != "" {
			out[r] = true
		}
	}
	return out, nil
}
