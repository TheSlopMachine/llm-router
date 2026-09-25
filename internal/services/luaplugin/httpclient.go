package luaplugin

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
	lua "github.com/yuin/gopher-lua"
)

// Plugin HTTP client budgets: one place for every timeout and size bound
// on the Lua request path.
const (
	defaultHTTPTimeoutMs = 60000
	minHTTPTimeoutMs     = 1000
	maxHTTPTimeoutMs     = 300000

	// maxResponseBody caps buffered upstream bodies.
	maxResponseBody = 16 << 20

	// streamErrBodyLimit caps the upstream body embedded in non-2xx stream
	// error messages.
	streamErrBodyLimit = 64 << 10
	// streamChunkSize is the read buffer for on_chunk mode.
	streamChunkSize = 32 << 10
	// streamScannerMin/Max bound the on_line scanner buffer.
	streamScannerMin = 64 << 10
	streamScannerMax = 1 << 20
)

var blockedCIDRs = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"100.64.0.0/10",
	"0.0.0.0/8",
	"::1/128",
	"::/128",
	"fc00::/7",
	"fe80::/10",
}

var blockedNets []*net.IPNet

func init() {
	for _, c := range blockedCIDRs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			blockedNets = append(blockedNets, n)
		}
	}
}

func ipBlocked(ip net.IP) bool {
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ssrfGuard validates every hop against the plugin allow-list and the
// hard private-range blocklist. The private-range check applies even
// when the plugin holds a wildcard allow_host.
type ssrfGuard struct {
	allowHosts map[string]bool
	wildcard   bool
	resolver   *net.Resolver
}

func newSSRFGuard(allowHosts []string) *ssrfGuard {
	g := &ssrfGuard{allowHosts: map[string]bool{}, resolver: net.DefaultResolver}
	for _, h := range allowHosts {
		if h == "*" {
			g.wildcard = true
			continue
		}
		g.allowHosts[strings.ToLower(strings.TrimSpace(h))] = true
	}
	return g
}

func (g *ssrfGuard) checkHost(host string) error {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if ip := net.ParseIP(host); ip != nil {
		if ipBlocked(ip) {
			return fmt.Errorf("dial to %q blocked: private/link-local address", host)
		}
		if !g.wildcard {
			return fmt.Errorf("host %q is not in plugin allow-list", host)
		}
		return nil
	}
	if !g.wildcard && !g.allowHosts[host] {
		return fmt.Errorf("host %q is not in plugin allow-list", host)
	}
	return nil
}

func (g *ssrfGuard) checkURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("scheme %q not allowed: only http/https", u.Scheme)
	}
	if err := g.checkHost(u.Hostname()); err != nil {
		return nil, err
	}
	return u, nil
}

// logSinkSafe logs through the exec context when a sink is wired.
func (ctx *execContext) logSinkSafe(msg string) {
	if ctx != nil && ctx.logSink != nil {
		ctx.logSink(ctx.pluginID, msg)
	}
}

// resolveAndPick resolves hostname and returns the first non-blocked IP.
// Every resolved IP in a blocked range is rejected, closing DNS rebinding
// between check and dial: dial targets the validated IP directly.
func (g *ssrfGuard) resolveAndPick(ctx context.Context, hostname string) (net.IP, error) {
	if ip := net.ParseIP(hostname); ip != nil {
		if ipBlocked(ip) {
			return nil, fmt.Errorf("dial to %q blocked: private/link-local address", hostname)
		}
		return ip, nil
	}
	addrs, err := g.resolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", hostname, err)
	}
	for _, a := range addrs {
		if !ipBlocked(a.IP) {
			return a.IP, nil
		}
	}
	return nil, fmt.Errorf("dial to %q blocked: all resolved addresses are private/link-local", hostname)
}

