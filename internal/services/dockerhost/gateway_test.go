package dockerhost

import (
	"context"
	"testing"
)

// A plugin container created before its network was removed is replaced by
// the next ensure and ends on the live network.
func TestEnsureReplacesContainerOnRemovedNetwork(t *testing.T) {
	ctx := context.Background()
	service, current, daemon := newFakeSession(t)
	policy := testPolicy(t)
	spec := ContainerSpec{Image: "node:20-alpine"}
	if _, err := service.Ensure(ctx, policy, "web", spec); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	networkName := current.networkName(policy.PluginID, "")
	oldNetwork, err := current.client.NetworkInspect(ctx, networkName)
	if err != nil {
		t.Fatalf("inspect network: %v", err)
	}
	if err := current.client.ContainerStop(ctx, current.gatewayName(policy.PluginID), 1); err != nil {
		t.Fatalf("stop gateway: %v", err)
	}
	if err := current.client.NetworkRemove(ctx, oldNetwork.ID); err != nil {
		t.Fatalf("remove network under the created container: %v", err)
	}

	if _, err := service.Ensure(ctx, policy, "web", spec); err != nil {
		t.Fatalf("ensure after network removal: %v", err)
	}
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	container := daemon.container(current.containerName(policy.PluginID, target{key: "web"}))
	if container == nil {
		t.Fatal("container is missing")
	}
	for networkID := range container.endpoints {
		if daemon.network(networkID) == nil {
			t.Fatalf("container endpoint points at removed network %s", networkID)
		}
	}
}

// A stopped gateway keeps the endpoint of a network that was removed under
// it: the daemon only refuses to remove a network with running endpoints. The
// next ensure replaces the gateway and keeps the proxy token.
func TestEnsureGatewayReplacesStaleNetworkAttachment(t *testing.T) {
	ctx := context.Background()
	service, current, daemon := newFakeSession(t)
	policy := testPolicy(t)
	networkName := current.networkName(policy.PluginID, "")
	networkLabels := current.ownerLabels(policy.PluginID, roleNetwork)

	oldNetwork, err := service.ensureNetwork(ctx, current, networkName, true, networkLabels)
	if err != nil {
		t.Fatalf("ensure network: %v", err)
	}
	first, err := service.ensureGateway(ctx, current, policy, oldNetwork)
	if err != nil {
		t.Fatalf("first gateway: %v", err)
	}
	if err := current.client.ContainerStop(ctx, first.id, 1); err != nil {
		t.Fatalf("stop gateway: %v", err)
	}
	if err := current.client.NetworkRemove(ctx, oldNetwork); err != nil {
		t.Fatalf("remove network under the stopped gateway: %v", err)
	}
	newNetwork, err := service.ensureNetwork(ctx, current, networkName, true, networkLabels)
	if err != nil {
		t.Fatalf("recreate network: %v", err)
	}

	second, err := service.ensureGateway(ctx, current, policy, newNetwork)
	if err != nil {
		t.Fatalf("gateway after network replacement: %v", err)
	}
	if second.token != first.token {
		t.Fatal("replacement changed the proxy token")
	}
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	gateway := daemon.container(current.gatewayName(policy.PluginID))
	if gateway == nil || !gateway.running {
		t.Fatalf("gateway is not running: %+v", gateway)
	}
	if len(gateway.endpoints) != 2 {
		t.Fatalf("gateway endpoints %v, want egress and plugin network", gateway.endpoints)
	}
	for networkID, name := range gateway.endpoints {
		if daemon.network(networkID) == nil {
			t.Fatalf("gateway endpoint %s points at removed network %s", name, networkID)
		}
	}
}
