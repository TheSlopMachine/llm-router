package dockerhost

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/dockerengine"
	"github.com/TheSlopMachine/llm-router/internal/dockergateway"
)

// Background and shutdown bounds.
const (
	sweepInterval          = time.Hour
	gatewayEventInterval   = 30 * time.Second
	shutdownStopTimeout    = 10
	shutdownWorkers        = 4
	backgroundCallTimeout  = 2 * time.Minute
	containerActionTimeout = 10
)

// StopPlugin stops every container of a plugin, its gateway included.
// Missing Docker settings stop nothing.
func (service *Service) StopPlugin(ctx context.Context, pluginID string) error {
	current, err := service.session(ctx)
	if err != nil {
		if fault, ok := AsFault(err); ok && fault.Reason == ReasonUnconfigured {
			return nil
		}
		return err
	}
	listed, err := current.client.ContainerList(ctx, map[string]string{labelNamespace: current.namespace, labelPlugin: pluginID})
	if err != nil {
		return service.faultFromError(err, "list containers")
	}
	return service.stopContainers(ctx, current, listed)
}

// stopContainers stops running containers with a bounded worker pool.
func (service *Service) stopContainers(ctx context.Context, current *session, containers []dockerengine.ContainerSummary) error {
	work := make(chan dockerengine.ContainerSummary)
	var group sync.WaitGroup
	var failureMu sync.Mutex
	var failure error
	for worker := 0; worker < shutdownWorkers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for summary := range work {
				if err := current.client.ContainerStop(ctx, summary.ID, shutdownStopTimeout); err != nil && !errors.Is(err, dockerengine.ErrNotFound) {
					failureMu.Lock()
					failure = errors.Join(failure, service.faultFromError(err, "stop %s", strings.TrimPrefix(firstName(summary), "/")))
					failureMu.Unlock()
				}
			}
		}()
	}
	for _, summary := range containers {
		if summary.State == "running" || summary.State == "restarting" || summary.State == "paused" {
			work <- summary
		}
	}
	close(work)
	group.Wait()
	return failure
}

func firstName(summary dockerengine.ContainerSummary) string {
	if len(summary.Names) > 0 {
		return summary.Names[0]
	}
	return summary.ID
}

// StopAll stops every running container of this router. The shutdown path
// calls it after plugin deinitialize hooks.
func (service *Service) StopAll(ctx context.Context) error {
	current, err := service.session(ctx)
	if err != nil {
		if fault, ok := AsFault(err); ok && fault.Reason == ReasonUnconfigured {
			return nil
		}
		return err
	}
	listed, err := current.client.ContainerList(ctx, current.namespaceLabels())
	if err != nil {
		return service.faultFromError(err, "list containers")
	}
	err = service.stopContainers(ctx, current, listed)
	if err == nil {
		service.logInfo("Docker containers stopped", "count", len(listed))
	}
	return err
}

// RemovePlugin removes every object of a plugin: containers, gateway,
// networks, volumes and built images.
func (service *Service) RemovePlugin(ctx context.Context, pluginID string) error {
	current, err := service.session(ctx)
	if err != nil {
		if fault, ok := AsFault(err); ok && fault.Reason == ReasonUnconfigured {
			return nil
		}
		return err
	}
	err = service.removeOwned(ctx, current, func(labels map[string]string) bool {
		return labels[labelPlugin] == pluginID
	})
	if err == nil {
		service.logInfo("Docker objects of plugin removed", "plugin_id", pluginID)
	}
	return err
}

// removeOwned removes namespace objects whose labels match: containers
// first, then networks, volumes and images.
func (service *Service) removeOwned(ctx context.Context, current *session, match func(map[string]string) bool) error {
	var failures error
	containers, err := current.client.ContainerList(ctx, current.namespaceLabels())
	if err != nil {
		return service.faultFromError(err, "list containers")
	}
	for _, summary := range containers {
		if summary.Labels[labelPlugin] == "" || !match(summary.Labels) {
			continue
		}
		if err := current.client.ContainerRemove(ctx, summary.ID); err != nil && !errors.Is(err, dockerengine.ErrNotFound) {
			failures = errors.Join(failures, service.faultFromError(err, "remove container %s", firstName(summary)))
		}
		owner := summary.Labels[labelPlugin]
		service.dropTransports(func(key transportKey) bool { return key.pluginID == owner })
	}
	networks, err := current.client.NetworkList(ctx, current.namespaceLabels())
	if err != nil {
		return errors.Join(failures, service.faultFromError(err, "list networks"))
	}
	for _, network := range networks {
		if network.Labels[labelPlugin] == "" || !match(network.Labels) {
			continue
		}
		if err := current.client.NetworkRemove(ctx, network.ID); err != nil && !errors.Is(err, dockerengine.ErrNotFound) {
			failures = errors.Join(failures, service.faultFromError(err, "remove network %s", network.Name))
		}
	}
	volumes, err := current.client.VolumeList(ctx, current.namespaceLabels())
	if err != nil {
		return errors.Join(failures, service.faultFromError(err, "list volumes"))
	}
	for _, volume := range volumes {
		if volume.Labels[labelPlugin] == "" || !match(volume.Labels) {
			continue
		}
		if err := current.client.VolumeRemove(ctx, volume.Name); err != nil && !errors.Is(err, dockerengine.ErrNotFound) {
			failures = errors.Join(failures, service.faultFromError(err, "remove volume %s", volume.Name))
		}
	}
	images, err := current.client.ImageList(ctx, current.namespaceLabels())
	if err != nil {
		return errors.Join(failures, service.faultFromError(err, "list images"))
	}
	for _, image := range images {
		if image.Labels[labelPlugin] == "" || !match(image.Labels) {
			continue
		}
		if err := current.client.ImageRemove(ctx, image.ID); err != nil {
			failures = errors.Join(failures, service.faultFromError(err, "remove image %s", image.ID))
		}
	}
	return failures
}

