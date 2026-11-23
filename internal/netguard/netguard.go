// Package netguard owns the outbound-address policy: which destinations the
// router may dial on behalf of untrusted input (client-supplied URLs, plugin
// requests, third-party proxy feeds). Every outbound path that dials a
// caller-chosen host goes through this package, so the blocked ranges exist
// in one place only.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// ErrBlockedAddress reports a destination outside the public unicast space.
var ErrBlockedAddress = errors.New("destination address is not public")

// sharedAddressSpace is the carrier-grade NAT range (RFC 6598). netip has no
// predicate for it.
var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

// reservedPrefixes never host a public endpoint: benchmarking, IPv4 reserved,
// NAT64 and 6to4 (both embed arbitrary IPv4 targets).
var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("2002::/16"),
}

// IsPublicAddress reports whether address is a globally routable unicast
// address. IPv4-mapped IPv6 addresses are unmapped first.
func IsPublicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() {
		return false
	}
	if address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() || address.IsInterfaceLocalMulticast() ||
		address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	if sharedAddressSpace.Contains(address) {
		return false
	}
	for _, prefix := range reservedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

// IsPublicIP is IsPublicAddress for net.IP values.
func IsPublicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	return ok && IsPublicAddress(address)
}

// IsLocalHostname reports hostnames that name the local machine or a local
// zone. They are refused before resolution: their answers are host-defined.
func IsLocalHostname(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" {
		return true
	}
	for _, suffix := range []string{".localhost", ".local", ".internal", ".lan", ".home.arpa"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// PublicHostLiteral classifies host without DNS: literal reports whether host
// is an IP literal, public whether that literal (or, for hostnames, the name
// itself) may be dialed. Hostnames still need ResolvePublic at dial time.
func PublicHostLiteral(host string) (literal bool, public bool) {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if address, err := netip.ParseAddr(host); err == nil {
		return true, IsPublicAddress(address)
	}
	return false, host != "" && !IsLocalHostname(host)
}

// HostAllowList is a plugin @allow_host list: bare hostnames, or "*" for any
// public destination. The public-address policy applies under "*" too.
type HostAllowList struct {
	hosts    map[string]bool
	wildcard bool
}

// NewHostAllowList builds the matcher from @allow_host values.
func NewHostAllowList(hosts []string) HostAllowList {
	list := HostAllowList{hosts: map[string]bool{}}
	for _, host := range hosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "*" {
			list.wildcard = true
			continue
		}
		if host != "" {
			list.hosts[host] = true
		}
	}
	return list
}

// Check reports whether host may be dialed. IP literals pass only when
// public and only under "*"; hostnames must be listed (or "*" set).
// Dial-time resolution through ResolvePublic still refuses local zones and
// non-public answers.
func (list HostAllowList) Check(host string) error {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return fmt.Errorf("%w: empty host", ErrBlockedAddress)
	}
	if ip := net.ParseIP(host); ip != nil {
		if !IsPublicIP(ip) {
			return fmt.Errorf("dial to %q blocked: %w", host, ErrBlockedAddress)
		}
		if !list.wildcard {
			return fmt.Errorf("host %q is not in plugin allow-list: %w", host, ErrHostNotAllowed)
		}
		return nil
	}
	if !list.wildcard && !list.hosts[host] {
		return fmt.Errorf("host %q is not in plugin allow-list: %w", host, ErrHostNotAllowed)
	}
	return nil
}

// ErrHostNotAllowed reports a hostname outside the plugin allow-list.
var ErrHostNotAllowed = errors.New("host is not allowed")

// Resolver is the DNS surface ResolvePublic needs; net.DefaultResolver
// satisfies it.
type Resolver interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

// ResolvePublic resolves host and returns the first public address. IP
// literals are checked directly. ipv4Only restricts both literals and
// answers to IPv4. Callers dial the returned IP, never the name again, which
// closes DNS rebinding between check and dial.
func ResolvePublic(ctx context.Context, resolver Resolver, host string, ipv4Only bool) (net.IP, error) {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return nil, fmt.Errorf("%w: empty host", ErrBlockedAddress)
	}
	if ip := net.ParseIP(host); ip != nil {
		if ipv4Only {
			if ip = ip.To4(); ip == nil {
				return nil, fmt.Errorf("%w: IPv6 address %q is not allowed", ErrBlockedAddress, host)
			}
		}
		if !IsPublicIP(ip) {
			return nil, fmt.Errorf("%w: %q", ErrBlockedAddress, host)
		}
		return ip, nil
	}
	if IsLocalHostname(host) {
		return nil, fmt.Errorf("%w: local hostname %q", ErrBlockedAddress, host)
	}
	network := "ip"
	if ipv4Only {
		network = "ip4"
	}
	ips, err := resolver.LookupIP(ctx, network, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}
	for _, candidate := range ips {
		if ipv4Only {
			candidate = candidate.To4()
		}
		if candidate != nil && IsPublicIP(candidate) {
			return candidate, nil
		}
	}
	return nil, fmt.Errorf("%w: every address of %q is non-public", ErrBlockedAddress, host)
}

// DialContext returns a dial function that resolves the target through
// ResolvePublic and dials the pinned address.
func DialContext(dialer *net.Dialer, resolver Resolver) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ip, err := ResolvePublic(ctx, resolver, host, false)
		if err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
}

// maxRedirects bounds redirect chains on guarded clients.
const maxRedirects = 10

// NewHTTPClient returns a client that only reaches public http(s)
// destinations: every dial pins a validated public address, every redirect
// hop re-checks scheme and host, and environment proxies are ignored (a
// proxy would dial the target on the client's behalf, bypassing the pin).
func NewHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           DialContext(dialer, net.DefaultResolver),
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			return CheckURL(request.URL)
		},
	}
}

// CheckURL validates scheme and host of one URL without DNS. Dial-time
// resolution still applies through the guarded transport.
func CheckURL(target *url.URL) error {
	if target == nil {
		return fmt.Errorf("%w: empty url", ErrBlockedAddress)
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed: only http/https", target.Scheme)
	}
	if _, public := PublicHostLiteral(target.Hostname()); !public {
		return fmt.Errorf("%w: %q", ErrBlockedAddress, target.Hostname())
	}
	return nil
}