// pluginHTTPClient is the Go backing of llm_router.http_client.
type pluginHTTPClient struct {
	ctx    *execContext
	guard  *ssrfGuard
	client *http.Client
	// timeout bounds one attempt; every key attempt gets a fresh budget.
	timeout time.Duration
	// proxyClients caches one transport per resolved proxy URL: handlers
	// issuing several requests through one client (image fan-out loops)
	// reuse connections instead of rebuilding the transport per request.
	proxyMu      sync.Mutex
	proxyClients map[string]*http.Client
}

func newPluginHTTPClient(ctx *execContext, timeoutMs int) *pluginHTTPClient {
	c := &pluginHTTPClient{ctx: ctx, guard: newSSRFGuard(ctx.allowHosts), proxyClients: map[string]*http.Client{}}
	timeout := time.Duration(timeoutMs) * time.Millisecond
	c.timeout = timeout

	// Direct transport: the target host is validated against the plugin
	// allow-list and the dial pins the resolved IP, closing DNS rebinding.
	// Proxy transports build per request in doWithProxyRotation, after the
	// route is resolved: DNS resolution then happens at the proxy, so the
	// dial-time IP pinning is replaced by hostname checks only.
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		DialContext: func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if err := c.guard.checkHost(host); err != nil {
				return nil, err
			}
			ip, err := c.guard.resolveAndPick(dialCtx, host)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(dialCtx, network, net.JoinHostPort(ip.String(), port))
		},
		DialTLSContext: func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if err := c.guard.checkHost(host); err != nil {
				return nil, err
			}
			ip, err := c.guard.resolveAndPick(dialCtx, host)
			if err != nil {
				return nil, err
			}
			raw, err := dialer.DialContext(dialCtx, network, net.JoinHostPort(ip.String(), port))
			if err != nil {
				return nil, err
			}
			cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
			tlsConn := tls.Client(raw, cfg)
			if err := tlsConn.HandshakeContext(dialCtx); err != nil {
				raw.Close()
				return nil, err
			}
			return tlsConn, nil
		},
	}
	c.client = &http.Client{
		Timeout:       timeout,
		Transport:     transport,
		CheckRedirect: c.checkRedirect,
	}
	return c
}

// proxyClient returns the cached client for the currently resolved proxy,
// building its transport once. A build failure is a loud error: the request
// rotates to the next proxy or fails, it never falls back to direct.
func (c *pluginHTTPClient) proxyClient() (*http.Client, error) {
	c.proxyMu.Lock()
	defer c.proxyMu.Unlock()
	if client, ok := c.proxyClients[c.ctx.proxyURL]; ok {
		return client, nil
	}
	transport, err := proxypool.TransportFor(c.ctx.proxyURL, c.timeout)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: c.timeout, Transport: transport, CheckRedirect: c.checkRedirect}
	c.proxyClients[c.ctx.proxyURL] = client
	return client, nil
}

// doWithProxyRotation executes one plugin HTTP request. The picks resolve
// fresh per request; a failed proxied attempt moves to the next untried
// pick. Direct requests and an exhausted pick list surface the last error.
// Proxy-to-direct fallback never happens.
func (c *pluginHTTPClient) doWithProxyRotation(req *http.Request) (*http.Response, string, time.Time, error) {
	if err := c.ctx.beginRequest(req.Context()); err != nil {
		return nil, "", time.Time{}, err
	}
	for {
		client := c.client
		proxyID := c.ctx.proxyID
		if c.ctx.proxyURL != "" {
			pc, berr := c.proxyClient()
			if berr != nil {
				c.logProxyDebug("plugin proxy client build failed, rotating", proxyID, berr)
				if !c.ctx.rotateProxy() {
					return nil, "", time.Time{}, berr
				}
				continue
			}
			client = pc
		}
		if req.GetBody != nil {
			if body, gerr := req.GetBody(); gerr == nil {
				req.Body = body
			}
		}
		start := time.Now()
		resp, derr := client.Do(req)
		if derr == nil {
			c.ctx.lastProxyID = c.ctx.proxyID
			if proxyID != "" && c.ctx.logger != nil {
				c.ctx.logger.Debug("plugin http request succeeded", "proxy_id", proxyID)
			}
			return resp, c.ctx.proxyID, start, nil
		}
		if c.ctx.proxyURL == "" {
			return nil, "", time.Time{}, derr
		}
		c.logProxyDebug("plugin proxy attempt failed, rotating", proxyID, derr)
		if !c.ctx.rotateProxy() {
			return nil, "", time.Time{}, derr
		}
	}
}

