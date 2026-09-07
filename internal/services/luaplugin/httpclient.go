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
	"time"

	lua "github.com/yuin/gopher-lua"
)

// maxResponseBody caps buffered upstream bodies.
const maxResponseBody = 16 << 20

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

// pluginHTTPClient is the Go backing of llm_router.create_http_client.
type pluginHTTPClient struct {
	ctx    *execContext
	guard  *ssrfGuard
	client *http.Client
	goCtx  context.Context
}

func newPluginHTTPClient(ctx *execContext, timeoutMs int) *pluginHTTPClient {
	c := &pluginHTTPClient{ctx: ctx, guard: newSSRFGuard(ctx.allowHosts)}
	timeout := time.Duration(timeoutMs) * time.Millisecond
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
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if _, err := c.guard.checkURL(req.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}
	return c
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
	if c.goCtx != nil {
		return c.goCtx
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
	if v := arg.RawGetString("body"); v != lua.LNil {
		if s, ok := v.(lua.LString); ok {
			body = bytes.NewReader([]byte(s))
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
	resp, err := c.client.Do(req)
	if err != nil {
		pushLuaErr(L, "upstream", err.Error())
		L.Push(lua.LNil)
		return 2
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		pushLuaErr(L, "upstream", err.Error())
		L.Push(lua.LNil)
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

// luaStream implements client:stream({...}) -> err-or-nil, feeding the
// upstream body to on_line (line mode) or on_chunk (raw bytes mode).
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
	req := c.buildRequest(L, arg)
	if req == nil {
		return 0
	}
	resp, err := c.client.Do(req)
	if err != nil {
		pushLuaErr(L, "upstream", err.Error())
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		pushLuaErr(L, "upstream", fmt.Sprintf("unexpected status %d: %s", resp.StatusCode, string(body)))
		return 1
	}
	if fn, ok := onChunk.(*lua.LFunction); ok {
		buf := make([]byte, 32<<10)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				if callErr := protectedCallback(L, fn, lua.LString(string(buf[:n]))); callErr != nil {
					pushLuaErr(L, "upstream", fmt.Sprintf("on_chunk failed: %s", callErr.Error()))
					return 1
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				pushLuaErr(L, "upstream", err.Error())
				return 1
			}
		}
		L.Push(lua.LNil)
		return 1
	}
	fn := onLine.(*lua.LFunction)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if callErr := protectedCallback(L, fn, lua.LString(line)); callErr != nil {
			pushLuaErr(L, "upstream", fmt.Sprintf("on_line failed: %s", callErr.Error()))
			return 1
		}
	}
	if err := scanner.Err(); err != nil {
		pushLuaErr(L, "upstream", err.Error())
		return 1
	}
	L.Push(lua.LNil)
	return 1
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
