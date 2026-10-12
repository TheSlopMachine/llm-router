package luaplugin

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/version"
)

// PluginAPIVersion is the plugin API contract served by this router.
// Format x.y: x breaks, y extends without breaking.
var PluginAPIVersion = version.Version{Major: 1, Minor: 2}

// APICompatError reports a plugin API mismatch with a machine-readable
// Reason for the dashboard toast: "too_new", "too_old", "no_api_version".
type APICompatError struct {
	Reason    string
	RouterAPI version.Version
	PluginAPI version.Version
}

func (e *APICompatError) Error() string {
	switch e.Reason {
	case "no_api_version":
		return "missing required manifest tag \"@plugin_api\""
	case "too_new":
		return fmt.Sprintf("plugin API %s is newer than router API %s: update the router", e.PluginAPI.String(), e.RouterAPI.String())
	default:
		return fmt.Sprintf("plugin API %s is older than router API %s: reissue the plugin", e.PluginAPI.String(), e.RouterAPI.String())
	}
}

// Manifest is the parsed "--- @tag value" header of a plugin file.
type Manifest struct {
	Plugin      string
	Author      string
	Version     string
	PluginAPI   string
	Description string
	License     string
	AllowHosts  []string
	// Unsafe is true when the plugin requests a wildcard allow_host.
	Unsafe bool
}

// ParseManifest parses the leading "---" header block. The header is the
// contiguous run of "---"-prefixed lines at the very start of the file.
func ParseManifest(source []byte) (*Manifest, error) {
	manifest := &Manifest{}
	seen := map[string]int{}
	allowHosts := []string{}
	// Unknown tags defer past the version floor below: an outdated plugin
	// reports the reissue error, never a tag error for a contract it
	// predates.
	unknownTag := ""

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
			manifest.Plugin = value
		case "@author":
			manifest.Author = value
		case "@version":
			manifest.Version = value
		case "@plugin_api":
			manifest.PluginAPI = value
		case "@description":
			manifest.Description = value
		case "@license":
			manifest.License = value
		case "@allow_host":
			if value != "" {
				allowHosts = append(allowHosts, value)
			}
		default:
			if unknownTag == "" {
				unknownTag = tag
			}
		}
	}
	if headerLen == 0 {
		return nil, fmt.Errorf("missing manifest header: file must start with \"--- @\" lines")
	}
	// Unknown tags fail here; @plugin_api presence is enforced below.
	if unknownTag != "" {
		return nil, fmt.Errorf("unknown manifest tag %q", unknownTag)
	}
	for _, tag := range []string{"@plugin", "@author", "@version", "@plugin_api"} {
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
	manifest.AllowHosts = allowHosts
	manifest.Unsafe = wildcard
	if _, _, _, err := parseSemver(manifest.Version); err != nil {
		return nil, fmt.Errorf("invalid @version %q: %w", manifest.Version, err)
	}
	if _, err := version.Parse(manifest.PluginAPI); err != nil {
		return nil, fmt.Errorf("invalid @plugin_api %q: %w", manifest.PluginAPI, err)
	}
	return manifest, nil
}

func dedupeStrings(input []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range input {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// CheckPluginAPI compares the plugin @plugin_api major against the served
// contract. Minor is informational only. Missing tag reports no_api_version.
func CheckPluginAPI(manifest *Manifest) error {
	if manifest == nil || manifest.PluginAPI == "" {
		return &APICompatError{Reason: "no_api_version", RouterAPI: PluginAPIVersion}
	}
	p, err := version.Parse(manifest.PluginAPI)
	if err != nil {
		return fmt.Errorf("invalid @plugin_api %q: %w", manifest.PluginAPI, err)
	}
	switch {
	case p.Major > PluginAPIVersion.Major:
		return &APICompatError{Reason: "too_new", RouterAPI: PluginAPIVersion, PluginAPI: p}
	case p.Major < PluginAPIVersion.Major:
		return &APICompatError{Reason: "too_old", RouterAPI: PluginAPIVersion, PluginAPI: p}
	default:
		return nil
	}
}

func parseSemver(text string) (maj, min, patch int, err error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "v")
	parts := strings.Split(text, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, 0, 0, fmt.Errorf("expected MAJOR.MINOR[.PATCH]")
	}
	for len(parts) < 3 {
		parts = append(parts, "0")
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