func (c *pluginHTTPClient) logProxyDebug(msg, proxyID string, err error) {
	if c.ctx == nil || c.ctx.logger == nil || proxyID == "" {
		return
	}
	c.ctx.logger.Debug(msg, "proxy_id", proxyID, "error", err)
}

// checkRedirect validates every redirect hop against the plugin allow-list.
func (c *pluginHTTPClient) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("too many redirects")
	}
	if _, err := c.guard.checkURL(req.URL.String()); err != nil {
		return err
	}
	return nil
}

func (c *pluginHTTPClient) toLua(L *lua.LState) lua.LValue {
	ud := L.NewUserData()
	ud.Value = c
	mt := L.NewTypeMetatable("llm_router_http_client")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(),
		map[string]lua.LGFunction{
			"request": c.luaRequest,
			"stream":  c.luaStream,
		}))
	L.SetMetatable(ud, mt)
	return ud
}

func clientOf(L *lua.LState) *pluginHTTPClient {
	ud, ok := L.Get(1).(*lua.LUserData)
	if !ok {
		L.RaiseError("http client method called without client")
		return nil
	}
	c, ok := ud.Value.(*pluginHTTPClient)
	if !ok || c == nil {
		L.RaiseError("invalid http client")
		return nil
	}
	return c
}

func goCtxOf(c *pluginHTTPClient) context.Context {
	if c.ctx != nil && c.ctx.goCtx != nil {
		return c.ctx.goCtx
	}
	return context.Background()
}

func checkCustomHostHeader(L *lua.LState, c *pluginHTTPClient, headers map[string]string) {
	for k, v := range headers {
		if strings.EqualFold(k, "host") {
			if err := c.guard.checkHost(strings.TrimSpace(v)); err != nil {
				L.RaiseError("custom Host header blocked: %s", err.Error())
			}
		}
	}
}

func (c *pluginHTTPClient) buildRequest(L *lua.LState, arg *lua.LTable) *http.Request {
	method := strings.ToUpper(strings.TrimSpace(luaTableString(arg, "method", "GET")))
	rawURL := strings.TrimSpace(luaTableString(arg, "url", ""))
	if rawURL == "" {
		L.RaiseError("request: url is required")
		return nil
	}
	u, err := c.guard.checkURL(rawURL)
	if err != nil {
		L.RaiseError("request blocked: %s", err.Error())
		return nil
	}
	headers := luaTableStringMap(arg, "headers")
	checkCustomHostHeader(L, c, headers)
	var body io.Reader
	var bodyBytes []byte
	if v := arg.RawGetString("body"); v != lua.LNil {
		if s, ok := v.(lua.LString); ok {
			bodyBytes = []byte(s)
			body = bytes.NewReader(bodyBytes)
		} else {
			L.RaiseError("request: body must be a string")
			return nil
		}
	}
	req, err := http.NewRequestWithContext(goCtxOf(c), method, u.String(), body)
	if err != nil {
		L.RaiseError("request: %s", err.Error())
		return nil
	}
	if bodyBytes != nil {
		// Rotation replays the body on every proxy attempt.
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}
	for k, v := range headers {
		if strings.EqualFold(k, "host") {
			req.Host = strings.TrimSpace(v)
			continue
		}
		req.Header.Set(k, v)
	}
	return req
}

