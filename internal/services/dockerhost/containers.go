package dockerhost

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/dockerengine"
)

// Container states the router reports besides Docker's own.
const (
	StateMissing   = "missing"
	StatePreparing = "preparing"
	StateMixed     = "mixed"
)

// Status is the plugin view of one container.
type Status struct {
	Key       string `json:"key"`
	Service   string `json:"service,omitempty"`
	State     string `json:"state"`
	Health    string `json:"health"`
	ExitCode  int    `json:"exit_code"`
	StartedAt string `json:"started_at,omitempty"`
	Image     string `json:"image,omitempty"`
}

// ProjectStatus is the plugin view of one compose project.
type ProjectStatus struct {
	Key      string            `json:"key"`
	State    string            `json:"state"`
	Services map[string]Status `json:"services"`
}

func statusOf(target target, details *dockerengine.ContainerDetails) Status {
	status := Status{Key: target.key, Service: target.service, State: StateMissing, Health: "none"}
	if details == nil {
		return status
	}
	status.State = details.State.Status
	status.ExitCode = details.State.ExitCode
	status.Image = details.Config.Image
	if details.State.Running {
		status.StartedAt = details.State.StartedAt
	}
	if details.State.Health != nil && details.State.Health.Status != "" {
		status.Health = details.State.Health.Status
	}
	return status
}

// aggregateState folds service states into one project state.
func aggregateState(services map[string]Status) string {
	if len(services) == 0 {
		return StateMissing
	}
	state := ""
	for _, status := range services {
		switch {
		case status.State == StatePreparing:
			return StatePreparing
		case state == "":
			state = status.State
		case state != status.State:
			state = StateMixed
		}
	}
	return state
}

// findContainer reads the container of a target. A missing container is
// (nil, nil). A container of that name without matching labels is an
// ownership conflict.
func (service *Service) findContainer(ctx context.Context, current *session, pluginID string, target target) (*dockerengine.ContainerDetails, error) {
	name := current.containerName(pluginID, target)
	details, err := current.client.ContainerInspect(ctx, name)
	if errors.Is(err, dockerengine.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, service.faultFromError(err, "inspect container %s", target)
	}
	labels := details.Config.Labels
	if labels[labelNamespace] != current.namespace || labels[labelPlugin] != pluginID || labels[labelKey] != target.key || labels[labelService] != target.service {
		return nil, newFault(ReasonOwnershipConflict, nil, "container %s exists and is not owned by this plugin", name)
	}
	return &details, nil
}

// pluginContainers lists the workload containers of a plugin, optionally of
// one key.
func (service *Service) pluginContainers(ctx context.Context, current *session, pluginID, key string) ([]dockerengine.ContainerSummary, error) {
	labels := map[string]string{labelNamespace: current.namespace, labelPlugin: pluginID}
	if key != "" {
		labels[labelKey] = key
	}
	listed, err := current.client.ContainerList(ctx, labels)
	if err != nil {
		return nil, service.faultFromError(err, "list containers")
	}
	workloads := listed[:0]
	for _, summary := range listed {
		if role := summary.Labels[labelRole]; role == roleContainer || role == roleService {
			workloads = append(workloads, summary)
		}
	}
	return workloads, nil
}

// keyKind reports whether a key holds a standalone container, a compose
// project or nothing.
func (service *Service) keyKind(ctx context.Context, current *session, pluginID, key string) (string, []dockerengine.ContainerSummary, error) {
	containers, err := service.pluginContainers(ctx, current, pluginID, key)
	if err != nil {
		return "", nil, err
	}
	for _, summary := range containers {
		if summary.Labels[labelRole] == roleService {
			return roleService, containers, nil
		}
	}
	if len(containers) > 0 {
		return roleContainer, containers, nil
	}
	return "", nil, nil
}

// placement is where one container lives.
type placement struct {
	target    target
	network   string
	networkID string
	noProxy   []string
	volumeKey string
}

