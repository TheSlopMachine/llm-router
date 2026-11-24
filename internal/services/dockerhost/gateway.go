package dockerhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/dockerengine"
	"github.com/TheSlopMachine/llm-router/internal/dockergateway"
)

// Gateway container limits: it only forwards bytes.
const (
	gatewayMemoryBytes = 64 << 20
	gatewayNanoCPUs    = 500_000_000
	gatewayPidsLimit   = 64
)

// gateway is the live gateway of one plugin.
type gateway struct {
	id    string
	token string
}

// proxyURL is the proxy address plugin containers use.
func (gateway gateway) proxyURL() string {
	return "http://" + dockergateway.ProxyUser + ":" + gateway.token + "@" + gatewayAlias + ":" + gatewayProxyPort
}

// ensureNetwork returns the ID of a router-owned network, creating it when
// missing. A network of that name without our labels is an ownership
// conflict.
func (service *Service) ensureNetwork(ctx context.Context, current *session, name string, internal bool, labels map[string]string) (string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		details, err := current.client.NetworkInspect(ctx, name)
		if err == nil {
			if details.Labels[labelNamespace] != current.namespace || details.Labels[labelPlugin] != labels[labelPlugin] {
				return "", newFault(ReasonOwnershipConflict, nil, "network %s exists and is not owned by this router", name)
			}
			return details.ID, nil
		}
		if !errors.Is(err, dockerengine.ErrNotFound) {
			return "", service.faultFromError(err, "inspect network %s", name)
		}
		id, err := current.client.NetworkCreate(ctx, name, internal, labels)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, dockerengine.ErrConflict) {
			return "", service.faultFromError(err, "create network %s", name)
		}
	}
	return "", newFault(ReasonDaemonError, nil, "network %s: concurrent creation did not settle", name)
}

// holdsRemovedNetwork reports whether a container keeps an endpoint of a
// network that no longer exists. The daemon removes a network under a
// stopped container and the container then fails to start. Only a
// replacement repairs it. A running container holds only live networks.
func (service *Service) holdsRemovedNetwork(ctx context.Context, current *session, details dockerengine.ContainerDetails) (bool, error) {
	if details.State.Running {
		return false, nil
	}
	networks, err := current.client.NetworkList(ctx, current.namespaceLabels())
	if err != nil {
		return false, service.faultFromError(err, "list networks")
	}
	live := map[string]bool{}
	for _, network := range networks {
		live[network.ID] = true
	}
	for _, attachment := range details.NetworkSettings.Networks {
		if attachment.NetworkID != "" && !live[attachment.NetworkID] {
			return true, nil
		}
	}
	return false, nil
}

