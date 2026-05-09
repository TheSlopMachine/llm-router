// Package errors defines standard error types used across llm-router services.
package errors

import (
	"errors"
	"fmt"
)

// Standard sentinel errors
var (
	// ErrNotFound is returned when a requested resource does not exist
	ErrNotFound = errors.New("not found")

	// ErrAlreadyExists is returned when attempting to create a duplicate resource
	ErrAlreadyExists = errors.New("already exists")

	// ErrInvalidInput is returned when input validation fails
	ErrInvalidInput = errors.New("invalid input")

	// ErrUnauthorized is returned for authentication failures
	ErrUnauthorized = errors.New("unauthorized")

	// ErrForbidden is returned for authorization failures
	ErrForbidden = errors.New("forbidden")

	// ErrModelNotAllowed is returned when a token's rules forbid the requested model
	ErrModelNotAllowed = errors.New("model not allowed by token rules")

	// ErrNoCredential is returned when no credential is available for a provider
	ErrNoCredential = errors.New("no credential available for provider")

	// ErrProviderNotFound is returned when no provider can handle a given ModelId
	ErrProviderNotFound = errors.New("provider not found for model")
)

// NotFoundError wraps ErrNotFound with context about what was not found
type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	if e.ID != "" {
		return fmt.Sprintf("%s %q not found", e.Resource, e.ID)
	}
	return fmt.Sprintf("%s not found", e.Resource)
}

func (e *NotFoundError) Unwrap() error {
	return ErrNotFound
}

// NewNotFoundError creates a NotFoundError
func NewNotFoundError(resource, id string) error {
	return &NotFoundError{Resource: resource, ID: id}
}

// ValidationError wraps ErrInvalidInput with details about what failed validation
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("validation failed for %s: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation failed: %s", e.Message)
}

func (e *ValidationError) Unwrap() error {
	return ErrInvalidInput
}

// NewValidationError creates a ValidationError
func NewValidationError(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}