// buildConfig renders the engine config of a validated spec.
func (current *session) buildConfig(policy Policy, place placement, spec ContainerSpec, image string, gateway gateway) dockerengine.ContainerConfig {
	role := roleContainer
	alias := place.target.key
	if place.target.service != "" {
		role = roleService
		alias = place.target.service
	}
	labels := current.ownerLabels(policy.PluginID, role)
	labels[labelKey] = place.target.key
	if place.target.service != "" {
		labels[labelService] = place.target.service
	}
	ports := make([]string, 0, len(spec.Ports))
	exposed := map[string]struct{}{}
	for _, port := range spec.Ports {
		ports = append(ports, strconv.Itoa(port))
		exposed[strconv.Itoa(port)+"/tcp"] = struct{}{}
	}
	labels[labelPorts] = strings.Join(ports, ",")
	if len(spec.DependsOn) > 0 {
		dependencies := make([]string, 0, len(spec.DependsOn))
		for _, name := range sortedKeys(spec.DependsOn) {
			dependencies = append(dependencies, name+":"+spec.DependsOn[name])
		}
		labels[labelDependsOn] = strings.Join(dependencies, ",")
	}

	proxy := gateway.proxyURL()
	noProxy := append([]string{"localhost", "127.0.0.1", "::1", gatewayAlias}, place.noProxy...)
	environment := []string{
		"HTTP_PROXY=" + proxy, "HTTPS_PROXY=" + proxy, "http_proxy=" + proxy, "https_proxy=" + proxy,
		"NO_PROXY=" + strings.Join(noProxy, ","), "no_proxy=" + strings.Join(noProxy, ","),
	}
	for _, name := range sortedKeys(spec.Environment) {
		environment = append(environment, name+"="+spec.Environment[name])
	}
	mounts := make([]dockerengine.Mount, 0, len(spec.Volumes))
	for _, volume := range sortedKeys(spec.Volumes) {
		mounts = append(mounts, dockerengine.Mount{Type: "volume", Source: current.volumeName(policy.PluginID, place.volumeKey, volume), Target: spec.Volumes[volume]})
	}
	restart := dockerengine.RestartPolicy{Name: "no"}
	if spec.Restart == RestartOnFailure {
		restart = dockerengine.RestartPolicy{Name: "on-failure", MaximumRetryCount: 5}
	}
	memory := int64(spec.MemoryLimitMB) << 20
	host := dockerengine.HostConfig{
		Memory:         memory,
		MemorySwap:     memory,
		NanoCPUs:       int64(spec.CPULimit * 1e9),
		PidsLimit:      int64(policy.Directives.PidsLimit),
		ShmSize:        int64(spec.ShmSizeMB) << 20,
		CapDrop:        []string{"NET_RAW", "MKNOD", "AUDIT_WRITE"},
		SecurityOpt:    []string{"no-new-privileges:true"},
		Init:           true,
		ReadonlyRootfs: spec.ReadOnly,
		NetworkMode:    place.network,
		RestartPolicy:  restart,
		LogConfig:      containerLogConfig(),
		Mounts:         mounts,
	}
	if spec.GPUs > 0 {
		host.DeviceRequests = []dockerengine.DeviceRequest{{Driver: "nvidia", Count: spec.GPUs, Capabilities: [][]string{{"gpu"}}}}
	}
	config := dockerengine.ContainerConfig{
		Hostname:     alias,
		User:         spec.User,
		Env:          environment,
		Cmd:          spec.Command,
		Entrypoint:   spec.Entrypoint,
		Image:        image,
		WorkingDir:   spec.WorkingDirectory,
		Labels:       labels,
		ExposedPorts: exposed,
		HostConfig:   host,
		NetworkingConfig: &dockerengine.NetworkingConfig{EndpointsConfig: map[string]dockerengine.EndpointSettings{
			place.network: {Aliases: []string{alias}},
		}},
	}
	if spec.Health != nil {
		config.Healthcheck = &dockerengine.HealthConfig{
			Test:        append([]string{"CMD"}, spec.Health.Command...),
			Interval:    int64(spec.Health.IntervalSeconds * 1e9),
			Timeout:     int64(spec.Health.TimeoutSeconds * 1e9),
			StartPeriod: int64(spec.Health.StartPeriodSeconds * 1e9),
			Retries:     spec.Health.Retries,
		}
	}
	return config
}

// configHash identifies a rendered config plus its files. A changed hash
// recreates the container.
func configHash(config dockerengine.ContainerConfig, files map[string]string) string {
	encoded, err := json.Marshal(struct {
		Config dockerengine.ContainerConfig `json:"config"`
		Files  map[string]string            `json:"files"`
	}{config, files})
	if err != nil {
		panic("encode container config: " + err.Error())
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:16])
}

