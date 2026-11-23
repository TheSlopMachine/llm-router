//go:build !windows && !freebsd

package dockerhost

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// shortTempDir keeps socket paths under the 104-byte limit of macOS.
func shortTempDir(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "ds")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	return directory
}

// serveSocket answers /_ping with the status on a unix socket.
func serveSocket(t *testing.T, path string, status int) *net.UnixListener {
	t.Helper()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_ping" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return listener
}

func TestProbeLocalSocket(t *testing.T) {
	directory := shortTempDir(t)
	ready := filepath.Join(directory, "ready.sock")
	serveSocket(t, ready, http.StatusOK)
	failing := filepath.Join(directory, "failing.sock")
	serveSocket(t, failing, http.StatusInternalServerError)
	stopped := filepath.Join(directory, "stopped.sock")
	listener := serveSocket(t, stopped, http.StatusOK)
	listener.SetUnlinkOnClose(false)
	listener.Close()
	plain := filepath.Join(directory, "plain")
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		path  string
		state string
	}{
		{name: "answers", path: ready, state: LocalSocketReady},
		{name: "error status", path: failing, state: LocalSocketUnreachable},
		{name: "nobody listens", path: stopped, state: LocalSocketNotRunning},
		{name: "no file", path: filepath.Join(directory, "none.sock"), state: LocalSocketMissing},
		{name: "not a socket", path: plain, state: LocalSocketUnreachable},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			socket := probeLocalSocket(context.Background(), localSocketCandidate{path: testCase.path, source: LocalSocketSourceCustom}, "router")
			if socket.State != testCase.state {
				t.Fatalf("state = %q (%s), want %q", socket.State, socket.Error, testCase.state)
			}
		})
	}
}

func TestProbeLocalSocketPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens every socket")
	}
	path := filepath.Join(shortTempDir(t), "locked.sock")
	serveSocket(t, path, http.StatusOK)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	socket := probeLocalSocket(context.Background(), localSocketCandidate{path: path, source: LocalSocketSourceCustom}, "router")
	if socket.State != LocalSocketPermissionDenied {
		t.Fatalf("state = %q (%s), want permission_denied", socket.State, socket.Error)
	}
}

func TestLocalSocketCandidates(t *testing.T) {
	directory := shortTempDir(t)
	colima := filepath.Join(directory, "colima.sock")
	serveSocket(t, colima, http.StatusOK)
	linked := filepath.Join(directory, "linked.sock")
	if err := os.Symlink(colima, linked); err != nil {
		t.Fatal(err)
	}
	contexts := filepath.Join(directory, "contexts", "meta")
	for name, content := range map[string]string{
		"a": `{"Name":"colima","Endpoints":{"docker":{"Host":"unix://` + colima + `"}}}`,
		"b": `{"Name":"remote","Endpoints":{"docker":{"Host":"ssh://user@host"}}}`,
		"c": `not json`,
		"d": `{"Name":"orbstack","Endpoints":{"docker":{"Host":"unix://` + filepath.Join(directory, "orbstack.sock") + `"}}}`,
	} {
		if err := os.MkdirAll(filepath.Join(contexts, name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(contexts, name, "meta.json"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DOCKER_CONFIG", directory)
	t.Setenv("DOCKER_HOST", "unix://"+linked)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(directory, "runtime"))

	service := &Service{}
	candidates := service.localSocketCandidates()
	if candidates[0].path != linked || candidates[0].source != LocalSocketSourceDockerHost {
		t.Fatalf("first candidate = %+v, want DOCKER_HOST", candidates[0])
	}
	var contextNames []string
	for _, candidate := range candidates[1:] {
		if candidate.source == LocalSocketSourceContext {
			contextNames = append(contextNames, candidate.context)
		}
	}
	if len(contextNames) != 1 || contextNames[0] != "orbstack" {
		t.Fatalf("contexts = %v, want only orbstack: colima repeats DOCKER_HOST, remote is not unix", contextNames)
	}
	found, err := service.LocalDocker(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Sockets) == 0 || found.Sockets[0].State != LocalSocketReady {
		t.Fatalf("sockets = %+v, want the DOCKER_HOST socket ready first", found.Sockets)
	}
	for _, socket := range found.Sockets {
		if socket.State == LocalSocketMissing {
			t.Fatalf("missing socket listed: %+v", socket)
		}
	}
}

func TestProbeLocalSocketRelativePath(t *testing.T) {
	if _, err := (&Service{}).ProbeLocalSocket(context.Background(), "docker.sock"); err == nil {
		t.Fatal("relative path accepted")
	}
}
