package dockerhost

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/dockerengine"
)

// fakeNetwork and fakeContainer model the daemon state the gateway tests
// need. Like the real daemon, removing a network succeeds while only
// stopped containers are attached, and the stopped containers keep the
// endpoint of the removed network.
type fakeNetwork struct {
	id       string
	name     string
	internal bool
	labels   map[string]string
}

type fakeContainer struct {
	id        string
	name      string
	labels    map[string]string
	running   bool
	endpoints map[string]string // network ID to network name
}

type fakeDaemon struct {
	mu         sync.Mutex
	counter    int
	networks   map[string]*fakeNetwork
	containers map[string]*fakeContainer
}

// newFakeSession serves a fake daemon over loopback TCP and returns a
// service holding a session on it.
func newFakeSession(t *testing.T) (*Service, *session, *fakeDaemon) {
	t.Helper()
	daemon := &fakeDaemon{networks: map[string]*fakeNetwork{}, containers: map[string]*fakeContainer{}}
	server := httptest.NewServer(daemon)
	t.Cleanup(server.Close)
	endpoint, err := dockerengine.ParseEndpoint("tcp://" + strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	connector, err := dockerengine.NewConnector(dockerengine.Options{Endpoint: endpoint})
	if err != nil {
		t.Fatal(err)
	}
	client := dockerengine.NewClient(connector)
	t.Cleanup(func() { _ = client.Close() })
	current := &session{client: client, endpoint: endpoint, namespace: "test", gatewayImage: "gateway:test", dialMode: DialModeRelay}
	service := &Service{images: map[string]*imageTask{}, transports: map[transportKey]*http.Transport{}, gatewayLogSince: map[string]int64{}}
	service.current = current
	return service, current, daemon
}

func (daemon *fakeDaemon) nextID(prefix string) string {
	daemon.counter++
	return fmt.Sprintf("%s-%d", prefix, daemon.counter)
}

func (daemon *fakeDaemon) network(reference string) *fakeNetwork {
	for _, network := range daemon.networks {
		if network.id == reference || network.name == reference {
			return network
		}
	}
	return nil
}

func (daemon *fakeDaemon) container(reference string) *fakeContainer {
	for _, container := range daemon.containers {
		if container.id == reference || container.name == reference {
			return container
		}
	}
	return nil
}

func matchesLabels(labels map[string]string, filters string) bool {
	var parsed struct {
		Label []string `json:"label"`
	}
	if filters == "" || json.Unmarshal([]byte(filters), &parsed) != nil {
		return true
	}
	for _, pair := range parsed.Label {
		name, value, _ := strings.Cut(pair, "=")
		if labels[name] != value {
			return false
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeDaemonError(w http.ResponseWriter, status int, format string, arguments ...any) {
	writeJSON(w, status, map[string]string{"message": fmt.Sprintf(format, arguments...)})
}

func (daemon *fakeDaemon) inspectBody(container *fakeContainer) map[string]any {
	networks := map[string]any{}
	for networkID := range container.endpoints {
		networks[networkID] = map[string]string{"NetworkID": networkID}
	}
	return map[string]any{
		"Id":              container.id,
		"Name":            "/" + container.name,
		"State":           map[string]any{"Running": container.running},
		"Config":          map[string]any{"Labels": container.labels},
		"NetworkSettings": map[string]any{"Networks": networks},
	}
}

func (daemon *fakeDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/v"+dockerengine.APIVersion)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "images":
		writeJSON(w, http.StatusOK, map[string]string{"Id": "image-1"})
	case parts[0] == "networks":
		daemon.serveNetworks(w, r, parts)
	case parts[0] == "containers":
		daemon.serveContainers(w, r, parts)
	default:
		writeDaemonError(w, http.StatusNotImplemented, "fake daemon: %s %s", r.Method, path)
	}
}

func (daemon *fakeDaemon) serveNetworks(w http.ResponseWriter, r *http.Request, parts []string) {
	switch {
	case r.Method == http.MethodGet && len(parts) == 1:
		listed := []map[string]any{}
		for _, network := range daemon.networks {
			if matchesLabels(network.labels, r.URL.Query().Get("filters")) {
				listed = append(listed, map[string]any{"Id": network.id, "Name": network.name, "Labels": network.labels})
			}
		}
		writeJSON(w, http.StatusOK, listed)
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "create":
		var body struct {
			Name     string
			Internal bool
			Labels   map[string]string
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if daemon.network(body.Name) != nil {
			writeDaemonError(w, http.StatusConflict, "network %s exists", body.Name)
			return
		}
		network := &fakeNetwork{id: daemon.nextID("net"), name: body.Name, internal: body.Internal, labels: body.Labels}
		daemon.networks[network.id] = network
		writeJSON(w, http.StatusCreated, map[string]string{"Id": network.id})
	case r.Method == http.MethodGet && len(parts) == 2:
		network := daemon.network(parts[1])
		if network == nil {
			writeDaemonError(w, http.StatusNotFound, "network %s not found", parts[1])
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"Id": network.id, "Name": network.name, "Internal": network.internal, "Labels": network.labels})
	case r.Method == http.MethodDelete && len(parts) == 2:
		network := daemon.network(parts[1])
		if network == nil {
			writeDaemonError(w, http.StatusNotFound, "network %s not found", parts[1])
			return
		}
		for _, container := range daemon.containers {
			if _, attached := container.endpoints[network.id]; container.running && attached {
				writeDaemonError(w, http.StatusForbidden, "network %s has active endpoints", network.name)
				return
			}
		}
		delete(daemon.networks, network.id)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[2] == "connect":
		network := daemon.network(parts[1])
		if network == nil {
			writeDaemonError(w, http.StatusNotFound, "network %s not found", parts[1])
			return
		}
		var body struct{ Container string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		container := daemon.container(body.Container)
		if container == nil {
			writeDaemonError(w, http.StatusNotFound, "container %s not found", body.Container)
			return
		}
		container.endpoints[network.id] = network.name
		w.WriteHeader(http.StatusOK)
	default:
		writeDaemonError(w, http.StatusNotImplemented, "fake daemon networks: %s %v", r.Method, parts)
	}
}

func (daemon *fakeDaemon) serveContainers(w http.ResponseWriter, r *http.Request, parts []string) {
	switch {
	case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "json":
		listed := []map[string]any{}
		for _, container := range daemon.containers {
			if matchesLabels(container.labels, r.URL.Query().Get("filters")) {
				state := "created"
				if container.running {
					state = "running"
				}
				listed = append(listed, map[string]any{"Id": container.id, "Names": []string{"/" + container.name}, "State": state, "Labels": container.labels})
			}
		}
		writeJSON(w, http.StatusOK, listed)
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "create":
		var body struct {
			Labels     map[string]string
			HostConfig struct{ NetworkMode string }
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		name := r.URL.Query().Get("name")
		if daemon.container(name) != nil {
			writeDaemonError(w, http.StatusConflict, "container %s exists", name)
			return
		}
		container := &fakeContainer{id: daemon.nextID("container"), name: name, labels: body.Labels, endpoints: map[string]string{}}
		if network := daemon.network(body.HostConfig.NetworkMode); network != nil {
			container.endpoints[network.id] = network.name
		}
		daemon.containers[container.id] = container
		writeJSON(w, http.StatusCreated, map[string]string{"Id": container.id})
	case len(parts) == 2 && r.Method == http.MethodDelete:
		container := daemon.container(parts[1])
		if container == nil {
			writeDaemonError(w, http.StatusNotFound, "container %s not found", parts[1])
			return
		}
		delete(daemon.containers, container.id)
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 3:
		container := daemon.container(parts[1])
		if container == nil {
			writeDaemonError(w, http.StatusNotFound, "container %s not found", parts[1])
			return
		}
		daemon.serveContainerAction(w, r, container, parts[2])
	default:
		writeDaemonError(w, http.StatusNotImplemented, "fake daemon containers: %s %v", r.Method, parts)
	}
}

func (daemon *fakeDaemon) serveContainerAction(w http.ResponseWriter, r *http.Request, container *fakeContainer, action string) {
	switch {
	case r.Method == http.MethodGet && action == "json":
		writeJSON(w, http.StatusOK, daemon.inspectBody(container))
	case r.Method == http.MethodPost && action == "start":
		for networkID := range container.endpoints {
			if daemon.network(networkID) == nil {
				writeDaemonError(w, http.StatusNotFound, "failed to set up container networking: network %s not found", networkID)
				return
			}
		}
		container.running = true
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && action == "stop":
		container.running = false
		w.WriteHeader(http.StatusNoContent)
	default:
		writeDaemonError(w, http.StatusNotImplemented, "fake daemon container action: %s %s", r.Method, action)
	}
}
