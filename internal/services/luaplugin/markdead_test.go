package luaplugin

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
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

func TestRedactProxyURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "password",
			input: "https://proxy-user:proxy-password@proxy.example.org:443",
			want:  "https://proxy-user:xxxxx@proxy.example.org:443",
		},
		{name: "empty", input: "", want: ""},
		{name: "invalid", input: "://invalid", want: "invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := redactProxyURL(test.input)
			if got != test.want {
				t.Fatalf("redactProxyURL() = %q, want %q", got, test.want)
			}
			if test.name == "password" && strings.Contains(got, "proxy-password") {
				t.Fatal("redacted proxy URL contains the password")
			}
		})
	}
}

func TestMarkDeadCallsHook(t *testing.T) {
	var gotURL, gotReason string
	c := &pluginHTTPClient{ctx: &executionContext{
		markDead: func(url, reason string) bool {
			gotURL, gotReason = url, reason
			return true
		},
	}}
	c.markDead("http://192.0.2.41:8080", "connection_refused")
	if gotURL != "http://192.0.2.41:8080" || gotReason != "connection_refused" {
		t.Fatalf("hook args = (%q, %q)", gotURL, gotReason)
	}
}

func TestMarkDeadDisabled(t *testing.T) {
	c := &pluginHTTPClient{ctx: &executionContext{}}
	c.markDead("", "dns_resolution")
	c.ctx.markDead = func(url, reason string) bool {
		t.Fatal("empty proxy URL must not mark")
		return true
	}
	c.markDead("", "dns_resolution")
}

func TestDoSingleStopsOnDeadContext(t *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	request, err := http.NewRequestWithContext(requestContext, "GET", "http://example.com/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	c := &pluginHTTPClient{ctx: &executionContext{}}
	_, _, err = c.doSingle(request, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dead context must surface cancellation, got %v", err)
	}
}
