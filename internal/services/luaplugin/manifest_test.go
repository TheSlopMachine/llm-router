package luaplugin

import (
	"strings"
	"testing"
)

func TestParseSemverAcceptsMinorOnly(t *testing.T) {
	cases := []struct {
		in              string
		maj, min, patch int
	}{
		{"1.1", 1, 1, 0},
		{"v2.3", 2, 3, 0},
		{"1.1.0", 1, 1, 0},
		{"0.0.7", 0, 0, 7},
		{" 1.2 ", 1, 2, 0},
	}
	for _, tc := range cases {
		maj, min, patch, err := parseSemver(tc.in)
		if err != nil {
			t.Errorf("parseSemver(%q): %v", tc.in, err)
			continue
		}
		if maj != tc.maj || min != tc.min || patch != tc.patch {
			t.Errorf("parseSemver(%q) = %d.%d.%d, want %d.%d.%d",
				tc.in, maj, min, patch, tc.maj, tc.min, tc.patch)
		}
	}
}

func TestParseSemverRejects(t *testing.T) {
	for _, in := range []string{"", "1", "a.b.c", "1.2.3.4", "1.-2", "1.x"} {
		if _, _, _, err := parseSemver(in); err == nil {
			t.Errorf("parseSemver(%q) must error", in)
		} else if !strings.Contains(err.Error(), "MAJOR.MINOR") && !strings.Contains(err.Error(), "numeric") {
			t.Errorf("parseSemver(%q) error must name the format: %v", in, err)
		}
	}
}

func TestCompareVersionsMinorOnly(t *testing.T) {
	cmp, err := CompareVersions("1.1", "1.1.0")
	if err != nil || cmp != 0 {
		t.Errorf("1.1 vs 1.1.0: got %d %v", cmp, err)
	}
	cmp, err = CompareVersions("1.1", "1.2")
	if err != nil || cmp >= 0 {
		t.Errorf("1.1 vs 1.2: got %d %v", cmp, err)
	}
}
