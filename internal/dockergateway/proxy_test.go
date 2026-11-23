package dockergateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func authorization(token string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(ProxyUser+":"+token))
}

func TestProxyRequiresCredentials(t *testing.T) {
	proxy := NewProxy([]string{"api.example.com"}, "secret", &bytes.Buffer{})
	for _, header := range []string{"", authorization("wrong"), "Bearer secret"} {
		request := httptest.NewRequest(http.MethodGet, "http://api.example.com/", nil)
		if header != "" {
			request.Header.Set("Proxy-Authorization", header)
		}
		recorder := httptest.NewRecorder()
		proxy.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusProxyAuthRequired {
			t.Errorf("header %q: status %d, want 407", header, recorder.Code)
		}
	}
}

func TestProxyBlocksOutsideAllowList(t *testing.T) {
	output := &bytes.Buffer{}
	proxy := NewProxy([]string{"api.example.com"}, "secret", output)
	cases := map[string]string{
		"other.example.com:443": "not_allowed",
		"10.0.0.5:443":          "not_public",
	}
	for target, reason := range cases {
		output.Reset()
		request := httptest.NewRequest(http.MethodConnect, "http://"+target, nil)
		request.Host = target
		request.Header.Set("Proxy-Authorization", authorization("secret"))
		recorder := httptest.NewRecorder()
		proxy.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("CONNECT %s: status %d, want 403", target, recorder.Code)
		}
		var event Event
		if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &event); err != nil {
			t.Fatalf("CONNECT %s: event line %q: %v", target, output.String(), err)
		}
		host, _, _ := net.SplitHostPort(target)
		if event.Event != EventEgressBlocked || event.Host != host || event.Reason != reason {
			t.Errorf("CONNECT %s: event %+v, want %s for %s", target, event, reason, host)
		}
	}
}

func TestProxyRefusesPrivateAnswersUnderWildcard(t *testing.T) {
	output := &bytes.Buffer{}
	proxy := NewProxy([]string{"*"}, "secret", output)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := proxy.dialAllowed(ctx, "tcp", "127.0.0.1:80"); err == nil {
		t.Fatal("dial to loopback under wildcard succeeded")
	}
	if _, err := proxy.dialAllowed(ctx, "tcp", "localhost:80"); err == nil {
		t.Fatal("dial to localhost under wildcard succeeded")
	}
	if !strings.Contains(output.String(), `"reason":"not_public"`) {
		t.Errorf("events %q lack not_public", output.String())
	}
}

func TestRelayBridgesOnePort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buffer := make([]byte, 4)
		_, _ = conn.Read(buffer)
		_, _ = conn.Write([]byte("pong:" + string(buffer)))
	}()
	output := &bytes.Buffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Relay(ctx, listener.Addr().String(), strings.NewReader("ping"), output); err != nil {
		t.Fatalf("Relay error = %v", err)
	}
	if output.String() != "pong:ping" {
		t.Fatalf("Relay output = %q", output.String())
	}
}
