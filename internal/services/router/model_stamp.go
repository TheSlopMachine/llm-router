package router

import (
	"bytes"
	"encoding/json"
	"io"
)

// Response model identity: every routed chat response names the model that
// answered as its full router id (provider/model), whatever the backend put
// there (plugins often return the bare upstream name). Virtual models pass
// their member's stamped value through untouched, so a virtual request
// reports the member that answered, not the virtual name.

// modelStampWriter rewrites the "model" field of every OpenAI SSE data frame
// to one router model id. Other fields and non-JSON frames ([DONE],
// comments) pass through byte-for-byte; partial frames wait for their
// newline.
type modelStampWriter struct {
	next    io.Writer
	model   json.RawMessage
	pending []byte
}

func newModelStampWriter(next io.Writer, model string) *modelStampWriter {
	encoded, _ := json.Marshal(model)
	return &modelStampWriter{next: next, model: encoded}
}

// maxStampPending bounds a buffered partial line; a longer line passes
// through unstamped.
const maxStampPending = 4 << 20

// Write consumes p and forwards every complete line, stamped when it is a
// JSON data frame. It reports len(p) on success: the bytes are accepted even
// when a partial line stays buffered for the next call.
func (w *modelStampWriter) Write(p []byte) (int, error) {
	w.pending = append(w.pending, p...)
	for {
		newline := bytes.IndexByte(w.pending, '\n')
		if newline < 0 {
			break
		}
		line := w.pending[:newline+1]
		if _, err := w.next.Write(w.stamp(line)); err != nil {
			return 0, err
		}
		w.pending = w.pending[newline+1:]
	}
	if len(w.pending) > maxStampPending {
		if _, err := w.next.Write(w.pending); err != nil {
			return 0, err
		}
		w.pending = w.pending[:0]
	}
	return len(p), nil
}

// Flush forwards a buffered partial line and the downstream flush. Stream
// producers end every frame with a newline, so a partial line here is a
// truncated frame and passes through as-is.
func (w *modelStampWriter) Flush() {
	if len(w.pending) > 0 {
		_, _ = w.next.Write(w.pending)
		w.pending = w.pending[:0]
	}
	if flusher, ok := w.next.(interface{ Flush() }); ok {
		flusher.Flush()
	}
}

func (w *modelStampWriter) stamp(line []byte) []byte {
	payload, found := bytes.CutPrefix(line, []byte("data: "))
	if !found {
		return line
	}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return line
	}
	var frame map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &frame); err != nil {
		return line
	}
	frame["model"] = w.model
	encoded, err := json.Marshal(frame)
	if err != nil {
		return line
	}
	out := make([]byte, 0, len(encoded)+8)
	out = append(out, "data: "...)
	out = append(out, encoded...)
	return append(out, '\n')
}
