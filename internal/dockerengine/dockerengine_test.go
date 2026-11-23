package dockerengine

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestParseEndpoint(t *testing.T) {
	cases := []struct {
		raw     string
		want    Endpoint
		wantErr bool
	}{
		{raw: "unix:///var/run/docker.sock", want: Endpoint{Kind: EndpointUnix, SocketPath: "/var/run/docker.sock"}},
		{raw: "tcp://127.0.0.1:2375", want: Endpoint{Kind: EndpointTCP, Address: "127.0.0.1:2375"}},
		{raw: "ssh://kys@192.168.178.29", want: Endpoint{Kind: EndpointSSH, Address: "192.168.178.29:22", SocketPath: DefaultSocketPath, User: "kys"}},
		{raw: "ssh://user@host:2222/run/user/1000/docker.sock", want: Endpoint{Kind: EndpointSSH, Address: "host:2222", SocketPath: "/run/user/1000/docker.sock", User: "user"}},
		{raw: "ssh://host", wantErr: true},
		{raw: "ssh://user:secret@host", wantErr: true},
		{raw: "tcp://host", wantErr: true},
		{raw: "tcp://host:99999", wantErr: true},
		{raw: "npipe:////./pipe/docker_engine", wantErr: true},
		{raw: "unix://relative/path", wantErr: true},
		{raw: "", wantErr: true},
	}
	for _, testCase := range cases {
		got, err := ParseEndpoint(testCase.raw)
		if testCase.wantErr {
			if !errors.Is(err, ErrInvalidEndpoint) {
				t.Errorf("ParseEndpoint(%q) error = %v, want ErrInvalidEndpoint", testCase.raw, err)
			}
			continue
		}
		if err != nil || got != testCase.want {
			t.Errorf("ParseEndpoint(%q) = %+v, %v, want %+v", testCase.raw, got, err, testCase.want)
		}
		if reparsed, err := ParseEndpoint(got.String()); err != nil || reparsed != got {
			t.Errorf("round trip of %q = %+v, %v", got.String(), reparsed, err)
		}
	}
}

func TestPlainTCPNeedsLoopback(t *testing.T) {
	remote, _ := ParseEndpoint("tcp://203.0.113.7:2375")
	if _, err := NewConnector(Options{Endpoint: remote}); !errors.Is(err, ErrPlainTCPNotLoopback) {
		t.Fatalf("remote plain tcp error = %v, want ErrPlainTCPNotLoopback", err)
	}
	local, _ := ParseEndpoint("tcp://localhost:2375")
	if _, err := NewConnector(Options{Endpoint: local}); err != nil {
		t.Fatalf("loopback plain tcp error = %v", err)
	}
	ssh, _ := ParseEndpoint("ssh://user@host")
	if _, err := NewConnector(Options{Endpoint: ssh}); !errors.Is(err, ErrHostKeyRequired) {
		t.Fatalf("ssh without host key error = %v, want ErrHostKeyRequired", err)
	}
}

func TestSplitReference(t *testing.T) {
	cases := map[string][2]string{
		"nginx":                                {"nginx", "latest"},
		"nginx:1.27-alpine":                    {"nginx", "1.27-alpine"},
		"registry:5000/team/app":               {"registry:5000/team/app", "latest"},
		"registry:5000/team/app:v2":            {"registry:5000/team/app", "v2"},
		"node:20-alpine@sha256:" + "ab12":      {"node:20-alpine", "sha256:ab12"},
		"ghcr.io/owner/image@sha256:" + "cd34": {"ghcr.io/owner/image", "sha256:cd34"},
	}
	for reference, want := range cases {
		repository, tag := SplitReference(reference)
		if repository != want[0] || tag != want[1] {
			t.Errorf("SplitReference(%q) = %q, %q, want %q, %q", reference, repository, tag, want[0], want[1])
		}
	}
}

func frame(stream byte, payload string) []byte {
	header := make([]byte, frameHeaderSize)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	return append(header, payload...)
}

