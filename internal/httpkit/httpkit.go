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

// StreamWriter is the single owner of streaming response commitment. The
// 200 status and SSE headers go out with the first byte, not before routing:
// a failure before any output (unknown provider, no credential, rate limit)
// still answers with its real HTTP status and headers such as Retry-After.
// Every write flushes, so each event reaches the client immediately instead
// of waiting in the server's output buffer.
type StreamWriter struct {
	response http.ResponseWriter
	flusher  http.Flusher
	started  bool
}

// NewStreamWriter wraps w. ok is false when w cannot flush: the caller then
// answers with a plain error instead of streaming.
func NewStreamWriter(w http.ResponseWriter) (stream *StreamWriter, ok bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	return &StreamWriter{response: w, flusher: flusher}, true
}

// Write commits the stream on first use, writes p and flushes.
func (s *StreamWriter) Write(p []byte) (int, error) {
	s.commit()
	written, err := s.response.Write(p)
	if err == nil {
		s.flusher.Flush()
	}
	return written, err
}

// Flush flushes committed output. Before the first byte it does nothing,
// so an early flush never commits a 200.
func (s *StreamWriter) Flush() {
	if s.started {
		s.flusher.Flush()
	}
}

// Started reports whether the stream committed (status 200 sent). Once true,
// failures must be reported inside the stream.
func (s *StreamWriter) Started() bool { return s.started }

func (s *StreamWriter) commit() {
	if s.started {
		return
	}
	s.started = true
	WriteSSEHeaders(s.response)
	s.response.WriteHeader(http.StatusOK)
}