// reconcile makes one container match its spec. It returns preparing
// while the image is being pulled or built.
func (service *Service) reconcile(ctx context.Context, current *session, policy Policy, place placement, spec ContainerSpec) (Status, error) {
	request := current.resolveImage(policy, spec)
	ready, err := service.ensureImage(ctx, current, request)
	if err != nil {
		return Status{}, err
	}
	if !ready {
		return Status{Key: place.target.key, Service: place.target.service, State: StatePreparing, Health: "none", Image: request.reference}, nil
	}
	if spec.GPUs > 0 && !current.info.GPUCapable() {
		return Status{}, newFault(ReasonGPUUnavailable, nil, "docker host has no NVIDIA runtime or CDI devices for %s", place.target)
	}
	gateway, err := service.ensureGateway(ctx, current, policy, place.networkID)
	if err != nil {
		return Status{}, err
	}
	config := current.buildConfig(policy, place, spec, request.reference, gateway)
	hash := configHash(config, spec.Files)
	config.Labels[labelSpec] = hash

	existing, err := service.findContainer(ctx, current, policy.PluginID, place.target)
	if err != nil {
		return Status{}, err
	}
	if existing != nil && existing.Config.Labels[labelSpec] == hash {
		stale, err := service.holdsRemovedNetwork(ctx, current, *existing)
		if err != nil {
			return Status{}, err
		}
		if !stale {
			return statusOf(place.target, existing), nil
		}
	}
	name := current.containerName(policy.PluginID, place.target)
	if existing != nil {
		if err := current.client.ContainerRemove(ctx, existing.ID); err != nil && !errors.Is(err, dockerengine.ErrNotFound) {
			return Status{}, service.faultFromError(err, "replace container %s", place.target)
		}
		service.logInfo("Docker container replaced", "plugin_id", policy.PluginID, "container_key", place.target.String())
	} else {
		containers, err := service.pluginContainers(ctx, current, policy.PluginID, "")
		if err != nil {
			return Status{}, err
		}
		if len(containers)+1 > policy.Directives.ContainerLimit {
			return Status{}, newFault(ReasonContainerLimit, nil, "plugin runs %d containers: @docker.container_limit is %d", len(containers), policy.Directives.ContainerLimit)
		}
	}
	for _, mount := range config.HostConfig.Mounts {
		labels := current.ownerLabels(policy.PluginID, roleVolume)
		labels[labelKey] = place.volumeKey
		if _, err := current.client.VolumeCreate(ctx, mount.Source, labels); err != nil {
			return Status{}, service.faultFromError(err, "create volume %s", mount.Source)
		}
	}
	id, err := current.client.ContainerCreate(ctx, name, config)
	if err != nil {
		return Status{}, service.faultFromError(err, "create container %s", place.target)
	}
	if len(spec.Files) > 0 {
		archive, err := filesArchive(spec.Files)
		if err != nil {
			return Status{}, newFault(ReasonDaemonError, err, "pack files of %s: %v", place.target, err)
		}
		if err := current.client.ContainerPutArchive(ctx, id, "/", bytes.NewReader(archive)); err != nil {
			_ = current.client.ContainerRemove(ctx, id)
			return Status{}, service.faultFromError(err, "upload files of %s", place.target)
		}
	}
	service.dropTransports(func(key transportKey) bool {
		return key.pluginID == policy.PluginID && key.target == place.target.String()
	})
	service.logInfo("Docker container created", "plugin_id", policy.PluginID, "container_key", place.target.String(), "image", request.reference)
	details, err := current.client.ContainerInspect(ctx, id)
	if err != nil {
		return Status{}, service.faultFromError(err, "inspect container %s", place.target)
	}
	return statusOf(place.target, &details), nil
}

// Ensure makes one standalone container match spec.
func (service *Service) Ensure(ctx context.Context, policy Policy, key string, spec ContainerSpec) (Status, error) {
	if !ValidKey(key) {
		return Status{}, specError("key %q must match %s", key, keyPattern.String())
	}
	if spec.DependsOn != nil {
		return Status{}, specError("depends_on is a compose field")
	}
	if err := validateContainer(&spec, policy, key); err != nil {
		return Status{}, err
	}
	current, err := service.session(ctx)
	if err != nil {
		return Status{}, err
	}
	unlock := service.lock(policy.PluginID, key)
	defer unlock()
	kind, _, err := service.keyKind(ctx, current, policy.PluginID, key)
	if err != nil {
		return Status{}, err
	}
	if kind == roleService {
		return Status{}, specError("key %q holds a compose project", key)
	}
	network := current.networkName(policy.PluginID, "")
	networkID, err := service.ensureNetwork(ctx, current, network, true, current.ownerLabels(policy.PluginID, roleNetwork))
	if err != nil {
		return Status{}, err
	}
	place := placement{target: target{key: key}, network: network, networkID: networkID, noProxy: []string{key}}
	return service.reconcile(ctx, current, policy, place, spec)
}

