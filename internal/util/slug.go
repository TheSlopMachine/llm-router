package util

import (
	"regexp"
	"strings"
)

var (
	slugStripRegexp = regexp.MustCompile(`[^a-z0-9-]+`)
	slugDashRegexp  = regexp.MustCompile(`-+`)
)

// Slugify converts a name to a URL-safe slug, or "" when unusable.
// Single source of truth for slugs across providers, virtual models and
// plugin repositories; callers apply their own empty fallback.
func Slugify(name string) string {
	slug := strings.ToLower(name)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	slug = slugStripRegexp.ReplaceAllString(slug, "")
	slug = strings.Trim(slug, "-")
	slug = slugDashRegexp.ReplaceAllString(slug, "-")
	return slug
}
