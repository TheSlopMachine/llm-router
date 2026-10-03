package luaplugin

import (
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/pool"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
)

// exhaustedSkip returns a pool.LimitFunc reporting the live rate-limit reset
// time for the credential-model pair. The key covers plugin, provider
// instance, account, and model dimensions. A nil exhausted store disables
// the check. Lookup failures fail open (not limited), so a struggling store
// never blocks traffic.
func (s *Service) exhaustedSkip(providerID, typeKey, model string) pool.LimitFunc {
	if s.exhausted == nil {
		return nil
	}
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return nil
	}
	pluginID := rec.ID
	return func(cred *models.Credential) (time.Time, bool) {
		if cred == nil {
			return time.Time{}, false
		}
		resetsAt, limited, err := s.exhausted.MatchExpiry(exhausted.Segments{
			Plugin:   pluginID,
			Provider: providerID,
			Account:  cred.ID,
			Model:    model,
		})
		if err != nil {
			return time.Time{}, false
		}
		return resetsAt, limited
	}
}
