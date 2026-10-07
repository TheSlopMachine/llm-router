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

func TestParseManifestRequiresPluginAPI(t *testing.T) {
	// Missing @plugin_api: precise missing-tag error.
	missing := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @allow_host example.com
`
	if _, err := ParseManifest([]byte(missing)); err == nil ||
		!strings.Contains(err.Error(), `missing required manifest tag "@plugin_api"`) {
		t.Fatalf("missing API must report the tag, got: %v", err)
	}

	// Malformed @plugin_api: precise invalid error.
	malformed := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @plugin_api old
--- @allow_host example.com
`
	if _, err := ParseManifest([]byte(malformed)); err == nil ||
		!strings.Contains(err.Error(), "invalid @plugin_api") {
		t.Fatalf("malformed API must report invalid, got: %v", err)
	}

	// Duplicated @plugin_api: precise duplicate error.
	dupe := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @plugin_api 1.0
--- @plugin_api 1.0
--- @allow_host example.com
`
	if _, err := ParseManifest([]byte(dupe)); err == nil ||
		!strings.Contains(err.Error(), "duplicate manifest tag") {
		t.Fatalf("duplicated tag must report duplicate, got: %v", err)
	}

	// Current API with a field defect: the field error, never the API error.
	current := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host bad/host
`
	if _, err := ParseManifest([]byte(current)); err == nil ||
		strings.Contains(err.Error(), "@plugin_api") {
		t.Fatalf("current API must report the field defect, got: %v", err)
	}
}