func TestDemultiplex(t *testing.T) {
	var input bytes.Buffer
	input.Write(frame(streamStdout, "hello "))
	input.Write(frame(streamStderr, "warning"))
	input.Write(frame(streamStdout, "world"))
	stdout, stderr, err := Demultiplex(&input, 0)
	if err != nil || string(stdout) != "hello world" || string(stderr) != "warning" {
		t.Fatalf("Demultiplex = %q, %q, %v", stdout, stderr, err)
	}
	input.Reset()
	input.Write(frame(streamStdout, "0123456789"))
	stdout, _, err = Demultiplex(&input, 4)
	if !errors.Is(err, ErrOutputLimit) || string(stdout) != "0123" {
		t.Fatalf("capped Demultiplex = %q, %v", stdout, err)
	}
}

// fakeDaemon serves a few Engine API routes over loopback TCP.
func fakeDaemon(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	endpoint, err := ParseEndpoint("tcp://" + strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	connector, err := NewConnector(Options{Endpoint: endpoint})
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(connector)
	t.Cleanup(func() { client.Close() })
	return client
}

func TestClientErrorsAndFilters(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1.41/containers/json", func(w http.ResponseWriter, r *http.Request) {
		var filters map[string][]string
		if err := json.Unmarshal([]byte(r.URL.Query().Get("filters")), &filters); err != nil || len(filters["label"]) != 1 || filters["label"][0] != "io.llm-router.plugin=a/b" {
			http.Error(w, `{"message":"bad filters"}`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`[{"Id":"c1","Names":["/x"],"State":"running","Labels":{"io.llm-router.plugin":"a/b"}}]`))
	})
	mux.HandleFunc("GET /v1.41/containers/missing/json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No such container: missing"}`))
	})
	mux.HandleFunc("POST /v1.41/containers/c1/start", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	})
	mux.HandleFunc("POST /v1.41/images/create", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"Pulling"}` + "\n" + `{"errorDetail":{"message":"pull access denied"},"error":"pull access denied"}` + "\n"))
	})
	client := fakeDaemon(t, mux)
	ctx := context.Background()

	containers, err := client.ContainerList(ctx, map[string]string{"io.llm-router.plugin": "a/b"})
	if err != nil || len(containers) != 1 || containers[0].ID != "c1" {
		t.Fatalf("ContainerList = %+v, %v", containers, err)
	}
	_, err = client.ContainerInspect(ctx, "missing")
	var apiError *APIError
	if !errors.Is(err, ErrNotFound) || !errors.As(err, &apiError) || apiError.Message != "No such container: missing" {
		t.Fatalf("ContainerInspect error = %v, want ErrNotFound with message", err)
	}
	if err := client.ContainerStart(ctx, "c1"); err != nil {
		t.Fatalf("ContainerStart on a running container = %v, want nil", err)
	}
	var statuses []string
	err = client.ImagePull(ctx, "private/image:1", "", func(event ProgressEvent) { statuses = append(statuses, event.Status) })
	if err == nil || !strings.Contains(err.Error(), "pull access denied") {
		t.Fatalf("ImagePull error = %v, want the stream error", err)
	}
	if len(statuses) != 2 || statuses[0] != "Pulling" {
		t.Fatalf("progress events = %q, want both lines", statuses)
	}
}

// sshTestServer serves one SSH endpoint that accepts allowedClient and
// refuses every channel.
func sshTestServer(t *testing.T, allowedClient ssh.PublicKey) (string, ssh.Signer) {
	t.Helper()
	_, hostPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPrivate)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if bytes.Equal(key.Marshal(), allowedClient.Marshal()) {
			return &ssh.Permissions{}, nil
		}
		return nil, errors.New("key refused")
	}}
	config.AddHostKey(hostSigner)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				_, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					conn.Close()
					return
				}
				go ssh.DiscardRequests(requests)
				for channel := range channels {
					_ = channel.Reject(ssh.ConnectionFailed, "socket refused")
				}
			}()
		}
	}()
	return listener.Addr().String(), hostSigner
}

