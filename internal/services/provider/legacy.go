package provider

import (
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// SyncDefaultProviders seeds the built-in provider rows.
func (s *Service) SyncDefaultProviders() error { return s.EnsureSeeded() }

// slugify converts a name to a URL-safe slug.
func slugify(name string) string {
	if slug := util.Slugify(name); slug != "" {
		return slug
	}
	return "provider"
}