// Sweep removes namespace objects whose plugin is no longer installed with
// @require_api docker.
func (service *Service) Sweep(ctx context.Context) error {
	service.wiringMu.RLock()
	source := service.policies
	service.wiringMu.RUnlock()
	if source == nil {
		return nil
	}
	current, err := service.session(ctx)
	if err != nil {
		if fault, ok := AsFault(err); ok && fault.Reason == ReasonUnconfigured {
			return nil
		}
		return err
	}
	policies, err := source(ctx)
	if err != nil {
		return err
	}
	installed := map[string]bool{}
	for _, policy := range policies {
		installed[policy.PluginID] = true
	}
	return service.removeOwned(ctx, current, func(labels map[string]string) bool {
		return !installed[labels[labelPlugin]]
	})
}

// RunBackground runs the background work until ctx ends: the startup and
// hourly orphan sweep and the gateway event poll.
func (service *Service) RunBackground(ctx context.Context) {
	go func() {
		sweep := time.NewTimer(0)
		defer sweep.Stop()
		events := time.NewTicker(gatewayEventInterval)
		defer events.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-sweep.C:
				callContext, cancel := context.WithTimeout(ctx, backgroundCallTimeout)
				if err := service.Sweep(callContext); err != nil {
					service.logWarn("Docker sweep failed", "error", err)
				}
				cancel()
				sweep.Reset(sweepInterval)
			case <-events.C:
				callContext, cancel := context.WithTimeout(ctx, backgroundCallTimeout)
				service.pollGatewayEvents(callContext)
				cancel()
			}
		}
	}()
}

// pollGatewayEvents reads new gateway log lines of a live session and logs
// every blocked egress host. It never connects on its own.
func (service *Service) pollGatewayEvents(ctx context.Context) {
	service.sessionMu.Lock()
	current := service.current
	service.sessionMu.Unlock()
	if current == nil {
		return
	}
	labels := current.namespaceLabels()
	labels[labelRole] = roleGateway
	gateways, err := current.client.ContainerList(ctx, labels)
	if err != nil {
		service.logDebug("Docker gateway poll failed", "error", err)
		return
	}
	now := time.Now().Unix()
	for _, summary := range gateways {
		if summary.State != "running" {
			continue
		}
		service.gatewayLogMu.Lock()
		since, seen := service.gatewayLogSince[summary.ID]
		service.gatewayLogMu.Unlock()
		if !seen {
			since = now - int64(gatewayEventInterval.Seconds())
		}
		stdout, _, err := current.client.ContainerLogs(ctx, summary.ID, 0, since, false, maximumLogBytes)
		if err != nil && !errors.Is(err, dockerengine.ErrOutputLimit) {
			service.logDebug("Docker gateway log read failed", "plugin_id", summary.Labels[labelPlugin], "error", err)
			continue
		}
		service.gatewayLogMu.Lock()
		service.gatewayLogSince[summary.ID] = now
		service.gatewayLogMu.Unlock()
		scanner := bufio.NewScanner(strings.NewReader(stdout))
		for scanner.Scan() {
			var event dockergateway.Event
			if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Event != dockergateway.EventEgressBlocked {
				continue
			}
			service.logWarn("Docker egress blocked", "plugin_id", summary.Labels[labelPlugin], "code", "docker_egress_blocked", "host", event.Host, "reason", event.Reason)
		}
	}
}

// PluginView is the dashboard view of one Docker plugin.
type PluginView struct {
	Policy     Policy             `json:"policy"`
	Images     []ImageStatus      `json:"images"`
	Containers []Status           `json:"containers"`
	Error      string             `json:"error,omitempty"`
	ErrorStage string             `json:"error_stage,omitempty"`
	Connection ConnectionSettings `json:"connection"`
}

// View reads the connection and, when Docker is reachable, the images and
// containers of one plugin. A connection failure is part of the view, not
// an error.
func (service *Service) View(ctx context.Context, policy Policy) (PluginView, error) {
	connection, err := service.Connection(ctx)
	if err != nil {
		return PluginView{}, err
	}
	view := PluginView{Policy: policy, Images: []ImageStatus{}, Containers: []Status{}, Connection: connection}
	current, err := service.session(ctx)
	if err != nil {
		view.setError(err)
		return view, nil
	}
	images, err := service.imageStatuses(ctx, current, policy)
	if err != nil {
		view.setError(err)
		return view, nil
	}
	view.Images = images
	statuses, err := service.listStatuses(ctx, current, policy.PluginID)
	if err != nil {
		view.setError(err)
		return view, nil
	}
	view.Containers = statuses
	return view, nil
}

func (view *PluginView) setError(err error) {
	view.Error = err.Error()
	if fault, ok := AsFault(err); ok {
		view.ErrorStage = fault.Stage
	}
}

// Container actions the dashboard offers.
const (
	ActionStart  = "start"
	ActionStop   = "stop"
	ActionRemove = "remove"
)

// OperatorAction runs one dashboard action.
func (service *Service) OperatorAction(ctx context.Context, policy Policy, text, action string) error {
	switch action {
	case ActionStart:
		_, err := service.Start(ctx, policy, text)
		return err
	case ActionStop, ActionRemove:
	default:
		return specError("unknown action %q", action)
	}
	current, err := service.session(ctx)
	if err != nil {
		return err
	}
	if action == ActionStop {
		_, err = service.stopTarget(ctx, current, policy, text, containerActionTimeout)
		return err
	}
	return service.removeTarget(ctx, current, policy, text, false)
}
