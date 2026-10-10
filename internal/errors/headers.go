package errors

import (
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// responseHeaderAllowList is the complete set of response header names a
// plugin may set on its terminal errors. Everything else is dropped so
// plugins can never touch framing, auth, or content negotiation.
var responseHeaderAllowList = map[string]bool{
	"Retry-After": true,
}

// SanitizeResponseHeaders canonicalizes names, drops anything outside the
// allow-list, and validates values. Retry-After accepts non-negative integer
// seconds or an HTTP date; anything else is dropped. Returns nil when nothing
// survives, so callers can range the result unconditionally.
func SanitizeResponseHeaders(headers map[string]string) map[string]string {
	out := map[string]string{}
	for name, value := range headers {
		canonical := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name))
		if !responseHeaderAllowList[canonical] {
			continue
		}
		value = strings.TrimSpace(value)
		if !validRetryAfter(value) {
			continue
		}
		out[canonical] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validRetryAfter(value string) bool {
	if value == "" {
		return false
	}
	if secs, err := strconv.Atoi(value); err == nil {
		return secs >= 0
	}
	_, err := time.Parse(http.TimeFormat, value)
	return err == nil
}

// WriteResponseHeaders sets sanitized plugin-supplied headers on the
// response. Call before the first WriteHeader only: after streaming starts
// headers no longer reach the client.
func WriteResponseHeaders(w http.ResponseWriter, headers map[string]string) {
	for name, value := range SanitizeResponseHeaders(headers) {
		w.Header().Set(name, value)
	}
}