// Compose makes one compose project match its services. Services the
// project no longer lists are removed with their volumes kept.
func (service *Service) Compose(ctx context.Context, policy Policy, key string, project ProjectSpec) (ProjectStatus, error) {
	if !ValidKey(key) {
		return ProjectStatus{}, specError("key %q must match %s", key, keyPattern.String())
	}
	if err := validateProject(&project, policy); err != nil {
		return ProjectStatus{}, err
	}
	current, err := service.session(ctx)
	if err != nil {
		return ProjectStatus{}, err
	}
	unlock := service.lock(policy.PluginID, key)
	defer unlock()
	kind, existing, err := service.keyKind(ctx, current, policy.PluginID, key)
	if err != nil {
		return ProjectStatus{}, err
	}
	if kind == roleContainer {
		return ProjectStatus{}, specError("key %q holds a standalone container", key)
	}
	for _, summary := range existing {
		if _, listed := project.Services[summary.Labels[labelService]]; listed {
			continue
		}
		if err := current.client.ContainerRemove(ctx, summary.ID); err != nil && !errors.Is(err, dockerengine.ErrNotFound) {
			return ProjectStatus{}, service.faultFromError(err, "remove service %s", summary.Labels[labelService])
		}
	}
	network := current.networkName(policy.PluginID, key)
	labels := current.ownerLabels(policy.PluginID, roleNetwork)
	labels[labelKey] = key
	networkID, err := service.ensureNetwork(ctx, current, network, true, labels)
	if err != nil {
		return ProjectStatus{}, err
	}
	names := project.ServiceNames()
	result := ProjectStatus{Key: key, Services: map[string]Status{}}
	for _, name := range names {
		place := placement{target: target{key: key, service: name}, network: network, networkID: networkID, noProxy: names, volumeKey: key}
		status, err := service.reconcile(ctx, current, policy, place, project.Services[name])
		if err != nil {
			return ProjectStatus{}, err
		}
		result.Services[name] = status
	}
	result.State = aggregateState(result.Services)
	return result, nil
}

// targetContainers resolves a target to its containers: one container, or
// every service of a project in start order.
func (service *Service) targetContainers(ctx context.Context, current *session, policy Policy, text string) (target, []target, []*dockerengine.ContainerDetails, error) {
	parsed, err := parseTarget(text)
	if err != nil {
		return target{}, nil, nil, err
	}
	if parsed.service != "" {
		details, err := service.findContainer(ctx, current, policy.PluginID, parsed)
		if err != nil {
			return parsed, nil, nil, err
		}
		return parsed, []target{parsed}, []*dockerengine.ContainerDetails{details}, nil
	}
	kind, summaries, err := service.keyKind(ctx, current, policy.PluginID, parsed.key)
	if err != nil {
		return parsed, nil, nil, err
	}
	if kind != roleService {
		details, err := service.findContainer(ctx, current, policy.PluginID, parsed)
		if err != nil {
			return parsed, nil, nil, err
		}
		return parsed, []target{parsed}, []*dockerengine.ContainerDetails{details}, nil
	}
	sort.Slice(summaries, func(left, right int) bool {
		return summaries[left].Labels[labelService] < summaries[right].Labels[labelService]
	})
	targets := make([]target, 0, len(summaries))
	containers := make([]*dockerengine.ContainerDetails, 0, len(summaries))
	for _, summary := range summaries {
		serviceTarget := target{key: parsed.key, service: summary.Labels[labelService]}
		details, err := service.findContainer(ctx, current, policy.PluginID, serviceTarget)
		if err != nil {
			return parsed, nil, nil, err
		}
		targets = append(targets, serviceTarget)
		containers = append(containers, details)
	}
	return parsed, targets, containers, nil
}

// statusResult renders a container or project answer.
func statusResult(parsed target, targets []target, containers []*dockerengine.ContainerDetails) any {
	if len(targets) == 1 && targets[0] == parsed {
		return statusOf(parsed, containers[0])
	}
	project := ProjectStatus{Key: parsed.key, Services: map[string]Status{}}
	for index, serviceTarget := range targets {
		project.Services[serviceTarget.service] = statusOf(serviceTarget, containers[index])
	}
	project.State = aggregateState(project.Services)
	return project
}

