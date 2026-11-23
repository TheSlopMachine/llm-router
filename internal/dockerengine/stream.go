package dockerengine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// Stream identifiers of the multiplexed attach format.
const (
	streamStdin  = 0
	streamStdout = 1
	streamStderr = 2
)

// frameHeaderSize is the fixed header of one multiplexed frame.
const frameHeaderSize = 8

// ErrOutputLimit reports output that exceeded the caller's cap. The output
// read so far is still returned.
var ErrOutputLimit = errors.New("output exceeds the size limit")

// frameReader reads one multiplexed stream frame by frame.
type frameReader struct {
	source    io.Reader
	header    [frameHeaderSize]byte
	remaining int
	stream    byte
}

// next returns the stream of the next frame and its size.
func (reader *frameReader) next() (byte, int, error) {
	if _, err := io.ReadFull(reader.source, reader.header[:]); err != nil {
		return 0, 0, err
	}
	size := int(binary.BigEndian.Uint32(reader.header[4:]))
	return reader.header[0], size, nil
}

// Demultiplex splits a multiplexed stream into stdout and stderr. limit caps
// each stream (0 = no cap); excess is discarded and reported as
// ErrOutputLimit after the stream ends.
func Demultiplex(source io.Reader, limit int) ([]byte, []byte, error) {
	reader := &frameReader{source: bufio.NewReader(source)}
	var stdout, stderr bytes.Buffer
	truncated := false
	for {
		stream, size, err := reader.next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("read stream frame: %w", err)
		}
		target := &stdout
		switch stream {
		case streamStdout, streamStdin:
		case streamStderr:
			target = &stderr
		default:
			return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("read stream frame: unknown stream %d", stream)
		}
		keep := size
		if limit > 0 && target.Len()+keep > limit {
			keep = max(0, limit-target.Len())
			truncated = true
		}
		if _, err := io.CopyN(target, reader.source, int64(keep)); err != nil {
			return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("read stream frame: %w", err)
		}
		if _, err := io.CopyN(io.Discard, reader.source, int64(size-keep)); err != nil {
			return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("read stream frame: %w", err)
		}
	}
	if truncated {
		return stdout.Bytes(), stderr.Bytes(), ErrOutputLimit
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}

// ExecSession is one attached exec: stdin goes to the process, Stdout reads
// the process stdout. Stderr collects up to 64 KiB of the process stderr.
type ExecSession struct {
	ExecID string
	conn   net.Conn
	frames *frameReader

	mu     sync.Mutex
	stderr bytes.Buffer
	closed bool
}

// maxSessionStderr caps the stderr an attached session keeps.
const maxSessionStderr = 64 << 10

// ExecAttach creates and starts an exec attached to stdin, stdout and
// stderr over a hijacked daemon connection.
func (client *Client) ExecAttach(ctx context.Context, containerID string, config ExecConfig) (*ExecSession, error) {
	config.AttachStdin = true
	config.AttachStdout = true
	config.AttachStderr = true
	config.Tty = false
	execID, err := client.ExecCreate(ctx, containerID, config)
	if err != nil {
		return nil, err
	}
	conn, reader, err := client.hijack(ctx, "/exec/"+execID+"/start", map[string]bool{"Detach": false, "Tty": false})
	if err != nil {
		return nil, err
	}
	return &ExecSession{ExecID: execID, conn: conn, frames: &frameReader{source: reader}}, nil
}

// ExecResult is the outcome of one finished exec.
type ExecResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	// Truncated reports output cut at the caller's limit.
	Truncated bool
}

// execExitPollInterval spaces exit-code reads after the output stream ends.
const execExitPollInterval = 100 * time.Millisecond

// ExecRun runs one command to completion and collects its output. stdin
// may be nil. limit caps stdout and stderr each (0 = no cap). Cancelling
// ctx closes the stream; the process keeps running in the container until
// it exits on its own.
func (client *Client) ExecRun(ctx context.Context, containerID string, config ExecConfig, stdin []byte, limit int) (ExecResult, error) {
	config.AttachStdin = stdin != nil
	config.AttachStdout = true
	config.AttachStderr = true
	config.Tty = false
	execID, err := client.ExecCreate(ctx, containerID, config)
	if err != nil {
		return ExecResult{}, err
	}
	conn, reader, err := client.hijack(ctx, "/exec/"+execID+"/start", map[string]bool{"Detach": false, "Tty": false})
	if err != nil {
		return ExecResult{}, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if stdin != nil {
		go func() {
			_, _ = conn.Write(stdin)
			if closer, ok := conn.(interface{ CloseWrite() error }); ok {
				_ = closer.CloseWrite()
			}
		}()
	}
	stdout, stderr, err := Demultiplex(reader, limit)
	result := ExecResult{Stdout: stdout, Stderr: stderr}
	if errors.Is(err, ErrOutputLimit) {
		result.Truncated = true
	} else if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, err
	}
	for {
		details, err := client.ExecInspect(ctx, execID)
		if err != nil {
			return result, err
		}
		if !details.Running {
			result.ExitCode = details.ExitCode
			return result, nil
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(execExitPollInterval):
		}
	}
}

