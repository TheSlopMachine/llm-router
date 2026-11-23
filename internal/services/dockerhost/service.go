package dockerhost

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/dockerengine"
)

// PolicySource lists the policies of every installed plugin that declares
// @require_api docker, enabled or disabled.
type PolicySource func(ctx context.Context) ([]Policy, error)

// ConnectionRemovedHook runs after the operator removes the Docker host.
type ConnectionRemovedHook func(ctx context.Context) error

// Service owns the Docker host on behalf of plugins.
type Service struct {
	database *gorm.DB

	wiringMu          sync.RWMutex
	logger            *slog.Logger
	policies          PolicySource
	connectionRemoved ConnectionRemovedHook

	sessionMu sync.Mutex
	current   *session

	// locks serializes operations per plugin and target.
	locks sync.Map

	imagesMu sync.Mutex
	images   map[string]*imageTask

	transportsMu sync.Mutex
	transports   map[transportKey]*http.Transport

	gatewayLogMu    sync.Mutex
	gatewayLogSince map[string]int64
}

// New builds the service on the durable database handle.
func New(database *db.Database) *Service {
	return &Service{
		database:        database.Handle,
		images:          map[string]*imageTask{},
		transports:      map[transportKey]*http.Transport{},
		gatewayLogSince: map[string]int64{},
	}
}

// SetLogger wires structured logging.
func (service *Service) SetLogger(logger *slog.Logger) {
	service.wiringMu.Lock()
	defer service.wiringMu.Unlock()
	service.logger = logger
}

// SetPolicySource wires the installed plugin policies for cleanup sweeps.
func (service *Service) SetPolicySource(source PolicySource) {
	service.wiringMu.Lock()
	defer service.wiringMu.Unlock()
	service.policies = source
}

// SetConnectionRemovedHook wires the work that follows removing the Docker
// host.
func (service *Service) SetConnectionRemovedHook(hook ConnectionRemovedHook) {
	service.wiringMu.Lock()
	defer service.wiringMu.Unlock()
	service.connectionRemoved = hook
}

func (service *Service) getLogger() *slog.Logger {
	service.wiringMu.RLock()
	defer service.wiringMu.RUnlock()
	return service.logger
}

func (service *Service) logInfo(message string, arguments ...any) {
	if logger := service.getLogger(); logger != nil {
		logger.Info(message, append([]any{"subsystem", "docker"}, arguments...)...)
	}
}

func (service *Service) logWarn(message string, arguments ...any) {
	if logger := service.getLogger(); logger != nil {
		logger.Warn(message, append([]any{"subsystem", "docker"}, arguments...)...)
	}
}

func (service *Service) logDebug(message string, arguments ...any) {
	if logger := service.getLogger(); logger != nil {
		logger.Debug(message, append([]any{"subsystem", "docker"}, arguments...)...)
	}
}

// session is one live daemon connection with the settings it was built
// from.
type session struct {
	client       *dockerengine.Client
	endpoint     dockerengine.Endpoint
	namespace    string
	dialMode     string
	gatewayImage string
	info         dockerengine.SystemInfo
	version      dockerengine.VersionInfo
}

// sessionProbeTimeout bounds the version and info calls of a new session.
const sessionProbeTimeout = 20 * time.Second

// session returns the live session, connecting on first use. Failures are
// not cached: the next call tries again.
func (service *Service) session(ctx context.Context) (*session, error) {
	service.sessionMu.Lock()
	defer service.sessionMu.Unlock()
	if service.current != nil {
		return service.current, nil
	}
	row, err := service.loadConnectionRow(ctx, service.database)
	if err != nil {
		return nil, newFault(ReasonUnreachable, err, "read docker connection: %v", err)
	}
	if row.Endpoint == "" {
		return nil, newFault(ReasonUnconfigured, nil, "docker is not configured: set the endpoint in Settings")
	}
	options, err := connectorOptions(row)
	if err != nil {
		return nil, newFault(ReasonUnconfigured, err, "docker endpoint: %v", err)
	}
	connector, err := dockerengine.NewConnector(options)
	if err != nil {
		return nil, newFault(ReasonUnconfigured, err, "docker connection: %v", err)
	}
	client := dockerengine.NewClient(connector)
	probe, cancel := context.WithTimeout(ctx, sessionProbeTimeout)
	defer cancel()
	version, err := client.Version(probe)
	if err != nil {
		client.Close()
		return nil, daemonFault(err, "docker daemon at %s: %v", options.Endpoint.String(), err)
	}
	info, err := client.Info(probe)
	if err != nil {
		client.Close()
		return nil, daemonFault(err, "docker daemon info: %v", err)
	}
	if info.OSType != "" && info.OSType != "linux" {
		client.Close()
		return nil, newFault(ReasonUnconfigured, nil, "docker daemon runs %s containers: only linux is supported", info.OSType)
	}
	current := &session{
		client:       client,
		endpoint:     options.Endpoint,
		namespace:    row.Namespace,
		gatewayImage: row.GatewayImage,
		info:         info,
		version:      version,
	}
	current.dialMode = resolveDialMode(row.DialMode, options.Endpoint, info)
	service.current = current
	service.logInfo("Docker daemon connected", "endpoint", options.Endpoint.String(), "version", version.Version, "dial_mode", current.dialMode)
	return current, nil
}