// Status reads one container or project. A missing target reports state
// missing.
func (service *Service) Status(ctx context.Context, policy Policy, text string) (any, error) {
	current, err := service.session(ctx)
	if err != nil {
		return nil, err
	}
	parsed, targets, containers, err := service.targetContainers(ctx, current, policy, text)
	if err != nil {
		return nil, err
	}
	return statusResult(parsed, targets, containers), nil
}

// List reads every workload container of a plugin.
func (service *Service) List(ctx context.Context, policy Policy) ([]Status, error) {
	current, err := service.session(ctx)
	if err != nil {
		return nil, err
	}
	return service.listStatuses(ctx, current, policy.PluginID)
}

func (service *Service) listStatuses(ctx context.Context, current *session, pluginID string) ([]Status, error) {
	summaries, err := service.pluginContainers(ctx, current, pluginID, "")
	if err != nil {
		return nil, err
	}
	statuses := make([]Status, 0, len(summaries))
	for _, summary := range summaries {
		serviceTarget := target{key: summary.Labels[labelKey], service: summary.Labels[labelService]}
		details, err := current.client.ContainerInspect(ctx, summary.ID)
		if errors.Is(err, dockerengine.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, service.faultFromError(err, "inspect container %s", serviceTarget)
		}
		statuses = append(statuses, statusOf(serviceTarget, &details))
	}
	sort.Slice(statuses, func(left, right int) bool {
		if statuses[left].Key != statuses[right].Key {
			return statuses[left].Key < statuses[right].Key
		}
		return statuses[left].Service < statuses[right].Service
	})
	return statuses, nil
}

// Start starts one container or every service of a project in dependency
// order. Already running is a no-op; a missing target is not_found.
func (service *Service) Start(ctx context.Context, policy Policy, text string) (any, error) {
	current, err := service.session(ctx)
	if err != nil {
		return nil, err
	}
	parsed, err := parseTarget(text)
	if err != nil {
		return nil, err
	}
	unlock := service.lock(policy.PluginID, parsed.key)
	defer unlock()
	parsed, targets, containers, err := service.targetContainers(ctx, current, policy, text)
	if err != nil {
		return nil, err
	}
	for index, details := range containers {
		if details == nil {
			return nil, newFault(ReasonNotFound, nil, "container %s does not exist: call ensure or compose first", targets[index])
		}
	}
	project := ""
	if targets[0].service != "" {
		project = targets[0].key
	}
	networkName := current.networkName(policy.PluginID, project)
	network, err := current.client.NetworkInspect(ctx, networkName)
	if err != nil {
		return nil, service.faultFromError(err, "inspect network %s", networkName)
	}
	if _, err := service.ensureGateway(ctx, current, policy, network.ID); err != nil {
		return nil, err
	}
	byService := map[string]int{}
	for index, serviceTarget := range targets {
		byService[serviceTarget.service] = index
	}
	for _, index := range startOrder(targets, containers) {
		details := containers[index]
		if details.State.Running {
			continue
		}
		for _, dependency := range dependencies(details) {
			position, sibling := byService[dependency.service]
			if !sibling || dependency.condition != ConditionHealthy {
				continue
			}
			healthy, err := service.awaitHealthy(ctx, current, targets[position], containers[position].ID)
			if err != nil {
				return nil, err
			}
			containers[position] = healthy
		}
		if err := current.client.ContainerStart(ctx, details.ID); err != nil {
			return nil, service.faultFromError(err, "start %s", targets[index])
		}
		refreshed, err := current.client.ContainerInspect(ctx, details.ID)
		if err != nil {
			return nil, service.faultFromError(err, "inspect %s", targets[index])
		}
		containers[index] = &refreshed
		service.logInfo("Docker container started", "plugin_id", policy.PluginID, "container_key", targets[index].String())
	}
	return statusResult(parsed, targets, containers), nil
}

// dependency is one depends_on entry read back from a container label.
type dependency struct {
	service   string
	condition string
}

func dependencies(details *dockerengine.ContainerDetails) []dependency {
	text := details.Config.Labels[labelDependsOn]
	if text == "" {
		return nil
	}
	parsed := []dependency{}
	for _, entry := range strings.Split(text, ",") {
		name, condition, _ := strings.Cut(entry, ":")
		parsed = append(parsed, dependency{service: name, condition: condition})
	}
	return parsed
}

