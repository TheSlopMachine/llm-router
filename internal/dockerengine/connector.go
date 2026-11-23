package dockerengine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Transport errors.
var (
	// ErrHostKeyRequired reports an ssh endpoint without a pinned host key.
	ErrHostKeyRequired = errors.New("ssh host key is not pinned")
	// ErrPlainTCPNotLoopback reports a tcp endpoint without TLS outside
	// loopback.
	ErrPlainTCPNotLoopback = errors.New("plain tcp is allowed on loopback only: configure TLS")
	// ErrPrivateKeyRequired reports an ssh endpoint without a private key.
	ErrPrivateKeyRequired = errors.New("ssh private key is not configured")
)

// Connection stages: where a daemon connection failed.
const (
	StageConnect      = "connect"
	StageHandshake    = "handshake"
	StageHostKey      = "host_key"
	StageAuthenticate = "authenticate"
	StageSocket       = "socket"
	StageTLS          = "tls"
	// StageLocalSocket is the unix socket on the router machine. StageSocket
	// is the daemon socket behind SSH.
	StageLocalSocket = "local_socket"
)

// ConnectionError is a daemon connection failure with the stage it
// happened in, so callers can tell the operator what to fix.
type ConnectionError struct {
	Stage string
	Err   error
}

func (e *ConnectionError) Error() string {
	switch e.Stage {
	case StageConnect:
		return "cannot reach the host: " + e.Err.Error()
	case StageHandshake:
		return "ssh handshake failed: " + e.Err.Error()
	case StageHostKey:
		return "the host key does not match the pinned key: " + e.Err.Error()
	case StageAuthenticate:
		return "the host refused the ssh key: " + e.Err.Error()
	case StageSocket, StageLocalSocket:
		return "cannot open the docker socket: " + e.Err.Error()
	case StageTLS:
		return "tls handshake failed: " + e.Err.Error()
	default:
		return e.Err.Error()
	}
}

func (e *ConnectionError) Unwrap() error { return e.Err }

func stageError(stage string, err error) error {
	if err == nil {
		return nil
	}
	var existing *ConnectionError
	if errors.As(err, &existing) {
		return err
	}
	return &ConnectionError{Stage: stage, Err: err}
}

// dialTimeout bounds one transport dial, including the SSH handshake.
const dialTimeout = 15 * time.Second

// sshKeepaliveInterval is the interval of keepalive requests on the shared
// SSH connection. A failed keepalive drops the connection; the next call
// reconnects.
const sshKeepaliveInterval = 30 * time.Second

// TLSMaterial holds PEM blocks for a tcp endpoint. An empty CA uses the
// system roots; certificate and key go together or not at all.
type TLSMaterial struct {
	CA          []byte
	Certificate []byte
	Key         []byte
}

// SSHMaterial holds the client key and the pinned server key.
type SSHMaterial struct {
	PrivateKey []byte
	Passphrase []byte
	// HostKey is the server public key in authorized_keys form.
	HostKey string
}

// Options configures one Connector.
type Options struct {
	Endpoint Endpoint
	TLS      TLSMaterial
	SSH      SSHMaterial
}

// Connector opens raw connections to the daemon socket and, for route-mode
// container dials, to addresses on the daemon host network.
type Connector struct {
	endpoint  Endpoint
	tlsConfig *tls.Config
	sshConfig *ssh.ClientConfig

	mu        sync.Mutex
	sshClient *ssh.Client
	closed    bool
	stop      chan struct{}
}

// NewConnector validates options and builds a Connector. It does not dial.
func NewConnector(options Options) (*Connector, error) {
	connector := &Connector{endpoint: options.Endpoint, stop: make(chan struct{})}
	switch options.Endpoint.Kind {
	case EndpointUnix:
	case EndpointTCP:
		if len(options.TLS.CA) == 0 && len(options.TLS.Certificate) == 0 {
			if !options.Endpoint.Loopback() {
				return nil, ErrPlainTCPNotLoopback
			}
			break
		}
		config, err := buildTLSConfig(options.Endpoint, options.TLS)
		if err != nil {
			return nil, err
		}
		connector.tlsConfig = config
	case EndpointSSH:
		config, err := buildSSHConfig(options.Endpoint, options.SSH)
		if err != nil {
			return nil, err
		}
		connector.sshConfig = config
	default:
		return nil, fmt.Errorf("%w: unknown kind %q", ErrInvalidEndpoint, options.Endpoint.Kind)
	}
	return connector, nil
}