// daemonFault classifies a failed first call: a connection failure keeps
// its stage, a daemon answer is the daemon stage.
func daemonFault(err error, format string, arguments ...any) *Fault {
	fault := newFault(ReasonUnreachable, err, format, arguments...)
	var apiError *dockerengine.APIError
	if fault.Stage == "" && errors.As(err, &apiError) {
		fault.Stage = StageDaemon
	}
	return fault
}

// resolveDialMode applies the fixed auto table: route where the router or
// the SSH host reaches container addresses, relay elsewhere.
func resolveDialMode(configured string, endpoint dockerengine.Endpoint, info dockerengine.SystemInfo) string {
	if configured == DialModeRoute || configured == DialModeRelay {
		return configured
	}
	if info.Rootless() {
		return DialModeRelay
	}
	switch endpoint.Kind {
	case dockerengine.EndpointSSH:
		return DialModeRoute
	case dockerengine.EndpointUnix:
		if runtime.GOOS == "linux" || runtime.GOOS == "freebsd" {
			return DialModeRoute
		}
	}
	return DialModeRelay
}

// resetSession closes the live session and drops every cache bound to it.
func (service *Service) resetSession() {
	service.sessionMu.Lock()
	current := service.current
	service.current = nil
	service.sessionMu.Unlock()
	service.dropTransports(func(transportKey) bool { return true })
	if current != nil {
		current.client.Close()
	}
}

// Close releases the daemon connection.
func (service *Service) Close() {
	service.resetSession()
}

// faultFromError wraps a daemon error as a fault. Unavailable transports
// become unreachable. The session stays: its connector redials a lost
// connection on the next call, and a reset here would close a session that
// other calls still hold.
func (service *Service) faultFromError(err error, format string, arguments ...any) error {
	if err == nil {
		return nil
	}
	if _, ok := AsFault(err); ok {
		return err
	}
	var specErr *SpecError
	if errors.As(err, &specErr) {
		return err
	}
	message := fmt.Sprintf(format, arguments...) + ": " + err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return newFault(ReasonTimeout, err, "%s", message)
	case errors.Is(err, dockerengine.ErrUnavailable):
		return newFault(ReasonUnreachable, err, "%s", message)
	case errors.Is(err, dockerengine.ErrNotFound):
		return newFault(ReasonNotFound, err, "%s", message)
	default:
		return newFault(ReasonDaemonError, err, "%s", message)
	}
}

// ConnectionTest is the answer of a connection test.
type ConnectionTest struct {
	ServerVersion   string `json:"server_version"`
	APIVersion      string `json:"api_version"`
	OperatingSystem string `json:"operating_system"`
	Architecture    string `json:"architecture"`
	Rootless        bool   `json:"rootless"`
	GPU             bool   `json:"gpu"`
	DialMode        string `json:"dial_mode"`
	GatewayImage    string `json:"gateway_image"`
}

// TestConnection reads the daemon over the live session and makes sure the
// gateway image exists. A settings change already replaced the session.
func (service *Service) TestConnection(ctx context.Context) (ConnectionTest, error) {
	current, err := service.session(ctx)
	if err != nil {
		return ConnectionTest{}, err
	}
	image, err := service.ensureGatewayImage(ctx, current)
	if err != nil {
		return ConnectionTest{}, err
	}
	return ConnectionTest{
		ServerVersion:   current.version.Version,
		APIVersion:      current.version.APIVersion,
		OperatingSystem: current.info.OperatingSystem,
		Architecture:    current.info.Architecture,
		Rootless:        current.info.Rootless(),
		GPU:             current.info.GPUCapable(),
		DialMode:        current.dialMode,
		GatewayImage:    image,
	}, nil
}

// lock serializes operations on one plugin target.
func (service *Service) lock(pluginID, key string) func() {
	value, _ := service.locks.LoadOrStore(pluginID+"\x00"+key, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}