func clientKey(t *testing.T) (ssh.Signer, []byte) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer, pem.EncodeToMemory(block)
}

func TestConnectionStages(t *testing.T) {
	allowed, allowedPEM := clientKey(t)
	_, otherPEM := clientKey(t)
	address, hostSigner := sshTestServer(t, allowed.PublicKey())
	hostKey := string(ssh.MarshalAuthorizedKey(hostSigner.PublicKey()))
	wrongHost, _ := clientKey(t)
	wrongHostKey := string(ssh.MarshalAuthorizedKey(wrongHost.PublicKey()))
	endpoint, err := ParseEndpoint("ssh://user@" + address)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddress := closed.Addr().String()
	closed.Close()
	unreachable, _ := ParseEndpoint("ssh://user@" + closedAddress)

	cases := []struct {
		name     string
		endpoint Endpoint
		key      []byte
		hostKey  string
		stage    string
	}{
		{name: "nothing listens", endpoint: unreachable, key: allowedPEM, hostKey: hostKey, stage: StageConnect},
		{name: "host key changed", endpoint: endpoint, key: allowedPEM, hostKey: wrongHostKey, stage: StageHostKey},
		{name: "client key refused", endpoint: endpoint, key: otherPEM, hostKey: hostKey, stage: StageAuthenticate},
		{name: "socket channel refused", endpoint: endpoint, key: allowedPEM, hostKey: hostKey, stage: StageSocket},
	}
	for _, testCase := range cases {
		connector, err := NewConnector(Options{Endpoint: testCase.endpoint, SSH: SSHMaterial{PrivateKey: testCase.key, HostKey: testCase.hostKey}})
		if err != nil {
			t.Fatalf("%s: NewConnector = %v", testCase.name, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err = connector.DialDaemon(ctx)
		cancel()
		connector.Close()
		var connectionError *ConnectionError
		if !errors.As(err, &connectionError) || connectionError.Stage != testCase.stage {
			t.Errorf("%s: error = %v, want stage %s", testCase.name, err, testCase.stage)
		}
	}
}

func TestExecRunOverHijackedConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveExec(conn)
		}
	}()
	endpoint, _ := ParseEndpoint("tcp://" + listener.Addr().String())
	connector, err := NewConnector(Options{Endpoint: endpoint})
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(connector)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := client.ExecRun(ctx, "c1", ExecConfig{Cmd: []string{"cat"}}, []byte("ping"), 0)
	if err != nil {
		t.Fatalf("ExecRun error = %v", err)
	}
	if string(result.Stdout) != "ping" || string(result.Stderr) != "done" || result.ExitCode != 3 {
		t.Fatalf("ExecRun = %+v", result)
	}
}

// serveExec answers one connection of the exec flow: create, hijacked
// start echoing stdin to stdout, inspect.
func serveExec(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		request, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		body, _ := io.ReadAll(request.Body)
		switch {
		case request.URL.Path == "/v1.41/containers/c1/exec":
			var config ExecConfig
			_ = json.Unmarshal(body, &config)
			writeResponse(conn, http.StatusCreated, `{"Id":"e1"}`)
		case request.URL.Path == "/v1.41/exec/e1/start":
			_, _ = io.WriteString(conn, "HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
			stdin, _ := io.ReadAll(reader)
			_, _ = conn.Write(frame(streamStdout, string(stdin)))
			_, _ = conn.Write(frame(streamStderr, "done"))
			return
		case request.URL.Path == "/v1.41/exec/e1/json":
			writeResponse(conn, http.StatusOK, `{"Running":false,"ExitCode":3}`)
		default:
			writeResponse(conn, http.StatusNotFound, `{"message":"unexpected"}`)
		}
	}
}

func writeResponse(conn net.Conn, status int, body string) {
	response := &http.Response{
		StatusCode: status, ProtoMajor: 1, ProtoMinor: 1,
		Header:        http.Header{"Content-Type": {"application/json"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
	_ = response.Write(conn)
}
