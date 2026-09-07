package luaplugin

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

// uuidV5 returns the RFC 4122 §4.3 name-based UUID (SHA-1) for namespace
// and name, formatted as xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.
// The namespace must itself be a UUID string.
func uuidV5(namespace, name string) (string, error) {
	ns, err := parseUUID(namespace)
	if err != nil {
		return "", fmt.Errorf("invalid namespace UUID %q: %w", namespace, err)
	}
	sum := sha1.Sum(append(ns[:], name...))
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		sum[0:4],
		sum[4:6],
		sum[6:8],
		sum[8:10],
		sum[10:16],
	), nil
}

func parseUUID(s string) ([16]byte, error) {
	var out [16]byte
	clean := strings.ReplaceAll(s, "-", "")
	if len(clean) != 32 {
		return out, fmt.Errorf("expected 32 hex digits")
	}
	raw, err := hex.DecodeString(clean)
	if err != nil {
		return out, err
	}
	copy(out[:], raw)
	return out, nil
}

// randomHex returns n cryptographically random bytes hex-encoded.
func randomHex(n int) (string, error) {
	if n < 1 || n > 1024 {
		return "", fmt.Errorf("byte count must be between 1 and 1024")
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
