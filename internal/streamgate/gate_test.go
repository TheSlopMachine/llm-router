package streamgate

import (
	"io"
	"testing"
)

func TestWriterLatchesOnFirstByte(t *testing.T) {
	w := New(discard{})
	if w.Written() {
		t.Fatal("fresh gate reports written")
	}
	if _, err := w.Write([]byte("data: x\n\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !w.Written() {
		t.Fatal("gate did not latch after write")
	}
}

func TestWriterEmptyWriteKeepsGateOpen(t *testing.T) {
	w := New(discard{})
	if _, err := w.Write(nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	if w.Written() {
		t.Fatal("empty write must not commit the stream")
	}
}

func TestWriterNilDstErrors(t *testing.T) {
	w := New(nil)
	if _, err := w.Write([]byte("x")); err != io.ErrClosedPipe {
		t.Fatalf("expected closed-pipe error, got %v", err)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
