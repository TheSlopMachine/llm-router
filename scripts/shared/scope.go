package shared

import (
	"path/filepath"
	"strings"
)

// ResolveScope maps a root-relative PKG pattern to the owning module:
// patterns under scripts/ run inside the scripts module with the path
// rebased, everything else runs in the root module. Empty and ./...
// patterns are unscoped. A pattern matching nothing fails loudly in the
// go tool itself (typo protection).
func ResolveScope(root, packagePattern string) (directory, pattern string, scoped bool) {
	clean := strings.TrimSpace(strings.ReplaceAll(packagePattern, "\\", "/"))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "" || clean == "..." {
		return "", "", false
	}
	if clean == "scripts" || strings.HasPrefix(clean, "scripts/") {
		remainder := strings.TrimPrefix(strings.TrimPrefix(clean, "scripts"), "/")
		if remainder == "" {
			remainder = "..."
		}
		return filepath.Join(root, "scripts"), "./" + remainder, true
	}
	return root, "./" + clean, true
}
