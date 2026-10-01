package luaplugin

import (
	"errors"
	"net"
	"syscall"
	"testing"
)

func TestStructuralPenaltyReason(t *testing.T) {
	if got := structuralPenaltyReason(&net.DNSError{Err: "no such host"}); got != "dns_resolution" {
		t.Fatalf("DNS reason = %q, want dns_resolution", got)
	}
	if got := structuralPenaltyReason(&net.AddrError{Err: "bad addr"}); got != "address_parse" {
		t.Fatalf("addr reason = %q, want address_parse", got)
	}
	if got := structuralPenaltyReason(syscall.ECONNREFUSED); got != "connection_refused" {
		t.Fatalf("refused reason = %q, want connection_refused", got)
	}
	if got := structuralPenaltyReason(errors.New("boom")); got != "" {
		t.Fatalf("generic error must carry no blame, got %q", got)
	}
	if !isStructuralTransport(&net.DNSError{Err: "no such host"}) {
		t.Fatal("DNS error must stay structural")
	}
	if isStructuralTransport(errors.New("boom")) {
		t.Fatal("generic error must not be structural")
	}
}

func TestPenalizeCurrentProxyDisabled(t *testing.T) {
	c := &pluginHTTPClient{ctx: &execContext{proxyID: "px-test", proxyURL: "http://192.0.2.40:8080"}}
	c.penalizeCurrentProxy("dns_resolution")
	c.ctx.penalizeProxy = func(url, reason string) bool {
		t.Fatal("nil proxy URL must not penalize")
		return true
	}
	c.ctx.proxyURL = ""
	c.penalizeCurrentProxy("dns_resolution")
}

func TestPenalizeCurrentProxyCallsHook(t *testing.T) {
	var gotURL, gotReason string
	c := &pluginHTTPClient{ctx: &execContext{
		proxyID:  "px-test",
		proxyURL: "http://192.0.2.41:8080",
		penalizeProxy: func(url, reason string) bool {
			gotURL, gotReason = url, reason
			return true
		},
	}}
	c.penalizeCurrentProxy("connection_refused")
	if gotURL != "http://192.0.2.41:8080" || gotReason != "connection_refused" {
		t.Fatalf("hook args = (%q, %q)", gotURL, gotReason)
	}
}