// startOrder returns container indexes with dependencies first. Targets
// arrive sorted by service name, which breaks ties.
func startOrder(targets []target, containers []*dockerengine.ContainerDetails) []int {
	byService := map[string]int{}
	for index, serviceTarget := range targets {
		byService[serviceTarget.service] = index
	}
	order := make([]int, 0, len(targets))
	placed := map[int]bool{}
	var place func(index int)
	place = func(index int) {
		if placed[index] {
			return
		}
		placed[index] = true
		for _, entry := range dependencies(containers[index]) {
			if position, ok := byService[entry.service]; ok {
				place(position)
			}
		}
		order = append(order, index)
	}
	for index := range targets {
		place(index)
	}
	return order
}

// awaitHealthy waits until a started dependency reports healthy.
func (service *Service) awaitHealthy(ctx context.Context, current *session, serviceTarget target, id string) (*dockerengine.ContainerDetails, error) {
	for {
		details, err := current.client.ContainerInspect(ctx, id)
		if err != nil {
			return nil, service.faultFromError(err, "inspect %s", serviceTarget)
		}
		switch {
		case details.State.Health != nil && details.State.Health.Status == "healthy":
			return &details, nil
		case details.State.Status == "exited" || details.State.Status == "dead":
			return nil, newFault(ReasonNotRunning, nil, "dependency %s exited with code %d", serviceTarget, details.State.ExitCode)
		case details.State.Health != nil && details.State.Health.Status == "unhealthy":
			return nil, newFault(ReasonNotRunning, nil, "dependency %s is unhealthy", serviceTarget)
		}
		select {
		case <-ctx.Done():
			return nil, newFault(ReasonTimeout, ctx.Err(), "dependency %s did not become healthy in time", serviceTarget)
		case <-time.After(waitPollInterval):
		}
	}
}

// Stop stops one container or a project. Stopped or missing is a no-op.
func (service *Service) Stop(ctx context.Context, policy Policy, text string, timeoutSeconds int) (any, error) {
	current, err := service.session(ctx)
	if err != nil {
		return nil, err
	}
	return service.stopTarget(ctx, current, policy, text, timeoutSeconds)
}

func (service *Service) stopTarget(ctx context.Context, current *session, policy Policy, text string, timeoutSeconds int) (any, error) {
	parsed, err := parseTarget(text)
	if err != nil {
		return nil, err
	}
	unlock := service.lock(policy.PluginID, parsed.key)
	defer unlock()
	parsed, targets, containers, err := service.targetContainers(ctx, current, policy, text)
	if err != nil {
		return nil, err
	}
	present := []int{}
	for index, details := range containers {
		if details != nil {
			present = append(present, index)
		}
	}
	presentTargets := make([]target, len(present))
	presentContainers := make([]*dockerengine.ContainerDetails, len(present))
	for position, index := range present {
		presentTargets[position] = targets[index]
		presentContainers[position] = containers[index]
	}
	order := startOrder(presentTargets, presentContainers)
	for position := len(order) - 1; position >= 0; position-- {
		index := present[order[position]]
		details := containers[index]
		if !details.State.Running {
			continue
		}
		if err := current.client.ContainerStop(ctx, details.ID, timeoutSeconds); err != nil {
			return nil, service.faultFromError(err, "stop %s", targets[index])
		}
		refreshed, err := current.client.ContainerInspect(ctx, details.ID)
		if err != nil {
			return nil, service.faultFromError(err, "inspect %s", targets[index])
		}
		containers[index] = &refreshed
		service.logInfo("Docker container stopped", "plugin_id", policy.PluginID, "container_key", targets[index].String())
	}
	return statusResult(parsed, targets, containers), nil
}

// Remove removes one container or a project. Missing is a no-op. With
// volumes the target's volumes go too.
func (service *Service) Remove(ctx context.Context, policy Policy, text string, volumes bool) error {
	current, err := service.session(ctx)
	if err != nil {
		return err
	}
	return service.removeTarget(ctx, current, policy, text, volumes)
}

func (service *Service) removeTarget(ctx context.Context, current *session, policy Policy, text string, volumes bool) error {
	parsed, err := parseTarget(text)
	if err != nil {
		return err
	}
	unlock := service.lock(policy.PluginID, parsed.key)
	defer unlock()
	_, targets, containers, err := service.targetContainers(ctx, current, policy, text)
	if err != nil {
		return err
	}
	for index, details := range containers {
		if details == nil {
			continue
		}
		if err := current.client.ContainerRemove(ctx, details.ID); err != nil && !errors.Is(err, dockerengine.ErrNotFound) {
			return service.faultFromError(err, "remove %s", targets[index])
		}
		removed := targets[index].String()
		service.dropTransports(func(key transportKey) bool { return key.pluginID == policy.PluginID && key.target == removed })
		service.logInfo("Docker container removed", "plugin_id", policy.PluginID, "container_key", targets[index].String())
	}
	projectRemoved := parsed.service == "" && len(targets) > 0 && targets[0].service != ""
	if projectRemoved {
		if err := service.removeNetwork(ctx, current, current.networkName(policy.PluginID, parsed.key)); err != nil {
			return err
		}
	}
	if volumes {
		volumeKey := ""
		if len(targets) > 0 && targets[0].service != "" {
			volumeKey = targets[0].key
		}
		if err := service.removeVolumes(ctx, current, policy.PluginID, volumeKey, containers); err != nil {
			return err
		}
	}
	return nil
}

