// Package exhausted owns the unified joint limit-key store (0.1.1).
//
// A stored key is a filter over candidate dimensions: a candidate combination
// matching every stored dimension is deprioritized until ResetsAt passes. Expired
// entries delete on read; Prune sweeps entries nobody reads anymore.
package exhausted

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
)

// Segments is the full identity of one request attempt. Plugin and Provider
// always participate (Provider is the configured instance ID, not the
// adapter type key); Credential, Model and Proxy narrow the key when
// non-empty. The credential dimension serializes with the legacy "account"
// scope word and "a=" key prefix: stored shapes never change.
type Segments struct {
	Plugin     string
	Provider   string
	Credential string
	Model      string
	Proxy      string
}

// BuildKey assembles the canonical key: fixed dimension order, non-empty
// dimensions only. Prefixes keep dimensions unambiguous after the join.
func BuildKey(s Segments) string {
	parts := []string{"p=" + s.Plugin, "pr=" + s.Provider}
	if s.Credential != "" {
		parts = append(parts, "a="+s.Credential)
	}
	if s.Model != "" {
		parts = append(parts, "m="+s.Model)
	}
	if s.Proxy != "" {
		parts = append(parts, "x="+s.Proxy)
	}
	return strings.Join(parts, "\x00")
}

// KeyFromScope builds the stored key for one error outcome: plugin and
// provider always participate, the scope words select the narrowing
// dimensions. Unknown scope words fail closed so typos never silently drop
// the marking.
func KeyFromScope(plugin, provider, credentialID, model, proxy string, scope []string) (string, error) {
	s := Segments{Plugin: plugin, Provider: provider}
	for _, w := range scope {
		switch w {
		case models.ExhaustedScopeAccount:
			s.Credential = credentialID
		case models.ExhaustedScopeModel:
			s.Model = model
		case models.ExhaustedScopeProxy:
			s.Proxy = proxy
		default:
			return "", fmt.Errorf("exhausted: unknown scope word %q", w)
		}
	}
	if s.Credential == "" && s.Model == "" && s.Proxy == "" {
		return "", fmt.Errorf("exhausted: scope selects no dimension")
	}
	return BuildKey(s), nil
}

// FullKey builds the strictest key: every known dimension. Used when a
// rate/quota error carries no scope: only the exact combination is
// deprioritized, so the router tries everything else first.
func FullKey(plugin, provider, credentialID, model, proxy string) string {
	return BuildKey(Segments{Plugin: plugin, Provider: provider, Credential: credentialID, Model: model, Proxy: proxy})
}

// SubKeys returns every stored-key shape matching the full candidate:
// plugin and provider fixed, every non-empty subset of the narrowing
// dimensions present on the candidate, most-specific first.
func SubKeys(full Segments) []string {
	values := []string{full.Credential, full.Model, full.Proxy}
	present := 0
	for i, v := range values {
		if v != "" {
			present |= 1 << i
		}
	}
	var out []string
	for pop := 3; pop >= 1; pop-- {
		for mask := 1; mask < 1<<3; mask++ {
			if mask&^present != 0 {
				continue
			}
			n := 0
			for i := range values {
				if mask&(1<<i) != 0 {
					n++
				}
			}
			if n != pop {
				continue
			}
			sub := Segments{Plugin: full.Plugin, Provider: full.Provider}
			if mask&1 != 0 {
				sub.Credential = full.Credential
			}
			if mask&2 != 0 {
				sub.Model = full.Model
			}
			if mask&4 != 0 {
				sub.Proxy = full.Proxy
			}
			out = append(out, BuildKey(sub))
		}
	}
	return out
}

// Service persists ExhaustedEntry rows keyed by the canonical key.
type Service struct {
	repo *repository.Repository[models.ExhaustedEntry]
}

// New constructs the exhausted Service.
func New(database *db.DB) *Service {
	return &Service{repo: repository.New[models.ExhaustedEntry](database, db.BucketExhausted, "exhausted")}
}

// Mark records the key as limited until resetsAt, overwriting any entry.
func (s *Service) Mark(key string, resetsAt time.Time, reason string) error {
	if key == "" {
		return fmt.Errorf("exhausted: empty key")
	}
	return s.repo.Put(key, &models.ExhaustedEntry{Key: key, ResetsAt: resetsAt, Reason: reason})
}

// Limited reports whether the exact key limits now. Expired entries delete
// on read, so a passed limit never blocks again.
func (s *Service) Limited(key string) (bool, error) {
	e, err := s.repo.Get(key)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	if !time.Now().Before(e.ResetsAt) {
		_ = s.repo.DeleteIfExists(key)
		return false, nil
	}
	return true, nil
}

// LimitedAny reports the first matching stored key for the full candidate,
// most-specific first. Empty when nothing limits the candidate.
func (s *Service) LimitedAny(full Segments) (string, error) {
	for _, key := range SubKeys(full) {
		limited, err := s.Limited(key)
		if err != nil {
			return "", err
		}
		if limited {
			return key, nil
		}
	}
	return "", nil
}

// MatchExpiry reports the reset time of the first matching live limit key
// for the full candidate, most-specific first. False when nothing limits
// the candidate. Expired entries delete on read through Limited.
func (s *Service) MatchExpiry(full Segments) (time.Time, bool, error) {
	for _, key := range SubKeys(full) {
		e, err := s.repo.Get(key)
		if err != nil {
			if errors.Is(err, apierrors.ErrNotFound) {
				continue
			}
			return time.Time{}, false, err
		}
		if !time.Now().Before(e.ResetsAt) {
			_ = s.repo.DeleteIfExists(key)
			continue
		}
		return e.ResetsAt, true, nil
	}
	return time.Time{}, false, nil
}

// Prune deletes expired entries and returns the removed count.
func (s *Service) Prune() (int, error) {
	all, err := s.repo.List()
	if err != nil {
		return 0, err
	}
	removed := 0
	now := time.Now()
	for _, e := range all {
		if !now.Before(e.ResetsAt) {
			if err := s.repo.DeleteIfExists(e.Key); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}
