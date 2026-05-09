// Package util provides shared utility functions for the llm-router framework.
package util

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// GenerateID returns a cryptographically secure 16-byte hex string (32 characters).
// Used for database record IDs across all services.
func GenerateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GenerateToken returns a cryptographically secure 32-byte hex string (64 characters).
// Used for session tokens and router tokens that require higher entropy.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashSecret produces a deterministic SHA-256 hex digest of a secret string.
// Used for storing hashed tokens in the database.
func HashSecret(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

