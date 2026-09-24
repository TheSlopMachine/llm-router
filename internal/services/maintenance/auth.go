package maintenance

import (
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/repository"
)

// cleanupAuthFlows removes auth flow entries older than 10 minutes.
func (s *Service) cleanupAuthFlows() {
	threshold := time.Now().UTC().Add(-10 * time.Minute)

	type authEntry struct {
		CreatedAt time.Time `json:"created_at"`
	}

	if err := repository.CleanupExpired(s.db, db.BucketAuth, threshold, func(e *authEntry) time.Time {
		return e.CreatedAt
	}); err != nil {
		s.logger.Error("maintenance: cleanup auth flows failed", "err", err)
	}
}
