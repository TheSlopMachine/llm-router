package dockerengine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// APIVersion is the Engine API version every request pins. Docker 20.10
// and later serve it.
const APIVersion = "1.41"

// Sentinels matched by errors.Is on APIError.
var (
	ErrNotFound    = errors.New("docker object not found")
	ErrConflict    = errors.New("docker object conflict")
	ErrUnavailable = errors.New("docker daemon unavailable")
)

// maxErrorBody caps the daemon error body kept in APIError.
const maxErrorBody = 64 << 10

// APIError is one non-2xx daemon answer.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("docker daemon status %d: %s", e.Status, e.Message)
}

// Is maps daemon statuses to the package sentinels.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrConflict:
		return e.Status == http.StatusConflict
	}
	return false
}

// Client is one Engine API client over a Connector.
type Client struct {
	connector *Connector
	http      *http.Client
}

// NewClient builds a client. Idle daemon connections are kept for reuse.
func NewClient(connector *Connector) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			conn, err := connector.DialDaemon(ctx)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
			}
			return conn, nil
		},
		MaxIdleConns:          8,
		IdleConnTimeout:       60 * time.Second,
		ResponseHeaderTimeout: 0,
		DisableCompression:    true,
	}
	return &Client{connector: connector, http: &http.Client{Transport: transport}}
}

// Connector returns the transport the client uses.
func (client *Client) Connector() *Connector { return client.connector }

// Close drops idle connections and closes the connector.
func (client *Client) Close() error {
	if transport, ok := client.http.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	return client.connector.Close()
}

// request describes one daemon call.
type request struct {
	method      string
	path        string
	query       url.Values
	body        io.Reader
	contentType string
	headers     http.Header
}

func (client *Client) newRequest(ctx context.Context, call request) (*http.Request, error) {
	target := "http://docker/v" + APIVersion + call.path
	if len(call.query) > 0 {
		target += "?" + call.query.Encode()
	}
	httpRequest, err := http.NewRequestWithContext(ctx, call.method, target, call.body)
	if err != nil {
		return nil, err
	}
	for name, values := range call.headers {
		for _, value := range values {
			httpRequest.Header.Add(name, value)
		}
	}
	if call.contentType != "" {
		httpRequest.Header.Set("Content-Type", call.contentType)
	}
	return httpRequest, nil
}

// do sends one call. Statuses of 400 and above become APIError; the caller
// owns the body of every other response.
func (client *Client) do(ctx context.Context, call request) (*http.Response, error) {
	httpRequest, err := client.newRequest(ctx, call)
	if err != nil {
		return nil, err
	}
	response, err := client.http.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		defer response.Body.Close()
		return nil, decodeAPIError(response)
	}
	return response, nil
}

func decodeAPIError(response *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
	var payload struct {
		Message string `json:"message"`
	}
	message := strings.TrimSpace(string(raw))
	if json.Unmarshal(raw, &payload) == nil && payload.Message != "" {
		message = payload.Message
	}
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	return &APIError{Status: response.StatusCode, Message: message}
}

// doJSON sends input as JSON (nil sends no body) and decodes the answer
// into output (nil discards it). It returns the response status.
func (client *Client) doJSON(ctx context.Context, method, path string, query url.Values, input, output any) (int, error) {
	call := request{method: method, path: path, query: query}
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return 0, fmt.Errorf("encode %s %s: %w", method, path, err)
		}
		call.body = bytes.NewReader(encoded)
		call.contentType = "application/json"
	}
	response, err := client.do(ctx, call)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if output != nil && response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotModified {
		if err := json.NewDecoder(response.Body).Decode(output); err != nil {
			return response.StatusCode, fmt.Errorf("decode %s %s: %w", method, path, err)
		}
	} else {
		_, _ = io.Copy(io.Discard, response.Body)
	}
	return response.StatusCode, nil
}

// ProgressEvent is one line of a pull, import or build progress stream:
// build output in Stream, layer progress in Status, LayerID and the
// byte counts.
type ProgressEvent struct {
	Stream         string `json:"stream"`
	Status         string `json:"status"`
	LayerID        string `json:"id"`
	ProgressDetail struct {
		Current int64 `json:"current"`
		Total   int64 `json:"total"`
	} `json:"progressDetail"`
	Error       string `json:"error"`
	ErrorDetail struct {
		Message string `json:"message"`
	} `json:"errorDetail"`
}

// readProgress drains a JSON progress stream, handing each line to
// onProgress (nil ignores them). An error line fails the call; the last
// stream lines travel in the error for build diagnostics.
func readProgress(body io.Reader, onProgress func(ProgressEvent)) error {
	decoder := json.NewDecoder(bufio.NewReader(body))
	var tail []string
	for {
		var message ProgressEvent
		if err := decoder.Decode(&message); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read progress stream: %w", err)
		}
		if onProgress != nil {
			onProgress(message)
		}
		if line := strings.TrimSpace(message.Stream); line != "" {
			tail = append(tail, line)
			if len(tail) > 8 {
				tail = tail[1:]
			}
		}
		if message.Error != "" || message.ErrorDetail.Message != "" {
			text := message.ErrorDetail.Message
			if text == "" {
				text = message.Error
			}
			if len(tail) > 0 {
				text += " (last output: " + strings.Join(tail, " | ") + ")"
			}
			return errors.New(text)
		}
	}
}
