package dockerhost

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/dockerengine"
)

// transportKey identifies one cached transport: a plugin, a target and one
// port. Recreating or removing the target drops it.
type transportKey struct {
	pluginID string
	target   string
	port     int
}

// Transport bounds of container HTTP clients.
const (
	containerIdleConnections = 8
	containerIdleTimeout     = 90 * time.Second
)

// dropTransports closes and forgets every cached transport matching keep.
func (service *Service) dropTransports(match func(transportKey) bool) {
	service.transportsMu.Lock()
	defer service.transportsMu.Unlock()
	for key, transport := range service.transports {
		if match(key) {
			transport.CloseIdleConnections()
			delete(service.transports, key)
		}
	}
}

// containerAddress returns the container address on its plugin network.
func (current *session) containerAddress(pluginID string, target target, details *dockerengine.ContainerDetails, port int) (string, error) {
	project := ""
	if target.service != "" {
		project = target.key
	}
	network := current.networkName(pluginID, project)
	attachment, ok := details.NetworkSettings.Networks[network]
	if !ok || attachment.IPAddress == "" {
		return "", newFault(ReasonNotRunning, nil, "container %s has no address on its plugin network", target)
	}
	return net.JoinHostPort(attachment.IPAddress, strconv.Itoa(port)), nil
}

// dialContainer opens one TCP stream to a container port: directly on the
// host network in route mode, through the gateway relay in relay mode.
func (service *Service) dialContainer(ctx context.Context, current *session, policy Policy, target target, details *dockerengine.ContainerDetails, port int) (net.Conn, error) {
	address, err := current.containerAddress(policy.PluginID, target, details, port)
	if err != nil {
		return nil, err
	}
	if current.dialMode == DialModeRoute {
		conn, err := current.client.Connector().DialHost(ctx, address)
		if err != nil {
			return nil, newFault(ReasonNotRunning, err, "dial %s port %d: %v", target, port, err)
		}
		return conn, nil
	}
	session, err := current.client.ExecAttach(ctx, current.gatewayName(policy.PluginID), dockerengine.ExecConfig{
		Cmd: []string{gatewayBinaryPath, "relay", address},
	})
	if err != nil {
		if errors.Is(err, dockerengine.ErrNotFound) || errors.Is(err, dockerengine.ErrConflict) {
			return nil, newFault(ReasonNotRunning, err, "gateway of this plugin is not running: start the container again")
		}
		return nil, service.faultFromError(err, "relay to %s port %d", target, port)
	}
	return session.Conn(), nil
}

// ContainerHTTP is an HTTP client bound to one container port. BaseURL
// names the container; the client's dialer ignores the host.
type ContainerHTTP struct {
	Client  *http.Client
	BaseURL string
}

// failingTransport answers every request with one fault.
type failingTransport struct{ fault *Fault }

func (transport failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, transport.fault
}

// UnavailableHTTP is the open_http client of a router without the Docker
// service: every request fails with unconfigured.
func UnavailableHTTP() ContainerHTTP {
	fault := newFault(ReasonUnconfigured, nil, "docker is not available in this router")
	return ContainerHTTP{Client: &http.Client{Transport: failingTransport{fault: fault}}, BaseURL: "http://unavailable"}
}

// OpenHTTP returns an HTTP client for one port of a container. It checks
// only the target syntax: the state and the declared ports are checked
// on every new connection, so a client opened before start works once the
// container runs, and a restart or recreation reaches the new instance.
// Connection failures carry a Fault.
func (service *Service) OpenHTTP(policy Policy, text string, port int, timeout time.Duration) (ContainerHTTP, error) {
	parsed, err := parseTarget(text)
	if err != nil {
		return ContainerHTTP{}, err
	}
	key := transportKey{pluginID: policy.PluginID, target: parsed.String(), port: port}
	service.transportsMu.Lock()
	transport, ok := service.transports[key]
	if !ok {
		transport = &http.Transport{
			Proxy: nil,
			DialContext: func(dialContext context.Context, _, _ string) (net.Conn, error) {
				live, err := service.session(dialContext)
				if err != nil {
					return nil, err
				}
				_, details, err := service.runningContainer(dialContext, live, policy, text)
				if err != nil {
					return nil, err
				}
				declared := strings.Split(details.Config.Labels[labelPorts], ",")
				if !slices.Contains(declared, strconv.Itoa(port)) {
					return nil, newFault(ReasonNotFound, nil, "port %d is not in the ports of %s", port, parsed)
				}
				return service.dialContainer(dialContext, live, policy, parsed, details, port)
			},
			MaxIdleConns:        containerIdleConnections,
			MaxIdleConnsPerHost: containerIdleConnections,
			IdleConnTimeout:     containerIdleTimeout,
			ForceAttemptHTTP2:   false,
		}
		service.transports[key] = transport
	}
	service.transportsMu.Unlock()
	host := parsed.key
	if parsed.service != "" {
		host = parsed.service
	}
	baseURL := "http://" + net.JoinHostPort(host, strconv.Itoa(port))
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if request.URL.Scheme != "http" || request.URL.Host != via[0].URL.Host {
				return fmt.Errorf("redirect to %s leaves the container", request.URL.Redacted())
			}
			return nil
		},
	}
	return ContainerHTTP{Client: client, BaseURL: baseURL}, nil
}
