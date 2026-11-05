package httpkit

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStreamWriterDefersCommitUntilFirstByte(t *testing.T) {
	recorder := httptest.NewRecorder()
	stream, ok := NewStreamWriter(recorder)
	if !ok {
		t.Fatal("recorder flushes")
	}
	stream.Flush()
	if stream.Started() || recorder.Flushed {
		t.Fatal("flush before the first byte must not commit")
	}
	// Nothing written yet: the caller can still answer with a real status.
	recorder.WriteHeader(http.StatusTooManyRequests)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
}

func TestStreamWriterCommitsAndFlushesEveryWrite(t *testing.T) {
	recorder := httptest.NewRecorder()
	stream, _ := NewStreamWriter(recorder)
	if _, err := stream.Write([]byte("data: {}\n\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !stream.Started() || recorder.Code != http.StatusOK || !recorder.Flushed {
		t.Fatalf("first write must commit 200 and flush, got code=%d flushed=%v", recorder.Code, recorder.Flushed)
	}
	if recorder.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("SSE headers missing: %v", recorder.Header())
	}
}
