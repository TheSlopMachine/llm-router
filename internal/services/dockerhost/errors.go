package dockerhost

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/dockerengine"
)

// SpecError is plugin misuse: an invalid key, an undeclared image, an
// unknown field, a host path, an undeclared port, a limit above its
// ceiling. The Lua bridge raises it as a Lua error.
type SpecError struct {
	Message string
}

func (e *SpecError) Error() string { return e.Message }

func specError(format string, arguments ...any) *SpecError {
	return &SpecError{Message: fmt.Sprintf(format, arguments...)}
}

// Fault reasons. The Lua bridge returns faults as error tables with
// docker = true and reason set.
const (
	ReasonUnconfigured      = "unconfigured"
	ReasonUnreachable       = "unreachable"
	ReasonNotFound          = "not_found"
	ReasonOwnershipConflict = "ownership_conflict"
	ReasonContainerLimit    = "container_limit"
	ReasonPreparing         = "preparing"
	ReasonNotRunning        = "not_running"
	ReasonTimeout           = "timeout"
	ReasonGPUUnavailable    = "gpu_unavailable"
	ReasonRegistryAuth      = "registry_auth"
	ReasonDaemonError       = "daemon_error"
)

// Fault is a runtime failure a plugin handles: the daemon or the container
// state stands in the way.
type Fault struct {
	Reason  string
	Message string
	// Stage names where a daemon connection failed (dockerengine.Stage*),
	// empty for other faults.
	Stage string
	// Cause is the underlying error, kept for errors.Is matching.
	Cause error
}

func (fault *Fault) Error() string { return fault.Message }

func (fault *Fault) Unwrap() error { return fault.Cause }

// Status is the HTTP status a plugin returning the fault unchanged answers
// with.
func (fault *Fault) Status() int {
	switch fault.Reason {
	case ReasonUnconfigured, ReasonUnreachable, ReasonPreparing, ReasonNotRunning, ReasonGPUUnavailable:
		return http.StatusServiceUnavailable
	case ReasonTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}

func newFault(reason string, cause error, format string, arguments ...any) *Fault {
	fault := &Fault{Reason: reason, Message: fmt.Sprintf(format, arguments...), Cause: cause}
	var connectionError *dockerengine.ConnectionError
	if errors.As(cause, &connectionError) {
		fault.Stage = connectionError.Stage
	}
	return fault
}

// StageDaemon marks a daemon that answered with an error during the
// connection test.
const StageDaemon = "daemon"

// AsFault extracts a Fault from err.
func AsFault(err error) (*Fault, bool) {
	var fault *Fault
	if errors.As(err, &fault) {
		return fault, true
	}
	return nil, false
}
