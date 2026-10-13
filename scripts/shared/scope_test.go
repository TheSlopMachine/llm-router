package shared

import (
	"path/filepath"
	"testing"
)

func TestResolveScope(t *testing.T) {
	root := filepath.Join("C:", "devel", "llm-router")
	cases := []struct {
		pattern   string
		directory string
		rebased   string
		scoped    bool
	}{
		{"", "", "", false},
		{"./...", "", "", false},
		{"./scripts/smoke/...", filepath.Join(root, "scripts"), "./smoke/...", true},
		{`.\scripts\smoke\...`, filepath.Join(root, "scripts"), "./smoke/...", true},
		{"scripts", filepath.Join(root, "scripts"), "./...", true},
		{"./internal/services/router/", root, "./internal/services/router/", true},
	}
	for _, testCase := range cases {
		directory, pattern, scoped := ResolveScope(root, testCase.pattern)
		if directory != testCase.directory || pattern != testCase.rebased || scoped != testCase.scoped {
			t.Errorf("ResolveScope(%q): got (%q, %q, %v), want (%q, %q, %v)",
				testCase.pattern, directory, pattern, scoped,
				testCase.directory, testCase.rebased, testCase.scoped)
		}
	}
}
