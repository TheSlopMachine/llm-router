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