// Endpoint returns the configured daemon endpoint.
func (connector *Connector) Endpoint() Endpoint { return connector.endpoint }

func buildTLSConfig(endpoint Endpoint, material TLSMaterial) (*tls.Config, error) {
	host, _, err := net.SplitHostPort(endpoint.Address)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEndpoint, err)
	}
	config := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if len(material.CA) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(material.CA) {
			return nil, errors.New("tls CA: no PEM certificate found")
		}
		config.RootCAs = pool
	}
	if (len(material.Certificate) > 0) != (len(material.Key) > 0) {
		return nil, errors.New("tls client certificate and key must be set together")
	}
	if len(material.Certificate) > 0 {
		certificate, err := tls.X509KeyPair(material.Certificate, material.Key)
		if err != nil {
			return nil, fmt.Errorf("tls client certificate: %w", err)
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	return config, nil
}

// ParseHostKey parses an authorized_keys line or a bare "type base64" key.
func ParseHostKey(text string) (ssh.PublicKey, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrHostKeyRequired
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("parse ssh host key: %w", err)
	}
	return key, nil
}

func buildSSHConfig(endpoint Endpoint, material SSHMaterial) (*ssh.ClientConfig, error) {
	hostKey, err := ParseHostKey(material.HostKey)
	if err != nil {
		return nil, err
	}
	if len(material.PrivateKey) == 0 {
		return nil, ErrPrivateKeyRequired
	}
	var signer ssh.Signer
	if len(material.Passphrase) > 0 {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(material.PrivateKey, material.Passphrase)
	} else {
		signer, err = ssh.ParsePrivateKey(material.PrivateKey)
	}
	if err != nil {
		return nil, fmt.Errorf("parse ssh private key: %w", err)
	}
	return &ssh.ClientConfig{
		User:              endpoint.User,
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback:   ssh.FixedHostKey(hostKey),
		HostKeyAlgorithms: hostKeyAlgorithms(hostKey),
		Timeout:           dialTimeout,
	}, nil
}

// hostKeyAlgorithms asks the server for the pinned key type, so a host with
// several keys presents the pinned one. RSA keys sign with SHA-2.
func hostKeyAlgorithms(key ssh.PublicKey) []string {
	if key.Type() == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	}
	return []string{key.Type()}
}

// DialDaemon opens one connection to the daemon API socket.
func (connector *Connector) DialDaemon(ctx context.Context) (net.Conn, error) {
	switch connector.endpoint.Kind {
	case EndpointUnix:
		dialer := net.Dialer{Timeout: dialTimeout}
		conn, err := dialer.DialContext(ctx, "unix", connector.endpoint.SocketPath)
		return conn, stageError(StageLocalSocket, err)
	case EndpointTCP:
		dialer := net.Dialer{Timeout: dialTimeout}
		conn, err := dialer.DialContext(ctx, "tcp", connector.endpoint.Address)
		if err != nil {
			return nil, stageError(StageConnect, err)
		}
		if connector.tlsConfig == nil {
			return conn, nil
		}
		tlsConn := tls.Client(conn, connector.tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, stageError(StageTLS, err)
		}
		return tlsConn, nil
	case EndpointSSH:
		conn, err := connector.dialSSH(ctx, "unix", connector.endpoint.SocketPath)
		return conn, stageError(StageSocket, err)
	default:
		return nil, ErrInvalidEndpoint
	}
}

// DialHost opens a TCP connection to address as seen from the daemon host:
// a local dial for unix and tcp endpoints, an SSH direct-tcpip channel for
// ssh endpoints.
func (connector *Connector) DialHost(ctx context.Context, address string) (net.Conn, error) {
	if connector.endpoint.Kind == EndpointSSH {
		return connector.dialSSH(ctx, "tcp", address)
	}
	dialer := net.Dialer{Timeout: dialTimeout}
	return dialer.DialContext(ctx, "tcp", address)
}

