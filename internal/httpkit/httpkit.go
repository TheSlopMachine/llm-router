// Package httpkit holds shared HTTP transport helpers for the API surfaces.
package httpkit

import "net/http"

// WriteSSEHeaders sets the Server-Sent Events headers shared by every
// streaming endpoint. Callers still own WriteHeader and Flush.
func WriteSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}
