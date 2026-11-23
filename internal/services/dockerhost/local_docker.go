//go:build !windows && !freebsd

package dockerhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/dockerengine"
)

// States of a local Docker socket.
const (
	LocalSocketReady            = "ready"
	LocalSocketPermissionDenied = "permission_denied"
	LocalSocketNotRunning       = "not_running"
	LocalSocketMissing          = "missing"
	LocalSocketUnreachable      = "unreachable"
)

// Sources of a local Docker socket: where discovery found it.
const (
	LocalSocketSourceDockerHost     = "docker_host"
	LocalSocketSourceContext        = "context"
	LocalSocketSourceSystem         = "system"
	LocalSocketSourceRootless       = "rootless"
	LocalSocketSourcePodman         = "podman"
	LocalSocketSourcePodmanRootless = "podman_rootless"
	LocalSocketSourceCustom         = "custom"
)

// LocalSocket is one Docker socket on the router machine and whether the
// router can use it. Commands fix the state when the router knows how.
type LocalSocket struct {
	Path     string `json:"path"`
	Source   string `json:"source" enums:"docker_host,context,system,rootless,podman,podman_rootless,custom"`
	Context  string `json:"context"`
	State    string `json:"state" enums:"ready,permission_denied,not_running,missing,unreachable"`
	Error    string `json:"error"`
	Commands string `json:"commands"`
}

// LocalDocker is what discovery found on the router machine. User is the
// account the router runs as. Commands start Docker when no socket exists.
type LocalDocker struct {
	User     string        `json:"user"`
	Sockets  []LocalSocket `json:"sockets"`
	Commands string        `json:"commands"`
}

// localSocketTimeout bounds one socket probe.
const localSocketTimeout = 3 * time.Second

// systemdDirectory exists while systemd is the init system.
const systemdDirectory = "/run/systemd/system"

type localSocketCandidate struct {
	path    string
	source  string
	context string
}

// LocalDocker finds the Docker sockets of the router machine and probes each
// one. Missing candidates are left out.
func (service *Service) LocalDocker(ctx context.Context) (LocalDocker, error) {
	account, err := user.Current()
	if err != nil {
		return LocalDocker{}, fmt.Errorf("resolve the router user: %w", err)
	}
	found := LocalDocker{User: account.Username, Sockets: []LocalSocket{}}
	for _, candidate := range service.localSocketCandidates() {
		socket := probeLocalSocket(ctx, candidate, account.Username)
		if socket.State != LocalSocketMissing {
			found.Sockets = append(found.Sockets, socket)
		}
	}
	if len(found.Sockets) == 0 {
		found.Commands = startCommand(LocalSocketSourceSystem)
	}
	return found, nil
}

// ProbeLocalSocket probes one socket path the user entered.
func (service *Service) ProbeLocalSocket(ctx context.Context, path string) (LocalSocket, error) {
	path = strings.TrimSpace(path)
	if !filepath.IsAbs(path) {
		return LocalSocket{}, fmt.Errorf("%w: socket path must be absolute", ErrInvalidSettings)
	}
	account, err := user.Current()
	if err != nil {
		return LocalSocket{}, fmt.Errorf("resolve the router user: %w", err)
	}
	return probeLocalSocket(ctx, localSocketCandidate{path: filepath.Clean(path), source: LocalSocketSourceCustom}, account.Username), nil
}

// localSocketCandidates lists the places a local daemon listens, most
// specific first: DOCKER_HOST, Docker contexts, then the well-known paths.
// Paths that resolve to the same file appear once.
func (service *Service) localSocketCandidates() []localSocketCandidate {
	runtimeDirectory := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDirectory == "" {
		runtimeDirectory = "/run/user/" + strconv.Itoa(os.Getuid())
	}
	var candidates []localSocketCandidate
	if path, ok := unixSocketPath(os.Getenv("DOCKER_HOST")); ok {
		candidates = append(candidates, localSocketCandidate{path: path, source: LocalSocketSourceDockerHost})
	}
	candidates = append(candidates, service.dockerContextSockets()...)
	candidates = append(candidates,
		localSocketCandidate{path: dockerengine.DefaultSocketPath, source: LocalSocketSourceSystem},
		localSocketCandidate{path: filepath.Join(runtimeDirectory, "docker.sock"), source: LocalSocketSourceRootless},
		localSocketCandidate{path: "/run/podman/podman.sock", source: LocalSocketSourcePodman},
		localSocketCandidate{path: filepath.Join(runtimeDirectory, "podman", "podman.sock"), source: LocalSocketSourcePodmanRootless},
	)
	seen := map[string]bool{}
	unique := candidates[:0]
	for _, candidate := range candidates {
		resolved, err := filepath.EvalSymlinks(candidate.path)
		if err != nil {
			resolved = candidate.path
		}
		if !seen[resolved] {
			seen[resolved] = true
			unique = append(unique, candidate)
		}
	}
	return unique
}

