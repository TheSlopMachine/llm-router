// Package dockerengine is a minimal Docker Engine API client. It speaks the
// HTTP API directly over a UNIX socket, TCP (TLS outside loopback) or an SSH
// connection to the daemon host. It never runs the Docker CLI and holds no
// knowledge of plugins: the dockerhost service owns policy.
package dockerengine

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// EndpointKind names the daemon transport.
type EndpointKind string

const (
	EndpointUnix EndpointKind = "unix"
	EndpointTCP  EndpointKind = "tcp"
	EndpointSSH  EndpointKind = "ssh"
)

// DefaultSocketPath is the daemon socket on Linux hosts.
const DefaultSocketPath = "/var/run/docker.sock"

// ErrInvalidEndpoint reports an endpoint string the client cannot use.
var ErrInvalidEndpoint = errors.New("invalid docker endpoint")

// Endpoint is one parsed daemon address.
type Endpoint struct {
	Kind EndpointKind
	// Address is host:port for tcp and ssh endpoints.
	Address string
	// SocketPath is the local socket (unix) or the remote socket (ssh).
	SocketPath string
	// User is the SSH login name.
	User string
}

// ParseEndpoint parses unix:///path, tcp://host:port and
// ssh://user@host[:port][/socket/path].
func ParseEndpoint(raw string) (Endpoint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Endpoint{}, fmt.Errorf("%w: empty endpoint", ErrInvalidEndpoint)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return Endpoint{}, fmt.Errorf("%w: %v", ErrInvalidEndpoint, err)
	}
	switch EndpointKind(parsed.Scheme) {
	case EndpointUnix:
		path := parsed.Path
		if parsed.Host != "" {
			return Endpoint{}, fmt.Errorf("%w: unix endpoint takes an absolute path (unix:///var/run/docker.sock)", ErrInvalidEndpoint)
		}
		if !strings.HasPrefix(path, "/") {
			return Endpoint{}, fmt.Errorf("%w: unix socket path must be absolute", ErrInvalidEndpoint)
		}
		return Endpoint{Kind: EndpointUnix, SocketPath: path}, nil
	case EndpointTCP:
		if parsed.Port() == "" || parsed.Hostname() == "" {
			return Endpoint{}, fmt.Errorf("%w: tcp endpoint needs host and port (tcp://host:2376)", ErrInvalidEndpoint)
		}
		if parsed.Path != "" && parsed.Path != "/" {
			return Endpoint{}, fmt.Errorf("%w: tcp endpoint takes no path", ErrInvalidEndpoint)
		}
		if err := validPort(parsed.Port()); err != nil {
			return Endpoint{}, err
		}
		return Endpoint{Kind: EndpointTCP, Address: net.JoinHostPort(parsed.Hostname(), parsed.Port())}, nil
	case EndpointSSH:
		if parsed.User == nil || parsed.User.Username() == "" {
			return Endpoint{}, fmt.Errorf("%w: ssh endpoint needs a user (ssh://user@host)", ErrInvalidEndpoint)
		}
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return Endpoint{}, fmt.Errorf("%w: ssh endpoint must not carry a password", ErrInvalidEndpoint)
		}
		if parsed.Hostname() == "" {
			return Endpoint{}, fmt.Errorf("%w: ssh endpoint needs a host", ErrInvalidEndpoint)
		}
		port := parsed.Port()
		if port == "" {
			port = "22"
		}
		if err := validPort(port); err != nil {
			return Endpoint{}, err
		}
		socketPath := parsed.Path
		if socketPath == "" || socketPath == "/" {
			socketPath = DefaultSocketPath
		}
		return Endpoint{
			Kind:       EndpointSSH,
			Address:    net.JoinHostPort(parsed.Hostname(), port),
			SocketPath: socketPath,
			User:       parsed.User.Username(),
		}, nil
	default:
		return Endpoint{}, fmt.Errorf("%w: scheme %q is not unix, tcp or ssh", ErrInvalidEndpoint, parsed.Scheme)
	}
}

func validPort(port string) error {
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("%w: port %q is out of range", ErrInvalidEndpoint, port)
	}
	return nil
}

// String renders the endpoint in its canonical form. It carries no secret.
func (endpoint Endpoint) String() string {
	switch endpoint.Kind {
	case EndpointUnix:
		return "unix://" + endpoint.SocketPath
	case EndpointTCP:
		return "tcp://" + endpoint.Address
	case EndpointSSH:
		return "ssh://" + endpoint.User + "@" + endpoint.Address + endpoint.SocketPath
	default:
		return ""
	}
}

// Loopback reports whether a tcp endpoint targets the local machine.
func (endpoint Endpoint) Loopback() bool {
	host, _, err := net.SplitHostPort(endpoint.Address)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