// dialSSH opens one channel over the shared SSH connection. A failure on an
// existing connection drops it and retries once on a fresh one.
func (connector *Connector) dialSSH(ctx context.Context, network, address string) (net.Conn, error) {
	for attempt := 0; attempt < 2; attempt++ {
		client, fresh, err := connector.sshConnection(ctx)
		if err != nil {
			return nil, err
		}
		conn, err := client.DialContext(ctx, network, address)
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var openError *ssh.OpenChannelError
		if fresh || errors.As(err, &openError) {
			// The server answered: the target refused, not the connection.
			return nil, fmt.Errorf("ssh dial %s %s: %w", network, address, err)
		}
		connector.dropSSH(client)
	}
	return nil, fmt.Errorf("ssh dial %s %s: connection lost", network, address)
}

// sshConnection returns the shared client, connecting when needed. fresh
// reports a connection made by this call.
func (connector *Connector) sshConnection(ctx context.Context) (*ssh.Client, bool, error) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	if connector.closed {
		return nil, false, errors.New("docker connector is closed")
	}
	if connector.sshClient != nil {
		return connector.sshClient, false, nil
	}
	client, err := dialSSHClient(ctx, connector.endpoint.Address, connector.sshConfig)
	if err != nil {
		return nil, false, err
	}
	connector.sshClient = client
	go connector.keepalive(client)
	return client, true, nil
}

// dialSSHClient connects and authenticates. Failures carry their stage:
// connect, handshake, host key or authenticate.
func dialSSHClient(ctx context.Context, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
	dialer := net.Dialer{Timeout: dialTimeout}
	raw, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, stageError(StageConnect, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(deadline)
	} else {
		_ = raw.SetDeadline(time.Now().Add(dialTimeout))
	}
	// The handshake checks the host key before authentication: the callback
	// outcome tells the two failures apart.
	hostKeyChecked, hostKeyRejected := false, false
	checked := *config
	checked.HostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := config.HostKeyCallback(hostname, remote, key); err != nil {
			hostKeyRejected = true
			return err
		}
		hostKeyChecked = true
		return nil
	}
	clientConn, channels, requests, err := ssh.NewClientConn(raw, address, &checked)
	if err != nil {
		raw.Close()
		switch {
		case hostKeyRejected:
			return nil, stageError(StageHostKey, err)
		case hostKeyChecked:
			return nil, stageError(StageAuthenticate, err)
		default:
			return nil, stageError(StageHandshake, err)
		}
	}
	_ = raw.SetDeadline(time.Time{})
	return ssh.NewClient(clientConn, channels, requests), nil
}

func (connector *Connector) keepalive(client *ssh.Client) {
	ticker := time.NewTicker(sshKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-connector.stop:
			return
		case <-ticker.C:
			if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				connector.dropSSH(client)
				return
			}
		}
	}
}

func (connector *Connector) dropSSH(client *ssh.Client) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	if connector.sshClient == client {
		connector.sshClient = nil
	}
	client.Close()
}

// Close releases the shared SSH connection. Connections already handed out
// close with it.
func (connector *Connector) Close() error {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	if connector.closed {
		return nil
	}
	connector.closed = true
	close(connector.stop)
	if connector.sshClient != nil {
		err := connector.sshClient.Close()
		connector.sshClient = nil
		return err
	}
	return nil
}

// errHostKeyCaptured aborts a scan handshake once the key is known.
var errHostKeyCaptured = errors.New("host key captured")

// ScanHostKey connects to an ssh endpoint, reads the server host key and
// aborts before authentication. It returns the key in authorized_keys form
// and its SHA256 fingerprint for operator confirmation.
func ScanHostKey(ctx context.Context, endpoint Endpoint) (string, string, error) {
	if endpoint.Kind != EndpointSSH {
		return "", "", fmt.Errorf("%w: host key scan needs an ssh endpoint", ErrInvalidEndpoint)
	}
	var captured ssh.PublicKey
	config := &ssh.ClientConfig{
		User: endpoint.User,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			captured = key
			return errHostKeyCaptured
		},
		Timeout: dialTimeout,
	}
	_, err := dialSSHClient(ctx, endpoint.Address, config)
	if captured == nil {
		if err == nil {
			err = errors.New("server sent no host key")
		}
		return "", "", err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(captured)))
	return line, ssh.FingerprintSHA256(captured), nil
}

// HostKeyFingerprint returns the SHA256 fingerprint of a pinned key.
func HostKeyFingerprint(text string) (string, error) {
	key, err := ParseHostKey(text)
	if err != nil {
		return "", err
	}
	return ssh.FingerprintSHA256(key), nil
}
