// Package streamgate tracks whether a streaming response committed its
// first byte to the client. Failover across credentials or models is only
// allowed before that point; afterwards the stream belongs to one upstream
// and ends with its error.
package streamgate

import "io"

// Writer wraps an io.Writer and records whether any byte was written.
type Writer struct {
	dst     io.Writer
	written bool
}

// New wraps dst. A nil dst is kept nil-safe: writes report an error.
func New(dst io.Writer) *Writer {
	return &Writer{dst: dst}
}

// Write forwards p to the wrapped writer and latches the written flag when
// at least one byte flows through, even if an error is also reported.
func (g *Writer) Write(p []byte) (int, error) {
	if g.dst == nil {
		return 0, io.ErrClosedPipe
	}
	n, err := g.dst.Write(p)
	if n > 0 {
		g.written = true
	}
	return n, err
}

// Written reports whether any byte reached the client.
func (g *Writer) Written() bool {
	return g.written
}

// Unwrap returns the wrapped writer.
func (g *Writer) Unwrap() io.Writer {
	return g.dst
}
