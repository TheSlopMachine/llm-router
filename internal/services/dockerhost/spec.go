package dockerhost

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// Spec bounds.
const (
	maximumFilesBytes    = 1 << 20
	maximumPorts         = 16
	maximumEnvironment   = 256
	maximumArguments     = 256
	maximumHealthSeconds = 3600
	maximumHealthRetries = 100
)

// Restart policies a spec may ask for.
const (
	RestartNo        = "no"
	RestartOnFailure = "on_failure"
)

// Dependency conditions of compose services.
const (
	ConditionStarted = "service_started"
	ConditionHealthy = "service_healthy"
)

// environmentNamePattern matches POSIX environment variable names.
var environmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// HealthSpec is a container health check.
type HealthSpec struct {
	Command            []string `json:"command"`
	IntervalSeconds    float64  `json:"interval_seconds"`
	TimeoutSeconds     float64  `json:"timeout_seconds"`
	StartPeriodSeconds float64  `json:"start_period_seconds"`
	Retries            int      `json:"retries"`
}

// ContainerSpec is one container a plugin asks for. Exactly one of Image
// and Build is set.
type ContainerSpec struct {
	Image            string            `json:"image,omitempty"`
	Build            string            `json:"build,omitempty"`
	Command          []string          `json:"command,omitempty"`
	Entrypoint       []string          `json:"entrypoint,omitempty"`
	Environment      map[string]string `json:"environment,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	User             string            `json:"user,omitempty"`
	Ports            []int             `json:"ports,omitempty"`
	Volumes          map[string]string `json:"volumes,omitempty"`
	Files            map[string]string `json:"files,omitempty"`
	Health           *HealthSpec       `json:"health,omitempty"`
	Restart          string            `json:"restart,omitempty"`
	MemoryLimitMB    int               `json:"memory_limit_mb,omitempty"`
	CPULimit         float64           `json:"cpu_limit,omitempty"`
	GPUs             int               `json:"gpus,omitempty"`
	ShmSizeMB        int               `json:"shm_size_mb,omitempty"`
	ReadOnly         bool              `json:"read_only,omitempty"`
	// DependsOn maps a sibling service to its condition. Compose only.
	DependsOn map[string]string `json:"depends_on,omitempty"`
}

// ProjectSpec is one compose project: named services on a shared network.
type ProjectSpec struct {
	Services map[string]ContainerSpec `json:"services"`
}

// ServiceNames returns the service names in start order: dependencies
// first, ties by name.
func (project ProjectSpec) ServiceNames() []string {
	names := make([]string, 0, len(project.Services))
	for name := range project.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := make([]string, 0, len(names))
	placed := map[string]bool{}
	var place func(name string)
	place = func(name string) {
		if placed[name] {
			return
		}
		placed[name] = true
		dependencies := make([]string, 0, len(project.Services[name].DependsOn))
		for dependency := range project.Services[name].DependsOn {
			dependencies = append(dependencies, dependency)
		}
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			place(dependency)
		}
		ordered = append(ordered, name)
	}
	for _, name := range names {
		place(name)
	}
	return ordered
}

// validateContainer checks one spec against the plugin policy and fills
// defaults: memory and CPU default to the ceilings, restart to "no".
func validateContainer(spec *ContainerSpec, policy Policy, field string) error {
	directives := policy.Directives
	switch {
	case spec.Image != "" && spec.Build != "":
		return specError("%s: set image or build, not both", field)
	case spec.Image == "" && spec.Build == "":
		return specError("%s: image or build is required", field)
	case spec.Image != "" && !policy.ImageDeclared(spec.Image):
		return specError("%s: image %q is not declared with @docker.image", field, spec.Image)
	case spec.Build != "":
		if _, ok := directives.Builds[spec.Build]; !ok {
			return specError("%s: build %q is not declared with @docker.build", field, spec.Build)
		}
	}
	if len(spec.Command) > maximumArguments || len(spec.Entrypoint) > maximumArguments {
		return specError("%s: command and entrypoint take at most %d arguments", field, maximumArguments)
	}
	if len(spec.Environment) > maximumEnvironment {
		return specError("%s: environment takes at most %d entries", field, maximumEnvironment)
	}
	for name := range spec.Environment {
		if !environmentNamePattern.MatchString(name) {
			return specError("%s: environment name %q is invalid", field, name)
		}
		if strings.HasPrefix(strings.ToUpper(name), "LLMR_") || isProxyVariable(name) {
			return specError("%s: environment name %q is reserved", field, name)
		}
	}
	if spec.WorkingDirectory != "" && !path.IsAbs(spec.WorkingDirectory) {
		return specError("%s: working_directory must be an absolute path", field)
	}
	if len(spec.Ports) > maximumPorts {
		return specError("%s: ports takes at most %d entries", field, maximumPorts)
	}
	seenPorts := map[int]bool{}
	for _, port := range spec.Ports {
		if port < 1 || port > 65535 {
			return specError("%s: port %d is out of range", field, port)
		}
		if seenPorts[port] {
			return specError("%s: duplicate port %d", field, port)
		}
		seenPorts[port] = true
	}
	sort.Ints(spec.Ports)
	for name, target := range spec.Volumes {
		if !ValidKey(name) {
			return specError("%s: volume name %q must match %s (host paths are not supported)", field, name, keyPattern.String())
		}
		if !path.IsAbs(target) || path.Clean(target) == "/" {
			return specError("%s: volume %q target must be an absolute path below /", field, name)
		}
	}
	total := 0
	for target, content := range spec.Files {
		if !path.IsAbs(target) || strings.HasSuffix(target, "/") || path.Clean(target) != target {
			return specError("%s: file path %q must be a clean absolute file path", field, target)
		}
		total += len(content)
	}
	if total > maximumFilesBytes {
		return specError("%s: files take at most %d bytes in total", field, maximumFilesBytes)
	}
	if spec.Health != nil {
		if err := validateHealth(spec.Health, field); err != nil {
			return err
		}
	}
	switch spec.Restart {
	case "":
		spec.Restart = RestartNo
	case RestartNo, RestartOnFailure:
	default:
		return specError("%s: restart must be %q or %q", field, RestartNo, RestartOnFailure)
	}
	if spec.MemoryLimitMB == 0 {
		spec.MemoryLimitMB = directives.MemoryLimitMB
	}
	if spec.MemoryLimitMB < minimumMemoryLimitMB || spec.MemoryLimitMB > directives.MemoryLimitMB {
		return specError("%s: memory_limit_mb %d must be from %d to the @docker.memory_limit_mb ceiling %d", field, spec.MemoryLimitMB, minimumMemoryLimitMB, directives.MemoryLimitMB)
	}
	if spec.CPULimit == 0 {
		spec.CPULimit = directives.CPULimit
	}
	if spec.CPULimit < minimumCPULimit || spec.CPULimit > directives.CPULimit {
		return specError("%s: cpu_limit %g must be from %.1f to the @docker.cpu_limit ceiling %g", field, spec.CPULimit, minimumCPULimit, directives.CPULimit)
	}
	if spec.GPUs < 0 || spec.GPUs > directives.GPULimit {
		return specError("%s: gpus %d exceeds the @docker.gpu_limit ceiling %d", field, spec.GPUs, directives.GPULimit)
	}
	if spec.ShmSizeMB == 0 {
		spec.ShmSizeMB = defaultShmLimitMB
	}
	if spec.ShmSizeMB < defaultShmLimitMB || spec.ShmSizeMB > directives.ShmLimitMB {
		return specError("%s: shm_size_mb %d must be from %d to the @docker.shm_limit_mb ceiling %d", field, spec.ShmSizeMB, defaultShmLimitMB, directives.ShmLimitMB)
	}
	return nil
}

func validateHealth(health *HealthSpec, field string) error {
	if len(health.Command) == 0 {
		return specError("%s: health.command is required", field)
	}
	for _, value := range []float64{health.IntervalSeconds, health.TimeoutSeconds, health.StartPeriodSeconds} {
		if value < 0 || value > maximumHealthSeconds {
			return specError("%s: health durations must be from 0 to %d seconds", field, maximumHealthSeconds)
		}
	}
	if health.Retries < 0 || health.Retries > maximumHealthRetries {
		return specError("%s: health.retries must be from 0 to %d", field, maximumHealthRetries)
	}
	return nil
}

func isProxyVariable(name string) bool {
	switch strings.ToUpper(name) {
	case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY":
		return true
	}
	return false
}

// validateProject checks every service, the dependency graph and the
// container limit.
func validateProject(project *ProjectSpec, policy Policy) error {
	if len(project.Services) == 0 {
		return specError("compose: services must not be empty")
	}
	if len(project.Services) > policy.Directives.ContainerLimit {
		return specError("compose: %d services exceed @docker.container_limit %d", len(project.Services), policy.Directives.ContainerLimit)
	}
	for name, service := range project.Services {
		if !ValidKey(name) {
			return specError("compose: service name %q must match %s", name, keyPattern.String())
		}
		field := "services." + name
		if err := validateContainer(&service, policy, field); err != nil {
			return err
		}
		for dependency, condition := range service.DependsOn {
			if _, ok := project.Services[dependency]; !ok {
				return specError("%s.depends_on: unknown service %q", field, dependency)
			}
			if dependency == name {
				return specError("%s.depends_on: a service cannot depend on itself", field)
			}
			switch condition {
			case "":
				service.DependsOn[dependency] = ConditionStarted
			case ConditionStarted, ConditionHealthy:
			default:
				return specError("%s.depends_on.%s: condition must be %q or %q", field, dependency, ConditionStarted, ConditionHealthy)
			}
			if condition == ConditionHealthy && project.Services[dependency].Health == nil {
				return specError("%s.depends_on.%s: service_healthy needs a health check on %q", field, dependency, dependency)
			}
		}
		project.Services[name] = service
	}
	if cycle := dependencyCycle(*project); cycle != "" {
		return specError("compose: depends_on has a cycle through %q", cycle)
	}
	return nil
}

func dependencyCycle(project ProjectSpec) string {
	const (
		unvisited = iota
		visiting
		done
	)
	state := map[string]int{}
	var visit func(name string) string
	visit = func(name string) string {
		switch state[name] {
		case visiting:
			return name
		case done:
			return ""
		}
		state[name] = visiting
		for dependency := range project.Services[name].DependsOn {
			if cycle := visit(dependency); cycle != "" {
				return cycle
			}
		}
		state[name] = done
		return ""
	}
	names := make([]string, 0, len(project.Services))
	for name := range project.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if cycle := visit(name); cycle != "" {
			return cycle
		}
	}
	return ""
}
