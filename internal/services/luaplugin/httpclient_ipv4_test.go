package luaplugin

import (
	"context"
	"net"
	"testing"
)

type stubIPResolver struct {
	ipNetwork         string
	ipHost            string
	ips               []net.IP
	ipErr             error
	addrHost          string
	addrs             []net.IPAddr
	addrErr           error
	lookupIPCalls     int
	lookupIPAddrCalls int
}

func (r *stubIPResolver) LookupIP(_ context.Context, network, host string) ([]net.IP, error) {
	r.lookupIPCalls++
	r.ipNetwork = network
	r.ipHost = host
	return r.ips, r.ipErr
}

func (r *stubIPResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	r.lookupIPAddrCalls++
	r.addrHost = host
	return r.addrs, r.addrErr
}

func TestResolveAndPickIPv4OnlyUsesIPv4Lookup(t *testing.T) {
	resolver := &stubIPResolver{
		// A correct resolver call requests ip4. Still include an IPv6 value
		// to make sure the guard never returns a non-IPv4 result from a faulty stub.
		ips: []net.IP{
			net.ParseIP("2001:db8::5"),
			net.ParseIP("203.0.113.10"),
		},
	}
	guard := newSSRFGuard([]string{"*"})
	guard.resolver = resolver

	ip, err := guard.resolveAndPick(context.Background(), "provider.example", true)
	if err != nil {
		t.Fatalf("resolve IPv4-only host: %v", err)
	}
	if resolver.ipNetwork != "ip4" || resolver.ipHost != "provider.example" {
		t.Fatalf("LookupIP called with network=%q host=%q", resolver.ipNetwork, resolver.ipHost)
	}
	if resolver.lookupIPCalls != 1 || resolver.lookupIPAddrCalls != 0 {
		t.Fatalf("unexpected resolver calls: LookupIP=%d LookupIPAddr=%d", resolver.lookupIPCalls, resolver.lookupIPAddrCalls)
	}
	if ip.To4() == nil || !ip.Equal(net.ParseIP("203.0.113.10")) {
		t.Fatalf("resolved IP = %v, want IPv4 203.0.113.10", ip)
	}
}

func TestResolveAndPickIPv4OnlyRejectsIPv6Literal(t *testing.T) {
	resolver := &stubIPResolver{}
	guard := newSSRFGuard([]string{"*"})
	guard.resolver = resolver

	if _, err := guard.resolveAndPick(context.Background(), "2001:db8::5", true); err == nil {
		t.Fatal("expected IPv6 literal to be rejected")
	}
	if resolver.lookupIPCalls != 0 || resolver.lookupIPAddrCalls != 0 {
		t.Fatalf("literal IP should not trigger DNS lookups: LookupIP=%d LookupIPAddr=%d", resolver.lookupIPCalls, resolver.lookupIPAddrCalls)
	}
}

func TestResolveAndPickIPv4OnlyFailsWithoutAllowedIPv4(t *testing.T) {
	resolver := &stubIPResolver{ips: []net.IP{net.ParseIP("192.168.1.10"), net.ParseIP("2001:db8::5")}}
	guard := newSSRFGuard([]string{"*"})
	guard.resolver = resolver

	if _, err := guard.resolveAndPick(context.Background(), "provider.example", true); err == nil {
		t.Fatal("expected lookup without an allowed IPv4 address to fail")
	}
}

func TestResolveAndPickWithoutIPv4PolicyPreservesDualStack(t *testing.T) {
	resolver := &stubIPResolver{addrs: []net.IPAddr{{IP: net.ParseIP("2001:db8::5")}}}
	guard := newSSRFGuard([]string{"*"})
	guard.resolver = resolver

	ip, err := guard.resolveAndPick(context.Background(), "plugin.example", false)
	if err != nil {
		t.Fatalf("resolve dual-stack host: %v", err)
	}
	if ip.To4() != nil {
		t.Fatalf("resolved IP = %v, want IPv6 address", ip)
	}
	if resolver.lookupIPAddrCalls != 1 || resolver.lookupIPCalls != 0 {
		t.Fatalf("unexpected resolver calls: LookupIP=%d LookupIPAddr=%d", resolver.lookupIPCalls, resolver.lookupIPAddrCalls)
	}
}

func TestIsProviderTypeUsesCompleteHandler(t *testing.T) {
	record := &PluginRecord{Handlers: map[string][]string{
		"provider-type": {string(HandlerComplete), string(HandlerAuthInitiate)},
		"proxy-feed":    {"fetch_proxies"},
	}}

	if !isProviderType(record, "provider-type") {
		t.Fatal("type with required complete handler should be classified as a provider")
	}
	if isProviderType(record, "proxy-feed") {
		t.Fatal("proxy source without complete handler must not be classified as a provider")
	}
	if isProviderType(nil, "provider-type") {
		t.Fatal("nil plugin record must not be classified as a provider")
	}
}

func TestPluginHTTPClientIPv4PolicyUsesProviderType(t *testing.T) {
	providerClient := newPluginHTTPClient(&executionContext{providerType: true, allowHosts: []string{"*"}}, defaultHTTPTimeoutMs)
	if !providerClient.ipv4Only {
		t.Fatal("provider type should enable IPv4-only mode even without a provider instance ID")
	}

	proxySourceClient := newPluginHTTPClient(&executionContext{allowHosts: []string{"*"}}, defaultHTTPTimeoutMs)
	if proxySourceClient.ipv4Only {
		t.Fatal("non-provider context must preserve existing network behavior")
	}
}
