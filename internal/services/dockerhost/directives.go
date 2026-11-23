// Package dockerhost runs plugin containers on the operator-configured
// Docker host. It owns the connection settings, container naming and ownership labels, the per-plugin egress gateway, image pulls
// and builds, container reconciliation, container port dials and cleanup.
// The Lua surface lives in luaplugin; this package holds every rule.
package dockerhost

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// API is the @require_api name of the Docker API.
const API = "docker"

// keyPattern matches container keys, compose project keys, service names,
// build names and volume names.
var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// imageReferencePattern is a permissive image reference check: the daemon
// validates the rest.
var imageReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@+-]{0,254}$`)

// ValidKey reports whether key is a valid container, project, service,
// build or volume name.
func ValidKey(key string) bool { return keyPattern.MatchString(key) }

// Directive bounds and defaults.
const (
	minimumMemoryLimitMB   = 16
	maximumMemoryLimitMB   = 65536
	minimumCPULimit        = 0.1
	maximumCPULimit        = 64
	defaultPidsLimit       = 256
	minimumPidsLimit       = 16
	maximumPidsLimit       = 8192
	defaultContainerLimit  = 4
	maximumContainerLimit  = 32
	maximumGPULimit        = 16
	defaultShmLimitMB      = 64
	maximumShmLimitMB      = 65536
	defaultDockerfile      = "Dockerfile"
	maximumDirectiveImages = 32
)

// BuildSource is one @docker.build context: an https URL of a tar or
// tar.gz archive. The router downloads it; a single top-level directory is
// stripped, as in source archives of code hosts.
type BuildSource struct {
	URL        string `json:"url"`
	Dockerfile string `json:"dockerfile"`
}

// Directives are the @docker.* manifest lines of one plugin.
type Directives struct {
	Images         []string               `json:"images"`
	Builds         map[string]BuildSource `json:"builds"`
	MemoryLimitMB  int                    `json:"memory_limit_mb"`
	CPULimit       float64                `json:"cpu_limit"`
	PidsLimit      int                    `json:"pids_limit"`
	ContainerLimit int                    `json:"container_limit"`
	GPULimit       int                    `json:"gpu_limit"`
	ShmLimitMB     int                    `json:"shm_limit_mb"`

	seen map[string]bool
}

// Apply parses one directive. name is the tag without the "@docker."
// prefix.
func (directives *Directives) Apply(name, value string) error {
	if directives.seen == nil {
		directives.seen = map[string]bool{}
	}
	value = strings.TrimSpace(value)
	repeatable := name == "image" || name == "build"
	if !repeatable && directives.seen[name] {
		return fmt.Errorf("duplicate manifest tag %q", "@docker."+name)
	}
	directives.seen[name] = true
	switch name {
	case "image":
		if !imageReferencePattern.MatchString(value) {
			return fmt.Errorf("invalid @docker.image %q", value)
		}
		for _, existing := range directives.Images {
			if existing == value {
				return fmt.Errorf("duplicate @docker.image %q", value)
			}
		}
		if len(directives.Images) >= maximumDirectiveImages {
			return fmt.Errorf("at most %d @docker.image tags", maximumDirectiveImages)
		}
		directives.Images = append(directives.Images, value)
	case "build":
		return directives.applyBuild(value)
	case "memory_limit_mb":
		return parseIntDirective(name, value, minimumMemoryLimitMB, maximumMemoryLimitMB, &directives.MemoryLimitMB)
	case "cpu_limit":
		number, err := strconv.ParseFloat(value, 64)
		if err != nil || number < minimumCPULimit || number > maximumCPULimit {
			return fmt.Errorf("@docker.cpu_limit %q must be a number from %.1f to %d", value, minimumCPULimit, maximumCPULimit)
		}
		directives.CPULimit = number
	case "pids_limit":
		return parseIntDirective(name, value, minimumPidsLimit, maximumPidsLimit, &directives.PidsLimit)
	case "container_limit":
		return parseIntDirective(name, value, 1, maximumContainerLimit, &directives.ContainerLimit)
	case "gpu_limit":
		return parseIntDirective(name, value, 0, maximumGPULimit, &directives.GPULimit)
	case "shm_limit_mb":
		return parseIntDirective(name, value, defaultShmLimitMB, maximumShmLimitMB, &directives.ShmLimitMB)
	default:
		return fmt.Errorf("unknown manifest tag %q", "@docker."+name)
	}
	return nil
}

func (directives *Directives) applyBuild(value string) error {
	fields := strings.Fields(value)
	if len(fields) < 2 || len(fields) > 3 {
		return fmt.Errorf("@docker.build takes NAME URL [DOCKERFILE], got %q", value)
	}
	name, rawURL := fields[0], fields[1]
	if !ValidKey(name) {
		return fmt.Errorf("@docker.build name %q must match %s", name, keyPattern.String())
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("@docker.build URL %q must be an https URL", rawURL)
	}
	dockerfile := defaultDockerfile
	if len(fields) == 3 {
		dockerfile = fields[2]
		if strings.HasPrefix(dockerfile, "/") || strings.Contains(dockerfile, "..") {
			return fmt.Errorf("@docker.build Dockerfile %q must be a relative path inside the context", dockerfile)
		}
	}
	if directives.Builds == nil {
		directives.Builds = map[string]BuildSource{}
	}
	if _, exists := directives.Builds[name]; exists {
		return fmt.Errorf("duplicate @docker.build name %q", name)
	}
	directives.Builds[name] = BuildSource{URL: rawURL, Dockerfile: dockerfile}
	return nil
}

func parseIntDirective(name, value string, minimum, maximum int, target *int) error {
	number, err := strconv.Atoi(value)
	if err != nil || number < minimum || number > maximum {
		return fmt.Errorf("@docker.%s %q must be a whole number from %d to %d", name, value, minimum, maximum)
	}
	*target = number
	return nil
}

// Declared reports whether any @docker.* directive was seen.
func (directives *Directives) Declared() bool { return len(directives.seen) > 0 }

// Finish checks required directives and applies defaults. It runs once
// after the whole manifest is parsed.
func (directives *Directives) Finish() error {
	if len(directives.Images) == 0 && len(directives.Builds) == 0 {
		return fmt.Errorf("@require_api docker needs at least one @docker.image or @docker.build tag")
	}
	if directives.MemoryLimitMB == 0 {
		return fmt.Errorf("missing required manifest tag %q", "@docker.memory_limit_mb")
	}
	if directives.CPULimit == 0 {
		return fmt.Errorf("missing required manifest tag %q", "@docker.cpu_limit")
	}
	if directives.PidsLimit == 0 {
		directives.PidsLimit = defaultPidsLimit
	}
	if directives.ContainerLimit == 0 {
		directives.ContainerLimit = defaultContainerLimit
	}
	if directives.ShmLimitMB == 0 {
		directives.ShmLimitMB = defaultShmLimitMB
	}
	if directives.Builds == nil {
		directives.Builds = map[string]BuildSource{}
	}
	if directives.Images == nil {
		directives.Images = []string{}
	}
	sort.Strings(directives.Images)
	return nil
}

// Policy is everything the router enforces for one plugin: its Docker
// directives and its @allow_host list.
type Policy struct {
	PluginID   string     `json:"plugin_id"`
	Directives Directives `json:"directives"`
	AllowHosts []string   `json:"allow_hosts"`
}

// ImageDeclared reports whether reference is listed in @docker.image.
func (policy Policy) ImageDeclared(reference string) bool {
	for _, image := range policy.Directives.Images {
		if image == reference {
			return true
		}
	}
	return false
}
