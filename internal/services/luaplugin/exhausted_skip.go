package luaplugin

import (
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/pool"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
)

// exhaustedSkip returns a pool.SkipFunc that reports true when the exhausted
// store holds a live rate-limit key for the credential-model pair. The key
// covers plugin, provider instance, account, and model dimensions. A nil
// exhausted store disables the check (skip always returns false).
// providerID is the calling provider instance ID; typeKey resolves the
// plugin record. Lookup failures fail open (no skip), so a struggling store
// never blocks traffic.
func (s *Service) exhaustedSkip(providerID, typeKey, model string) pool.SkipFunc {
	if s.exhausted == nil {
		return nil
	}
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return nil
	}
	pluginID := rec.ID
	return func(cred *models.Credential) bool {
		if cred == nil {
			return false
		}
		hit, err := s.exhausted.LimitedAny(exhausted.Segments{
			Plugin:   pluginID,
			Provider: providerID,
			Account:  cred.ID,
			Model:    model,
		})
		if err != nil {
			return false
		}
		return hit != ""
	}
}
