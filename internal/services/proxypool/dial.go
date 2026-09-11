package proxypool

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/net/proxy"
)

// TransportFor builds an http.Transport routing through the given proxy.
// Supports http, https, socks4 (with 4a remote DNS) and socks5.
func TransportFor(proxyURL string, timeout time.Duration) (*http.Transport, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url: %w", err)
	}
	dialer := &net.Dialer{Timeout: timeout}

	switch u.Scheme {
	case "http", "https":
		return &http.Transport{
			Proxy:       http.ProxyURL(u),
			DialContext: dialer.DialContext,
		}, nil

	case "socks5":
		var auth *proxy.Auth
		if u.User != nil {
			pw, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: pw}
		}
		d, err := proxy.SOCKS5("tcp", u.Host, auth, dialer)
		if err != nil {
			return nil, err
		}
		return &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				type ctxDialer interface {
					DialContext(ctx context.Context, network, addr string) (net.Conn, error)
				}
				if cd, ok := d.(ctxDialer); ok {
					return cd.DialContext(ctx, network, addr)
				}
				type res struct {
					conn net.Conn
					err  error
				}
				ch := make(chan res, 1)
				go func() {
					c, err := d.Dial(network, addr)
					ch <- res{c, err}
				}()
				select {
				case r := <-ch:
					return r.conn, r.err
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		}, nil

	case "socks4":
		return &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return socks4Dial(ctx, dialer, u, addr)
			},
		}, nil
	}
	return nil, fmt.Errorf("unsupported proxy protocol %q", u.Scheme)
}

// socks4Dial connects through a SOCKS4/4a proxy. Empty user id; SOCKS4a
// remote DNS when the target is a hostname.
func socks4Dial(ctx context.Context, dialer *net.Dialer, proxyURL *url.URL, addr string) (net.Conn, error) {
	conn, err := dialer.DialContext(ctx, "tcp", proxyURL.Host)
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		conn.Close()
		return nil, err
	}
	var port uint16
	if p, err := strconvParseUint16(portStr); err != nil {
		conn.Close()
		return nil, err
	} else {
		port = p
	}

	req := []byte{0x04, 0x01}
	req = binary.BigEndian.AppendUint16(req, port)
	ip := net.ParseIP(host).To4()
	socks4a := ip == nil
	if socks4a {
		req = append(req, 0, 0, 0, 1) // 0.0.0.1 marks SOCKS4a
	} else {
		req = append(req, ip...)
	}
	user := ""
	if proxyURL.User != nil {
		user = proxyURL.User.Username()
	}
	req = append(req, []byte(user)...)
	req = append(req, 0)
	if socks4a {
		req = append(req, []byte(host)...)
		req = append(req, 0)
	}
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}

	r := bufio.NewReader(conn)
	head := make([]byte, 8)
	if _, err := io.ReadFull(r, head); err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks4: read reply: %w", err)
	}
	if head[1] != 0x5a {
		conn.Close()
		return nil, fmt.Errorf("socks4: request rejected (code %d)", head[1])
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func strconvParseUint16(s string) (uint16, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 65535 {
		return 0, fmt.Errorf("invalid port %q", s)
	}
	return uint16(n), nil
}

// probeThrough issues a GET to checkURL through the proxy.
func probeThrough(ctx context.Context, proxyURL, checkURL string) error {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return err
	}
	transport, err := TransportFor(u.String(), 10*time.Second)
	if err != nil {
		return err
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("check url status %d", resp.StatusCode)
	}
	return nil
}
