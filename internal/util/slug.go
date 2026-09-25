package util

import (
	"regexp"
	"strings"
)

var (
	slugStripReg = regexp.MustCompile(`[^a-z0-9-]+`)
	slugDashReg  = regexp.MustCompile(`-+`)
)

// Slugify converts a name to a URL-safe slug, or "" when unusable.
// Single source of truth for slugs across providers, virtual models and
// plugin repositories; callers apply their own empty fallback.
func Slugify(name string) string {
	slug := strings.ToLower(name)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	slug = slugStripReg.ReplaceAllString(slug, "")
	slug = strings.Trim(slug, "-")
	slug = slugDashReg.ReplaceAllString(slug, "-")
	return slug
}
