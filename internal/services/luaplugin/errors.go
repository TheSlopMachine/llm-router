package luaplugin

import (
	"errors"
	"fmt"
)

// ErrHandlerNotFound is returned when a plugin does not declare the
// requested handler. Callers apply the fixed fallback behavior per handler.
var ErrHandlerNotFound = errors.New("luaplugin: handler not declared")

// ErrTypeKeyConflict is returned when installing a plugin whose type key is
// already served by a different plugin. The registry is a single source of
// truth: one type key maps to exactly one plugin.
var ErrTypeKeyConflict = errors.New("luaplugin: type key already registered by another plugin")

// ErrPluginDisabled is returned when a type key belongs to an installed but
// disabled plugin. Disabled plugins stay inventoried; the registry serves
// enabled types only. Callers map it to provider_disabled, never to a
// missing-backend outcome.
var ErrPluginDisabled = errors.New("luaplugin: plugin is disabled")

// ErrPermissionsChanged is returned when a plugin source requests other
// permissions than the ones the operator approved.
var ErrPermissionsChanged = errors.New("plugin permissions changed: review and approve them on the plugin card")

// ErrDockerUnconfigured is returned when enabling a plugin that needs the
// Docker API while no Docker host is configured.
var ErrDockerUnconfigured = errors.New("this plugin needs Docker: set up a Docker host first")

// notFoundError wraps ErrHandlerNotFound with plugin context.
type notFoundError struct {
	PluginID string
	TypeKey  string
	Handler  string
}

func (e *notFoundError) Error() string {
	return fmt.Sprintf("plugin %q (type %q) does not declare handler %q", e.PluginID, e.TypeKey, e.Handler)
}

func (e *notFoundError) Unwrap() error { return ErrHandlerNotFound }
