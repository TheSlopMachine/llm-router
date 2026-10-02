package luaplugin

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
)

func TestMarkDeadReason(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "dns", err: &net.DNSError{Err: "no such host"}, want: "dns_resolution"},
		{name: "addr", err: &net.AddrError{Err: "bad addr"}, want: "address_parse"},
		{name: "refused", err: syscall.ECONNREFUSED, want: "connection_refused"},
		{name: "reset", err: syscall.ECONNRESET, want: "connection_reset"},
		{name: "eof", err: io.EOF, want: "early_eof"},
		{name: "generic", err: errors.New("boom"), want: ""},
		{name: "timeout", err: context.DeadlineExceeded, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := markDeadReason(tc.err); got != tc.want {
				t.Fatalf("markDeadReason() = %q, want %q", got, tc.want)
			}
		})
	}
	var header tls.RecordHeaderError
	if got := markDeadReason(header); got != "tls_handshake" {
		t.Fatalf("record header reason = %q, want tls_handshake", got)
	}
	if !isStructuralTransport(&net.DNSError{Err: "no such host"}) {
		t.Fatal("DNS error must stay structural")
	}
	if isStructuralTransport(errors.New("boom")) {
		t.Fatal("generic error must not be structural")
	}
}

func TestMarkCurrentDeadCallsHook(t *testing.T) {
	var gotURL, gotReason string
	c := &pluginHTTPClient{ctx: &execContext{
		proxyID:  "px-test",
		proxyURL: "http://192.0.2.41:8080",
		markDead: func(url, reason string) bool {
			gotURL, gotReason = url, reason
			return true
		},
	}}
	c.markCurrentDead("connection_refused")
	if gotURL != "http://192.0.2.41:8080" || gotReason != "connection_refused" {
		t.Fatalf("hook args = (%q, %q)", gotURL, gotReason)
	}
}

func TestMarkCurrentDeadDisabled(t *testing.T) {
	c := &pluginHTTPClient{ctx: &execContext{proxyID: "px-test", proxyURL: "http://192.0.2.40:8080"}}
	c.markCurrentDead("dns_resolution")
	c.ctx.markDead = func(url, reason string) bool {
		t.Fatal("empty proxy URL must not mark")
		return true
	}
	c.ctx.proxyURL = ""
	c.markCurrentDead("dns_resolution")
}

type messageSink struct {
	mu   sync.Mutex
	msgs []string
}

func (s *messageSink) Enabled(context.Context, slog.Level) bool { return true }
func (s *messageSink) Handle(_ context.Context, r slog.Record) error {
	s.mu.Lock()
	s.msgs = append(s.msgs, r.Message)
	s.mu.Unlock()
	return nil
}
func (s *messageSink) WithAttrs([]slog.Attr) slog.Handler { return s }
func (s *messageSink) WithGroup(string) slog.Handler      { return s }

func (s *messageSink) count(substr string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.msgs {
		if strings.Contains(m, substr) {
			n++
		}
	}
	return n
}

func TestRotationStopsOnDeadContext(t *testing.T) {
	sink := &messageSink{}
	resolved := 0
	marked := 0
	ex := &execContext{
		logger: slog.New(sink),
		proxyResolver: func(_ context.Context, _ *PluginRecord, _ map[string]any, _ exhausted.Segments) ([]ProxyPick, error) {
			resolved++
			return []ProxyPick{
				{ID: "px-a", URL: "http://127.0.0.1:1"},
				{ID: "px-b", URL: "http://127.0.0.1:2"},
			}, nil
		},
		markDead: func(url, reason string) bool {
			marked++
			return true
		},
	}
	reqCtx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(reqCtx, "GET", "http://example.com/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	c := &pluginHTTPClient{ctx: ex, proxyClients: map[string]*http.Client{}}
	_, _, _, _, err = c.doWithProxyRotation(req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dead context must surface cancellation, got %v", err)
	}
	if resolved != 1 {
		t.Fatalf("picks must resolve once, got %d", resolved)
	}
	if marked != 0 {
		t.Fatalf("cancellation carries no proxy blame, marked %d", marked)
	}
	if n := sink.count("rotating"); n != 0 {
		t.Fatalf("dead context must not rotate, got %d rotations", n)
	}
}
