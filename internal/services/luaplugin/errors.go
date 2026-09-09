package luaplugin

import (
	"errors"
	"fmt"
)

// ErrHandlerNotFound is returned when a plugin does not declare the
// requested handler. Callers apply the fixed fallback behavior per handler.
var ErrHandlerNotFound = errors.New("luaplugin: handler not declared")

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