// removeNetwork detaches every container and removes a router network.
func (service *Service) removeNetwork(ctx context.Context, current *session, name string) error {
	network, err := current.client.NetworkInspect(ctx, name)
	if errors.Is(err, dockerengine.ErrNotFound) {
		return nil
	}
	if err != nil {
		return service.faultFromError(err, "inspect network %s", name)
	}
	if network.Labels[labelNamespace] != current.namespace {
		return newFault(ReasonOwnershipConflict, nil, "network %s is not owned by this router", name)
	}
	gatewayName := current.gatewayName(network.Labels[labelPlugin])
	if err := current.client.NetworkDisconnect(ctx, network.ID, gatewayName); err != nil && !errors.Is(err, dockerengine.ErrNotFound) {
		service.logDebug("Docker gateway not detached", "network", name, "error", err)
	}
	if err := current.client.NetworkRemove(ctx, network.ID); err != nil && !errors.Is(err, dockerengine.ErrNotFound) {
		return service.faultFromError(err, "remove network %s", name)
	}
	return nil
}

// removeVolumes removes the named volumes the removed containers mounted.
func (service *Service) removeVolumes(ctx context.Context, current *session, pluginID, volumeKey string, containers []*dockerengine.ContainerDetails) error {
	labels := current.ownerLabels(pluginID, roleVolume)
	labels[labelKey] = volumeKey
	volumes, err := current.client.VolumeList(ctx, labels)
	if err != nil {
		return service.faultFromError(err, "list volumes")
	}
	mounted := map[string]bool{}
	for _, details := range containers {
		if details == nil {
			continue
		}
		for _, mount := range details.Mounts {
			mounted[mount.Name] = true
		}
	}
	for _, volume := range volumes {
		if !mounted[volume.Name] {
			continue
		}
		if err := current.client.VolumeRemove(ctx, volume.Name); err != nil && !errors.Is(err, dockerengine.ErrNotFound) {
			return service.faultFromError(err, "remove volume %s", volume.Name)
		}
	}
	return nil
}

// runningContainer resolves one container target that must be running.
func (service *Service) runningContainer(ctx context.Context, current *session, policy Policy, text string) (target, *dockerengine.ContainerDetails, error) {
	parsed, targets, containers, err := service.targetContainers(ctx, current, policy, text)
	if err != nil {
		return target{}, nil, err
	}
	if len(targets) != 1 || targets[0] != parsed {
		return target{}, nil, specError("target %q is a compose project: name a service as %s/<service>", text, parsed.key)
	}
	if containers[0] == nil {
		return parsed, nil, newFault(ReasonNotFound, nil, "container %s does not exist", parsed)
	}
	if !containers[0].State.Running {
		return parsed, containers[0], newFault(ReasonNotRunning, nil, "container %s is %s", parsed, containers[0].State.Status)
	}
	return parsed, containers[0], nil
}

// Logs reads the last lines of one container, stdout and stderr merged.
func (service *Service) Logs(ctx context.Context, policy Policy, text string, tail int) (string, error) {
	current, err := service.session(ctx)
	if err != nil {
		return "", err
	}
	parsed, targets, containers, err := service.targetContainers(ctx, current, policy, text)
	if err != nil {
		return "", err
	}
	if len(targets) != 1 || targets[0] != parsed {
		return "", specError("target %q is a compose project: name a service as %s/<service>", text, parsed.key)
	}
	if containers[0] == nil {
		return "", newFault(ReasonNotFound, nil, "container %s does not exist", parsed)
	}
	stdout, stderr, err := current.client.ContainerLogs(ctx, containers[0].ID, tail, 0, false, maximumLogBytes)
	if err != nil && !errors.Is(err, dockerengine.ErrOutputLimit) {
		return "", service.faultFromError(err, "read logs of %s", parsed)
	}
	combined := stdout + stderr
	if len(combined) > maximumLogBytes {
		combined = combined[len(combined)-maximumLogBytes:]
	}
	return combined, nil
}

