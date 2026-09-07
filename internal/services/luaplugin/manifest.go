package luaplugin

import (
	"fmt"
	"strconv"
	"strings"
)

// Manifest is the parsed "--- @tag value" header of a plugin file.
type Manifest struct {
	Plugin        string
	Author        string
	Version       string
	RouterVersion string
	Description   string
	License       string
	AllowHosts    []string
	// Unsafe is true when the plugin requests a wildcard allow_host.
	Unsafe bool
}

// ParseManifest parses the leading "---" header block. The header is the
// contiguous run of "---"-prefixed lines at the very start of the file.
func ParseManifest(source []byte) (*Manifest, error) {
	m := &Manifest{}
	seen := map[string]int{}
	allowHosts := []string{}

	text := strings.ReplaceAll(string(source), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	headerLen := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "---") {
			break
		}
		headerLen++
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "---"))
		if rest == "" {
			continue
		}
		if !strings.HasPrefix(rest, "@") {
			continue
		}
		space := strings.IndexAny(rest, " \t")
		var tag, value string
		if space == -1 {
			tag = rest
		} else {
			tag = rest[:space]
			value = strings.TrimSpace(rest[space+1:])
		}
		seen[tag]++
		switch tag {
		case "@plugin":
			m.Plugin = value
		case "@author":
			m.Author = value
		case "@version":
			m.Version = value
		case "@router_version":
			m.RouterVersion = value
		case "@description":
			m.Description = value
		case "@license":
			m.License = value
		case "@allow_host":
			if value != "" {
				allowHosts = append(allowHosts, value)
			}
		default:
			return nil, fmt.Errorf("unknown manifest tag %q", tag)
		}
	}
	if headerLen == 0 {
		return nil, fmt.Errorf("missing manifest header: file must start with \"--- @\" lines")
	}
	for _, tag := range []string{"@plugin", "@author", "@version", "@router_version"} {
		if seen[tag] == 0 {
			return nil, fmt.Errorf("missing required manifest tag %q", tag)
		}
		if seen[tag] > 1 {
			return nil, fmt.Errorf("duplicate manifest tag %q", tag)
		}
	}
	if seen["@description"] > 1 || seen["@license"] > 1 {
		return nil, fmt.Errorf("duplicate single-value manifest tag")
	}
	if len(allowHosts) == 0 {
		return nil, fmt.Errorf("at least one @allow_host tag is required")
	}
	wildcard := false
	for _, h := range allowHosts {
		if h == "*" {
			wildcard = true
		}
	}
	if wildcard && len(allowHosts) > 1 {
		return nil, fmt.Errorf("@allow_host \"*\" must be the only value when used")
	}
	for _, h := range allowHosts {
		if h != "*" {
			if strings.Contains(h, "/") || strings.Contains(h, " ") || strings.Contains(h, ":") {
				return nil, fmt.Errorf("invalid @allow_host %q: must be a bare hostname or \"*\"", h)
			}
		}
	}
	m.AllowHosts = allowHosts
	m.Unsafe = wildcard
	if _, _, _, err := parseSemver(m.Version); err != nil {
		return nil, fmt.Errorf("invalid @version %q: %w", m.Version, err)
	}
	if _, _, _, err := parseSemver(m.RouterVersion); err != nil {
		return nil, fmt.Errorf("invalid @router_version %q: %w", m.RouterVersion, err)
	}
	return m, nil
}

// CheckRouterVersion rejects plugins requiring a newer router.
func CheckRouterVersion(manifest *Manifest, current string) error {
	cMaj, cMin, cPatch, err := parseSemver(current)
	if err != nil {
		return fmt.Errorf("invalid router version %q: %w", current, err)
	}
	rMaj, rMin, rPatch, err := parseSemver(manifest.RouterVersion)
	if err != nil {
		return fmt.Errorf("invalid plugin @router_version %q: %w", manifest.RouterVersion, err)
	}
	if cMaj != rMaj {
		if cMaj < rMaj {
			return fmt.Errorf("plugin requires router %s, current is %s", manifest.RouterVersion, current)
		}
		return nil
	}
	if cMin < rMin || (cMin == rMin && cPatch < rPatch) {
		return fmt.Errorf("plugin requires router %s, current is %s", manifest.RouterVersion, current)
	}
	return nil
}

func parseSemver(s string) (maj, min, patch int, err error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("expected MAJOR.MINOR.PATCH")
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return 0, 0, 0, fmt.Errorf("invalid numeric component %q", p)
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], nil
}

// CompareVersions compares two MAJOR.MINOR.PATCH versions.
// Returns -1, 0 or 1 when a is older, equal or newer than b.
func CompareVersions(a, b string) (int, error) {
	aMaj, aMin, aPatch, err := parseSemver(a)
	if err != nil {
		return 0, fmt.Errorf("invalid version %q: %w", a, err)
	}
	bMaj, bMin, bPatch, err := parseSemver(b)
	if err != nil {
		return 0, fmt.Errorf("invalid version %q: %w", b, err)
	}
	switch {
	case aMaj != bMaj:
		if aMaj < bMaj {
			return -1, nil
		}
		return 1, nil
	case aMin != bMin:
		if aMin < bMin {
			return -1, nil
		}
		return 1, nil
	case aPatch != bPatch:
		if aPatch < bPatch {
			return -1, nil
		}
		return 1, nil
	default:
		return 0, nil
	}
}