// luaRequest implements client:request({...}) -> (resp, err).
func (c *pluginHTTPClient) luaRequest(L *lua.LState) int {
	arg := L.CheckTable(2)
	req := c.buildRequest(L, arg)
	if req == nil {
		return 0
	}
	resp, _, _, err := c.doWithProxyRotation(req)
	if err != nil {
		// Contract is (resp, err): nil response first, error table second.
		L.Push(lua.LNil)
		pushLuaErr(L, "upstream", err.Error())
		return 2
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		L.Push(lua.LNil)
		pushLuaErr(L, "upstream", err.Error())
		return 2
	}
	out := L.NewTable()
	out.RawSetString("status", lua.LNumber(resp.StatusCode))
	hdrs := L.NewTable()
	for k, vv := range resp.Header {
		hdrs.RawSetString(strings.ToLower(k), lua.LString(strings.Join(vv, ", ")))
	}
	out.RawSetString("headers", hdrs)
	out.RawSetString("body", lua.LString(string(body)))
	L.Push(out)
	L.Push(lua.LNil)
	return 2
}

// luaStream implements client:stream({...}) -> (resp, err), feeding the
// upstream body to on_line (line mode) or on_chunk (raw bytes mode). resp
// carries {status, headers}; err follows the error contract, or nil.
// on_response(resp) optionally classifies the head before the body streams:
// its non-nil return aborts with that error table. Without a hook decision
// a non-2xx surfaces as upstream, never as a silent stream.
func (c *pluginHTTPClient) luaStream(L *lua.LState) int {
	arg := L.CheckTable(2)
	onLine := arg.RawGetString("on_line")
	onChunk := arg.RawGetString("on_chunk")
	if onLine == lua.LNil && onChunk == lua.LNil {
		L.RaiseError("stream: on_line or on_chunk callback is required")
		return 0
	}
	if onLine != lua.LNil {
		if _, ok := onLine.(*lua.LFunction); !ok {
			L.RaiseError("stream: on_line must be a function")
			return 0
		}
	}
	if onChunk != lua.LNil {
		if _, ok := onChunk.(*lua.LFunction); !ok {
			L.RaiseError("stream: on_chunk must be a function")
			return 0
		}
	}
	onResponse := arg.RawGetString("on_response")
	if onResponse != lua.LNil {
		if _, ok := onResponse.(*lua.LFunction); !ok {
			L.RaiseError("stream: on_response must be a function")
			return 0
		}
	}
	req := c.buildRequest(L, arg)
	if req == nil {
		return 0
	}
	resp, _, _, err := c.doWithProxyRotation(req)
	if err != nil {
		L.Push(lua.LNil)
		pushLuaErr(L, "upstream", err.Error())
		return 2
	}
	defer resp.Body.Close()
	head := responseHead(L, resp)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if hookErr := c.runOnResponse(L, onResponse, head); hookErr != nil {
			L.Push(lua.LNil)
			L.Push(hookErr)
			return 2
		}
	} else {
		// Error responses never stream: buffer the bounded body first so
		// the hook classifies with full context (status, headers, body).
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, streamErrBodyLimit))
		head.RawSetString("body", lua.LString(string(errBody)))
		if hookErr := c.runOnResponse(L, onResponse, head); hookErr != nil {
			L.Push(lua.LNil)
			L.Push(hookErr)
			return 2
		}
		L.Push(lua.LNil)
		pushLuaErr(L, "upstream", fmt.Sprintf("unexpected status %d: %s", resp.StatusCode, string(errBody)))
		return 2
	}
	if fn, ok := onChunk.(*lua.LFunction); ok {
		buf := make([]byte, streamChunkSize)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				if callErr := protectedCallback(L, fn, lua.LString(string(buf[:n]))); callErr != nil {
					L.Push(lua.LNil)
					pushLuaErr(L, "upstream", fmt.Sprintf("on_chunk failed: %s", callErr.Error()))
					return 2
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				L.Push(lua.LNil)
				pushLuaErr(L, "upstream", err.Error())
				return 2
			}
		}
		L.Push(head)
		L.Push(lua.LNil)
		return 2
	}
	fn := onLine.(*lua.LFunction)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, streamScannerMin), streamScannerMax)
	for scanner.Scan() {
		line := scanner.Text()
		if callErr := protectedCallback(L, fn, lua.LString(line)); callErr != nil {
			L.Push(lua.LNil)
			pushLuaErr(L, "upstream", fmt.Sprintf("on_line failed: %s", callErr.Error()))
			return 2
		}
	}
	if err := scanner.Err(); err != nil {
		L.Push(lua.LNil)
		pushLuaErr(L, "upstream", err.Error())
		return 2
	}
	L.Push(head)
	L.Push(lua.LNil)
	return 2
}

