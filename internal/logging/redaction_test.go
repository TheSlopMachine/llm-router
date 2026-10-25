package logging

import (
	"bytes"
	"log/slog"
	"testing"
)

func TestRedactLogValue(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "sensitive key", key: "api_key", value: "proxy-key", want: "[REDACTED]"},
		{name: "json field", key: "body", value: `{"access_token":"token-value","status":"ok"}`, want: `{"access_token":[REDACTED],"status":"ok"}`},
		{name: "bearer", key: "error", value: "Authorization failed: Bearer token-value", want: "Authorization failed: Bearer [REDACTED]"},
		{name: "basic", key: "detail", value: "Basic dXNlcjpwYXNzd29yZA==", want: "Basic [REDACTED]"},
		{name: "token field", key: "token", value: "token-value", want: "[REDACTED]"},
		{name: "jwt", key: "detail", value: "eyJheader-value.payload-value.signature-value", want: "[REDACTED]"},
		{name: "provider token", key: "detail", value: "sk-test-token-value-123456", want: "[REDACTED]"},
		{name: "url userinfo", key: "proxy_url", value: "https://proxy-user:proxy-password@proxy.example.org:443", want: "https://proxy-user:xxxxx@proxy.example.org:443"},
		{name: "query value", key: "endpoint", value: "https://example.org/?api_key=query-value", want: "https://example.org/?api_key=[REDACTED]"},
		{name: "ordinary value", key: "model", value: "example-model", want: "example-model"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := redactLogValue(test.key, test.value); got != test.want {
				t.Fatalf("redactLogValue() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestHandlerRedactsMessagesAndAttributes(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(NewHandler(&buffer, slog.LevelDebug))
	logger.Info(
		"request failed with Bearer token-value",
		slog.String("api_key", "key-value"),
		slog.String("proxy_url", "https://proxy-user:proxy-password@proxy.example.org:443"),
	)

	output := buffer.String()
	for _, secret := range []string{"key-value", "proxy-password", "Bearer token-value"} {
		if bytes.Contains([]byte(output), []byte(secret)) {
			t.Fatalf("log output contains secret %q: %s", secret, output)
		}
	}
	for _, expected := range []string{"[api_key: [REDACTED]]", "Bearer [REDACTED]", "proxy-user:xxxxx@proxy.example.org:443"} {
		if !bytes.Contains([]byte(output), []byte(expected)) {
			t.Fatalf("log output lacks %q: %s", expected, output)
		}
	}
}