// hijack sends one upgrade request on a dedicated daemon connection and
// returns the raw connection once the daemon switches protocols.
func (client *Client) hijack(ctx context.Context, path string, body any) (net.Conn, *bufio.Reader, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, nil, err
	}
	conn, err := client.connector.DialDaemon(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	httpRequest, err := client.newRequest(ctx, request{method: http.MethodPost, path: path, body: bytes.NewReader(encoded), contentType: "application/json"})
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	httpRequest.Header.Set("Connection", "Upgrade")
	httpRequest.Header.Set("Upgrade", "tcp")
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err := httpRequest.Write(conn); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("send %s: %w", path, err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, httpRequest)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("read %s answer: %w", path, err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols && response.StatusCode != http.StatusOK {
		defer conn.Close()
		return nil, nil, decodeAPIError(response)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, reader, nil
}

// Read reads process stdout. Stderr frames are collected on the side.
func (session *ExecSession) Read(buffer []byte) (int, error) {
	for {
		if session.frames.remaining > 0 {
			chunk := buffer
			if len(chunk) > session.frames.remaining {
				chunk = chunk[:session.frames.remaining]
			}
			if session.frames.stream == streamStderr {
				scratch := make([]byte, len(chunk))
				n, err := session.frames.source.Read(scratch)
				session.frames.remaining -= n
				session.keepStderr(scratch[:n])
				if err != nil {
					return 0, err
				}
				continue
			}
			n, err := session.frames.source.Read(chunk)
			session.frames.remaining -= n
			return n, err
		}
		stream, size, err := session.frames.next()
		if err != nil {
			return 0, err
		}
		session.frames.stream = stream
		session.frames.remaining = size
	}
}

func (session *ExecSession) keepStderr(data []byte) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if room := maxSessionStderr - session.stderr.Len(); room > 0 {
		if len(data) > room {
			data = data[:room]
		}
		session.stderr.Write(data)
	}
}

// Stderr returns the stderr collected so far.
func (session *ExecSession) Stderr() string {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.stderr.String()
}

// Write writes to process stdin.
func (session *ExecSession) Write(data []byte) (int, error) { return session.conn.Write(data) }

// CloseWrite ends process stdin when the transport supports half-close.
func (session *ExecSession) CloseWrite() error {
	if closer, ok := session.conn.(interface{ CloseWrite() error }); ok {
		return closer.CloseWrite()
	}
	return nil
}

// Close closes the hijacked connection.
func (session *ExecSession) Close() error {
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return nil
	}
	session.closed = true
	session.mu.Unlock()
	return session.conn.Close()
}

// Conn wraps the session as a net.Conn: the relay mode of container dials
// uses it as the TCP stream to the container port.
func (session *ExecSession) Conn() net.Conn { return &sessionConn{session: session} }

type sessionConn struct {
	session *ExecSession
}

func (conn *sessionConn) Read(buffer []byte) (int, error)  { return conn.session.Read(buffer) }
func (conn *sessionConn) Write(buffer []byte) (int, error) { return conn.session.Write(buffer) }
func (conn *sessionConn) Close() error                     { return conn.session.Close() }
func (conn *sessionConn) CloseWrite() error                { return conn.session.CloseWrite() }
func (conn *sessionConn) LocalAddr() net.Addr              { return relayAddress{} }
func (conn *sessionConn) RemoteAddr() net.Addr             { return relayAddress{} }
func (conn *sessionConn) SetDeadline(deadline time.Time) error {
	return conn.session.conn.SetDeadline(deadline)
}
func (conn *sessionConn) SetReadDeadline(deadline time.Time) error {
	return conn.session.conn.SetReadDeadline(deadline)
}
func (conn *sessionConn) SetWriteDeadline(deadline time.Time) error {
	return conn.session.conn.SetWriteDeadline(deadline)
}

type relayAddress struct{}

func (relayAddress) Network() string { return "docker-exec" }
func (relayAddress) String() string  { return "docker-exec" }