// responseHead renders the response status line as a Lua table for the
// on_response hook and the stream success value.
func responseHead(L *lua.LState, resp *http.Response) *lua.LTable {
	head := L.NewTable()
	head.RawSetString("status", lua.LNumber(resp.StatusCode))
	hdrs := L.NewTable()
	for k, vv := range resp.Header {
		hdrs.RawSetString(strings.ToLower(k), lua.LString(strings.Join(vv, ", ")))
	}
	head.RawSetString("headers", hdrs)
	return head
}

// runOnResponse invokes the on_response hook (LNil = absent) with the
// response head. A nil hook return accepts the head; a table return aborts
// the stream with that error. Anything else is a plugin bug and raises.
func (c *pluginHTTPClient) runOnResponse(L *lua.LState, hook lua.LValue, head *lua.LTable) lua.LValue {
	if hook == lua.LNil {
		return nil
	}
	fn, ok := hook.(*lua.LFunction)
	if !ok {
		L.RaiseError("stream: on_response must be a function")
		return nil
	}
	top := L.GetTop()
	L.Push(fn)
	L.Push(head)
	if err := L.PCall(1, 1, nil); err != nil {
		L.SetTop(top)
		L.RaiseError("stream: on_response failed: %s", err.Error())
		return nil
	}
	ret := L.Get(-1)
	L.Pop(1)
	if ret == lua.LNil {
		return nil
	}
	tbl, ok := ret.(*lua.LTable)
	if !ok {
		L.RaiseError("stream: on_response must return a table or nil")
		return nil
	}
	return tbl
}

func protectedCallback(L *lua.LState, fn *lua.LFunction, args ...lua.LValue) error {
	top := L.GetTop()
	L.Push(fn)
	for _, a := range args {
		L.Push(a)
	}
	err := L.PCall(len(args), 0, nil)
	L.SetTop(top)
	return err
}

// pushLuaErr pushes an error-table {type=, message=} onto the stack.
func pushLuaErr(L *lua.LState, errType, message string) {
	tbl := L.NewTable()
	tbl.RawSetString("type", lua.LString(errType))
	tbl.RawSetString("message", lua.LString(message))
	L.Push(tbl)
}

func luaTableString(tbl *lua.LTable, key, def string) string {
	if v := tbl.RawGetString(key); v != lua.LNil {
		if s, ok := v.(lua.LString); ok {
			return string(s)
		}
	}
	return def
}

func luaTableStringMap(tbl *lua.LTable, key string) map[string]string {
	out := map[string]string{}
	v := tbl.RawGetString(key)
	hdrs, ok := v.(*lua.LTable)
	if !ok {
		return out
	}
	var keys []string
	hdrs.ForEach(func(k, _ lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			keys = append(keys, string(ks))
		}
	})
	sort.Strings(keys)
	for _, k := range keys {
		if s, ok := hdrs.RawGetString(k).(lua.LString); ok {
			out[k] = string(s)
		}
	}
	return out
}