// maximumLogBytes caps one logs answer.
const maximumLogBytes = 64 << 10

// Maximum output of one exec stream.
const maximumExecOutput = 1 << 20

// CommandOptions configures one exec.
type CommandOptions struct {
	Environment      map[string]string
	User             string
	WorkingDirectory string
	Stdin            *string
	Timeout          time.Duration
}

// CommandResult is the outcome of one exec.
type CommandResult struct {
	ExitCode  int    `json:"exit_code"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Truncated bool   `json:"truncated"`
}

// RunCommand runs one command in a running container and waits for it.
func (service *Service) RunCommand(ctx context.Context, policy Policy, text string, command []string, options CommandOptions) (CommandResult, error) {
	if len(command) == 0 || len(command) > maximumArguments {
		return CommandResult{}, specError("exec: command takes 1 to %d arguments", maximumArguments)
	}
	for name := range options.Environment {
		if !environmentNamePattern.MatchString(name) {
			return CommandResult{}, specError("exec: environment name %q is invalid", name)
		}
	}
	current, err := service.session(ctx)
	if err != nil {
		return CommandResult{}, err
	}
	parsed, details, err := service.runningContainer(ctx, current, policy, text)
	if err != nil {
		return CommandResult{}, err
	}
	runContext := ctx
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		runContext, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}
	environment := make([]string, 0, len(options.Environment))
	for _, name := range sortedKeys(options.Environment) {
		environment = append(environment, name+"="+options.Environment[name])
	}
	var stdin []byte
	if options.Stdin != nil {
		stdin = []byte(*options.Stdin)
	}
	result, err := current.client.ExecRun(runContext, details.ID, dockerengine.ExecConfig{
		Cmd: command, Env: environment, User: options.User, WorkingDir: options.WorkingDirectory,
	}, stdin, maximumExecOutput)
	if err != nil {
		if runContext.Err() != nil {
			return CommandResult{}, newFault(ReasonTimeout, err, "exec in %s timed out", parsed)
		}
		return CommandResult{}, service.faultFromError(err, "exec in %s", parsed)
	}
	service.logDebug("Docker exec finished", "plugin_id", policy.PluginID, "container_key", parsed.String(), "status", result.ExitCode)
	return CommandResult{ExitCode: result.ExitCode, Stdout: string(result.Stdout), Stderr: string(result.Stderr), Truncated: result.Truncated}, nil
}

// waitPollInterval spaces state reads while waiting.
const waitPollInterval = 500 * time.Millisecond

// Wait blocks until the target runs, is healthy when it has a health check
// and, with port set, accepts a connection on it.
func (service *Service) Wait(ctx context.Context, policy Policy, text string, port int, timeout time.Duration) (any, error) {
	current, err := service.session(ctx)
	if err != nil {
		return nil, err
	}
	waitContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		parsed, targets, containers, err := service.targetContainers(waitContext, current, policy, text)
		if err != nil {
			if waitContext.Err() != nil {
				return nil, newFault(ReasonTimeout, err, "wait for %s timed out", text)
			}
			return nil, err
		}
		ready := true
		for index, details := range containers {
			switch {
			case details == nil:
				return nil, newFault(ReasonNotFound, nil, "container %s does not exist", targets[index])
			case details.State.Status == "exited" || details.State.Status == "dead":
				return nil, newFault(ReasonNotRunning, nil, "container %s exited with code %d", targets[index], details.State.ExitCode)
			case !details.State.Running:
				ready = false
			case details.State.Health != nil && details.State.Health.Status != "healthy":
				if details.State.Health.Status == "unhealthy" {
					return nil, newFault(ReasonNotRunning, nil, "container %s is unhealthy", targets[index])
				}
				ready = false
			}
		}
		if ready && port > 0 {
			if len(targets) != 1 || targets[0] != parsed {
				return nil, specError("wait: port needs a container target, not the project %q", text)
			}
			conn, err := service.dialContainer(waitContext, current, policy, parsed, containers[0], port)
			if err == nil {
				conn.Close()
			} else {
				ready = false
			}
		}
		if ready {
			return statusResult(parsed, targets, containers), nil
		}
		select {
		case <-waitContext.Done():
			return nil, newFault(ReasonTimeout, waitContext.Err(), "wait for %s timed out", text)
		case <-time.After(waitPollInterval):
		}
	}
}
