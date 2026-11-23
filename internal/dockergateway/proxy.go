// Package dockergateway is the program the router runs in one gateway
// container per plugin. Plugin containers sit on internal networks with no
// route out. The gateway joins those networks and one egress bridge. Its
// proxy forwards HTTP and CONNECT traffic only to the plugin @allow_host
// list, dialing public addresses only. Its relay command bridges stdin and
// stdout to one container port for route-less dials from the router.
package dockergateway

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/netguard"
)

// Environment variables the router sets on the gateway container.
const (
	EnvironmentAllowHosts = "LLMR_ALLOW_HOSTS"
	EnvironmentProxyToken = "LLMR_PROXY_TOKEN"
	EnvironmentListen     = "LLMR_LISTEN"
)

// ProxyUser is the user name of the proxy credentials.
const ProxyUser = "llmr"

// DefaultListen is the proxy address inside the gateway container.
const DefaultListen = ":3128"

// dialTimeout bounds one upstream dial.
const dialTimeout = 15 * time.Second

// Event is one JSON line the gateway writes to stdout. The router reads
// gateway logs and turns blocked hosts into events.
type Event struct {
	Event  string `json:"event"`
	Host   string `json:"host"`
	Reason string `json:"reason"`
}

// EventEgressBlocked names a refused destination.
const EventEgressBlocked = "egress_blocked"

// Proxy is the allow-list forward proxy.
type Proxy struct {
	allowList netguard.HostAllowList
	token     string
	resolver  netguard.Resolver
	output    io.Writer
	outputMu  sync.Mutex
	transport *http.Transport
}

// NewProxy builds a proxy. token is the password of the ProxyUser
// credentials every request must carry.
func NewProxy(allowHosts []string, token string, output io.Writer) *Proxy {
	proxy := &Proxy{
		allowList: netguard.NewHostAllowList(allowHosts),
		token:     token,
		resolver:  net.DefaultResolver,
		output:    output,
	}
	proxy.transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           proxy.dialAllowed,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          16,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   dialTimeout,
		ExpectContinueTimeout: time.Second,
	}
	return proxy
}

// emit writes one event line. Write failures are ignored: stdout is the
// container log and has no other reader.
func (proxy *Proxy) emit(event Event) {
	line, err := json.Marshal(event)
	if err != nil {
		return
	}
	proxy.outputMu.Lock()
	defer proxy.outputMu.Unlock()
	_, _ = proxy.output.Write(append(line, '\n'))
}

// dialAllowed checks the host against the allow-list, resolves it to a
// public address and dials that address.
func (proxy *Proxy) dialAllowed(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if err := proxy.allowList.Check(host); err != nil {
		proxy.emit(Event{Event: EventEgressBlocked, Host: host, Reason: blockReason(err)})
		return nil, err
	}
	ip, err := netguard.ResolvePublic(ctx, proxy.resolver, host, false)
	if err != nil {
		proxy.emit(Event{Event: EventEgressBlocked, Host: host, Reason: blockReason(err)})
		return nil, err
	}
	dialer := net.Dialer{Timeout: dialTimeout}
	return dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
}

func blockReason(err error) string {
	switch {
	case errors.Is(err, netguard.ErrHostNotAllowed):
		return "not_allowed"
	case errors.Is(err, netguard.ErrBlockedAddress):
		return "not_public"
	default:
		return "resolve_failed"
	}
}

// authorized checks the Proxy-Authorization header.
func (proxy *Proxy) authorized(request *http.Request) bool {
	header := request.Header.Get("Proxy-Authorization")
	scheme, encoded, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Basic") {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return false
	}
	user, password, found := strings.Cut(string(decoded), ":")
	return found && user == ProxyUser && subtle.ConstantTimeCompare([]byte(password), []byte(proxy.token)) == 1
}

// hopHeaders are connection-scoped headers a proxy never forwards.
var hopHeaders = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

// ServeHTTP handles CONNECT tunnels and absolute-form HTTP requests.
func (proxy *Proxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if !proxy.authorized(request) {
		writer.Header().Set("Proxy-Authenticate", `Basic realm="llm-router"`)
		http.Error(writer, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	if request.Method == http.MethodConnect {
		proxy.serveConnect(writer, request)
		return
	}
	if request.URL.Scheme != "http" || request.URL.Host == "" {
		http.Error(writer, "only absolute http:// requests and CONNECT are supported", http.StatusBadRequest)
		return
	}
	outgoing := request.Clone(request.Context())
	outgoing.RequestURI = ""
	for _, name := range hopHeaders {
		outgoing.Header.Del(name)
	}
	response, err := proxy.transport.RoundTrip(outgoing)
	if err != nil {
		http.Error(writer, "upstream request failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	for _, name := range hopHeaders {
		response.Header.Del(name)
	}
	for name, values := range response.Header {
		for _, value := range values {
			writer.Header().Add(name, value)
		}
	}
	writer.WriteHeader(response.StatusCode)
	_, _ = io.Copy(writer, response.Body)
}

func (proxy *Proxy) serveConnect(writer http.ResponseWriter, request *http.Request) {
	address := request.Host
	if _, _, err := net.SplitHostPort(address); err != nil {
		http.Error(writer, "CONNECT target must be host:port", http.StatusBadRequest)
		return
	}
	upstream, err := proxy.dialAllowed(request.Context(), "tcp", address)
	if err != nil {
		http.Error(writer, "destination refused: "+err.Error(), http.StatusForbidden)
		return
	}
	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(writer, "connection cannot be hijacked", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		client.Close()
		upstream.Close()
		return
	}
	if buffered != nil && buffered.Reader.Buffered() > 0 {
		pending := make([]byte, buffered.Reader.Buffered())
		_, _ = buffered.Read(pending)
		_, _ = upstream.Write(pending)
	}
	Pipe(client, upstream)
}

// Pipe copies both directions until both end, half-closing each side when
// its source ends.
func Pipe(left, right net.Conn) {
	var group sync.WaitGroup
	group.Add(2)
	copyHalf := func(destination, source net.Conn) {
		defer group.Done()
		_, _ = io.Copy(destination, source)
		if closer, ok := destination.(interface{ CloseWrite() error }); ok {
			_ = closer.CloseWrite()
		} else {
			_ = destination.Close()
		}
	}
	go copyHalf(left, right)
	go copyHalf(right, left)
	group.Wait()
	left.Close()
	right.Close()
}

// Serve runs the proxy on address until ctx ends.
func (proxy *Proxy) Serve(ctx context.Context, address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen %s: %w", address, err)
	}
	server := &http.Server{Handler: proxy, ReadHeaderTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
