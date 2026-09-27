package luaplugin

import (
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/pool"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
)

// exhaustedSkip returns a pool.SkipFunc that reports true when the exhausted
// store holds a live rate-limit key for the credential-model pair. The key
// covers plugin, provider type, account, and model dimensions. A nil
// exhausted store disables the check (skip always returns false).
func (s *Service) exhaustedSkip(pluginID, typeKey, model string) pool.SkipFunc {
	if s.exhausted == nil {
		return nil
	}
	return func(cred *models.Credential) bool {
		if cred == nil {
			return false
		}
		hit, err := s.exhausted.LimitedAny(exhausted.Segments{
			Plugin:   pluginID,
			Provider: typeKey,
			Account:  cred.ID,
			Model:    model,
		})
		if err != nil {
			return false
		}
		return hit != ""
	}
}