// dockerContextMeta is the part of a Docker CLI context file discovery reads.
type dockerContextMeta struct {
	Name      string `json:"Name"`
	Endpoints map[string]struct {
		Host string `json:"Host"`
	} `json:"Endpoints"`
}

// dockerContextSockets reads the unix sockets of the Docker CLI contexts of
// the router user. Docker Desktop, OrbStack, Colima and Rancher Desktop each
// register one. A context file that cannot be read is logged and skipped.
func (service *Service) dockerContextSockets() []localSocketCandidate {
	directory := os.Getenv("DOCKER_CONFIG")
	if directory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			service.logWarn("Docker contexts not read", "error", err)
			return nil
		}
		directory = filepath.Join(home, ".docker")
	}
	files, err := filepath.Glob(filepath.Join(directory, "contexts", "meta", "*", "meta.json"))
	if err != nil {
		service.logWarn("Docker contexts not read", "error", err)
		return nil
	}
	var candidates []localSocketCandidate
	for _, file := range files {
		content, err := os.ReadFile(file)
		var meta dockerContextMeta
		if err == nil {
			err = json.Unmarshal(content, &meta)
		}
		if err != nil {
			service.logWarn("Docker context skipped", "path", file, "error", err)
			continue
		}
		if path, ok := unixSocketPath(meta.Endpoints["docker"].Host); ok {
			candidates = append(candidates, localSocketCandidate{path: path, source: LocalSocketSourceContext, context: meta.Name})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].context < candidates[j].context })
	return candidates
}

// unixSocketPath returns the socket of a unix:// endpoint.
func unixSocketPath(raw string) (string, bool) {
	if !strings.HasPrefix(strings.TrimSpace(raw), "unix://") {
		return "", false
	}
	endpoint, err := dockerengine.ParseEndpoint(raw)
	if err != nil {
		return "", false
	}
	return endpoint.SocketPath, true
}

// probeLocalSocket pings Docker on one socket and names what stops the
// router from using it.
func probeLocalSocket(ctx context.Context, candidate localSocketCandidate, username string) LocalSocket {
	socket := LocalSocket{Path: candidate.path, Source: candidate.source, Context: candidate.context}
	info, err := os.Stat(candidate.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		socket.State = LocalSocketMissing
		return socket
	case errors.Is(err, fs.ErrPermission):
		socket.State = LocalSocketPermissionDenied
		return socket
	case err != nil:
		socket.State, socket.Error = LocalSocketUnreachable, err.Error()
		return socket
	case info.Mode().Type() != fs.ModeSocket:
		socket.State, socket.Error = LocalSocketUnreachable, candidate.path+" is not a socket"
		return socket
	}
	if err := pingLocalSocket(ctx, candidate.path); err != nil {
		switch {
		case errors.Is(err, fs.ErrPermission):
			socket.State, socket.Commands = LocalSocketPermissionDenied, accessCommand(info, username)
		case errors.Is(err, syscall.ECONNREFUSED):
			socket.State, socket.Commands = LocalSocketNotRunning, startCommand(candidate.source)
		default:
			socket.State, socket.Error = LocalSocketUnreachable, err.Error()
		}
		return socket
	}
	socket.State = LocalSocketReady
	return socket
}

// pingLocalSocket calls /_ping of the daemon on the socket.
func pingLocalSocket(ctx context.Context, path string) error {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", path)
		},
		DisableKeepAlives: true,
	}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, localSocketTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/_ping", nil)
	if err != nil {
		return err
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("docker answered HTTP %d on /_ping", response.StatusCode)
	}
	return nil
}

// accessCommand adds the router user to the group of the socket. It returns
// nothing for root-owned sockets: root group membership is not a fix.
func accessCommand(info fs.FileInfo, username string) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Gid == 0 {
		return ""
	}
	group, err := user.LookupGroupId(strconv.FormatUint(uint64(stat.Gid), 10))
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "linux":
		return "sudo usermod -aG " + group.Name + " " + username
	case "darwin":
		return "sudo dseditgroup -o edit -a " + username + " -t user " + group.Name
	}
	return ""
}

// startCommand starts the daemon behind a socket source on a systemd host.
func startCommand(source string) string {
	if runtime.GOOS != "linux" {
		return ""
	}
	if _, err := os.Stat(systemdDirectory); err != nil {
		return ""
	}
	switch source {
	case LocalSocketSourceSystem:
		return "sudo systemctl enable --now docker"
	case LocalSocketSourceRootless:
		return "systemctl --user enable --now docker"
	case LocalSocketSourcePodman:
		return "sudo systemctl enable --now podman.socket"
	case LocalSocketSourcePodmanRootless:
		return "systemctl --user enable --now podman.socket"
	}
	return ""
}
