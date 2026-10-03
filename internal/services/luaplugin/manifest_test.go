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

func TestParseManifestFloorFirst(t *testing.T) {
	// Old router version plus every other defect class: the floor error
	// wins, never the field errors.
	old := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @router_version 0.5.3
--- @allow_host bad/host
--- @bogus_tag x
`
	_, err := ParseManifest([]byte(old))
	if err == nil {
		t.Fatal("old router version must fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "predates the oldest served contract") {
		t.Fatalf("floor must fire first, got: %v", err)
	}
	for _, leaked := range []string{"bad/host", "bogus_tag", "@allow_host", "unknown manifest tag"} {
		if strings.Contains(msg, leaked) {
			t.Fatalf("floor must not leak field errors, got: %v", err)
		}
	}

	// Old version with missing required tags: floor still fires first.
	missing := `--- @router_version 0.3.0
--- @allow_host example.com
`
	if _, err := ParseManifest([]byte(missing)); err == nil ||
		!strings.Contains(err.Error(), "predates the oldest served contract") {
		t.Fatalf("floor must beat missing-tag errors, got: %v", err)
	}
}

func TestParseManifestVersionDefectsUnchanged(t *testing.T) {
	// Malformed version: precise invalid-version error, not the floor.
	malformed := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @router_version old
--- @allow_host example.com
`
	if _, err := ParseManifest([]byte(malformed)); err == nil ||
		!strings.Contains(err.Error(), "invalid @router_version") {
		t.Fatalf("malformed version must report invalid, got: %v", err)
	}

	// Duplicated version tag: precise duplicate error, not the floor.
	dupe := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @router_version 0.5.3
--- @router_version 0.7.0
--- @allow_host example.com
`
	if _, err := ParseManifest([]byte(dupe)); err == nil ||
		!strings.Contains(err.Error(), "duplicate manifest tag") {
		t.Fatalf("duplicated tag must report duplicate, got: %v", err)
	}

	// Current version with a field defect: the field error, never the floor.
	current := `--- @plugin P
--- @author a
--- @version 1.0.0
--- @router_version 0.7.0
--- @allow_host bad/host
`
	if _, err := ParseManifest([]byte(current)); err == nil ||
		strings.Contains(err.Error(), "predates") {
		t.Fatalf("current version must report the field defect, got: %v", err)
	}
}