// gatewayToken returns a fresh proxy password.
func gatewayToken() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// ensureGateway reconciles the plugin gateway: the right image and
// allow-list, running, attached to every network in networkIDs. Callers
// hold a target lock of the plugin.
func (service *Service) ensureGateway(ctx context.Context, current *session, policy Policy, networkIDs ...string) (gateway, error) {
	gatewayLock := service.targetLock(policy.PluginID, "\x00gateway")
	gatewayLock.Lock()
	defer gatewayLock.Unlock()
	image, err := service.ensureGatewayImage(ctx, current)
	if err != nil {
		return gateway{}, err
	}
	egressLabels := map[string]string{labelNamespace: current.namespace, labelRole: roleEgress}
	egressID, err := service.ensureNetwork(ctx, current, current.egressNetworkName(), false, egressLabels)
	if err != nil {
		return gateway{}, err
	}
	hosts := append([]string(nil), policy.AllowHosts...)
	sort.Strings(hosts)
	specHash := shortHash(16, image, strings.Join(hosts, ","))
	name := current.gatewayName(policy.PluginID)

	token := ""
	details, err := current.client.ContainerInspect(ctx, name)
	switch {
	case err == nil:
		if details.Config.Labels[labelNamespace] != current.namespace || details.Config.Labels[labelPlugin] != policy.PluginID || details.Config.Labels[labelRole] != roleGateway {
			return gateway{}, newFault(ReasonOwnershipConflict, nil, "container %s exists and is not owned by this router", name)
		}
		token = details.Config.Labels[labelProxyToken]
		replace := details.Config.Labels[labelSpec] != specHash
		if !replace {
			if replace, err = service.holdsRemovedNetwork(ctx, current, details); err != nil {
				return gateway{}, err
			}
		}
		if replace {
			if err := current.client.ContainerRemove(ctx, details.ID); err != nil && !errors.Is(err, dockerengine.ErrNotFound) {
				return gateway{}, service.faultFromError(err, "replace gateway %s", name)
			}
			details = dockerengine.ContainerDetails{}
		}
	case errors.Is(err, dockerengine.ErrNotFound):
	default:
		return gateway{}, service.faultFromError(err, "inspect gateway %s", name)
	}
	if token == "" {
		if token, err = gatewayToken(); err != nil {
			return gateway{}, newFault(ReasonDaemonError, err, "generate gateway token: %v", err)
		}
	}
	if details.ID == "" {
		labels := current.ownerLabels(policy.PluginID, roleGateway)
		labels[labelSpec] = specHash
		labels[labelProxyToken] = token
		config := dockerengine.ContainerConfig{
			Image: image,
			Cmd:   []string{"proxy"},
			Env: []string{
				dockergateway.EnvironmentAllowHosts + "=" + strings.Join(hosts, ","),
				dockergateway.EnvironmentProxyToken + "=" + token,
				dockergateway.EnvironmentListen + "=:" + gatewayProxyPort,
			},
			Labels: labels,
			HostConfig: dockerengine.HostConfig{
				Memory:        gatewayMemoryBytes,
				MemorySwap:    gatewayMemoryBytes,
				NanoCPUs:      gatewayNanoCPUs,
				PidsLimit:     gatewayPidsLimit,
				CapDrop:       []string{"ALL"},
				SecurityOpt:   []string{"no-new-privileges:true"},
				NetworkMode:   current.egressNetworkName(),
				RestartPolicy: dockerengine.RestartPolicy{Name: "no"},
				LogConfig:     containerLogConfig(),
			},
		}
		id, err := current.client.ContainerCreate(ctx, name, config)
		if err != nil {
			return gateway{}, service.faultFromError(err, "create gateway %s", name)
		}
		details, err = current.client.ContainerInspect(ctx, id)
		if err != nil {
			return gateway{}, service.faultFromError(err, "inspect gateway %s", name)
		}
		service.logInfo("Docker gateway created", "plugin_id", policy.PluginID, "image", image)
	}
	attached := map[string]bool{}
	for _, network := range details.NetworkSettings.Networks {
		attached[network.NetworkID] = true
	}
	if !attached[egressID] {
		if err := current.client.NetworkConnect(ctx, egressID, details.ID, nil); err != nil {
			return gateway{}, service.faultFromError(err, "attach gateway to egress network")
		}
	}
	for _, networkID := range networkIDs {
		if attached[networkID] {
			continue
		}
		if err := current.client.NetworkConnect(ctx, networkID, details.ID, []string{gatewayAlias}); err != nil {
			return gateway{}, service.faultFromError(err, "attach gateway to plugin network")
		}
	}
	if !details.State.Running {
		if err := current.client.ContainerStart(ctx, details.ID); err != nil {
			return gateway{}, service.faultFromError(err, "start gateway %s", name)
		}
	}
	return gateway{id: details.ID, token: token}, nil
}

// containerLogConfig caps container logs so a chatty container cannot fill
// the host disk.
func containerLogConfig() dockerengine.LogConfig {
	return dockerengine.LogConfig{Type: "json-file", Config: map[string]string{"max-size": "10m", "max-file": "3"}}
}
