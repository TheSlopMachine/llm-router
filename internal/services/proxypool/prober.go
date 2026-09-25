package proxypool

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
	"golang.org/x/net/proxy"
)

type probeResult struct {
	handshakeMs int64
	speedKbps   int64
}

// probe runs the two-stage measurement: CONNECT tunnel, then download.
func (s *Service) probe(ctx context.Context, proxyURL string) (probeResult, error) {
	handshakeMs, err := s.probeHandshake(ctx, proxyURL)
	if err != nil {
		return probeResult{}, err
	}
	speedKbps, err := s.probeDownload(ctx, proxyURL)
	if err != nil {
		return probeResult{}, err
	}
	return probeResult{handshakeMs: handshakeMs, speedKbps: speedKbps}, nil
}

// probeHandshake opens a CONNECT tunnel to the speed host and closes it.
func (s *Service) probeHandshake(ctx context.Context, proxyURL string) (int64, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return 0, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, s.hsTimeout)
	defer cancel()
	start := time.Now()
	switch u.Scheme {
	case "http", "https":
		conn, err := (&net.Dialer{}).DialContext(probeCtx, "tcp", u.Host)
		if err != nil {
			return 0, err
		}
		defer conn.Close()
		if dl, ok := probeCtx.Deadline(); ok {
			_ = conn.SetDeadline(dl)
		}
		_, _ = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", s.HandshakeHost, s.HandshakeHost)
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			return 0, err
		}
		if !strings.Contains(line, "200") {
			return 0, fmt.Errorf("tunnel rejected: %s", strings.TrimSpace(line))
		}
	case "socks5":
		var auth *proxy.Auth
		if u.User != nil {
			pw, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: pw}
		}
		d, err := proxy.SOCKS5("tcp", u.Host, auth, &net.Dialer{})
		if err != nil {
			return 0, err
		}
		// x/net proxy dialers ignore contexts: bound the dial by the
		// probe deadline explicitly instead of hanging on OS timeouts.
		type dialResult struct {
			conn net.Conn
			err  error
		}
		ch := make(chan dialResult, 1)
		go func() {
			conn, err := d.Dial("tcp", s.HandshakeHost)
			ch <- dialResult{conn, err}
		}()
		select {
		case r := <-ch:
			if r.err != nil {
				return 0, r.err
			}
			r.conn.Close()
		case <-probeCtx.Done():
			return 0, probeCtx.Err()
		}
	case "socks4":
		conn, err := socks4Dial(probeCtx, &net.Dialer{}, u, s.HandshakeHost)
		if err != nil {
			return 0, err
		}
		conn.Close()
	default:
		return 0, fmt.Errorf("unsupported proxy protocol %q", u.Scheme)
	}
	return time.Since(start).Milliseconds(), nil
}

// countingReader tallies downloaded bytes.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// probeDownload fetches the speed test file through the proxy. A timeout
// with partial data still yields the achieved speed; zero bytes is an error.
func (s *Service) probeDownload(ctx context.Context, proxyURL string) (int64, error) {
	transport, err := TransportFor(proxyURL, handshakeTimeout)
	if err != nil {
		return 0, err
	}
	defer transport.CloseIdleConnections()
	probeCtx, cancel := context.WithTimeout(ctx, s.dlTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, s.CheckURL, nil)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("speed url status %d", resp.StatusCode)
	}
	counter := &countingReader{r: resp.Body}
	_, copyErr := io.Copy(io.Discard, counter)
	elapsed := time.Since(start)
	if counter.n == 0 {
		if copyErr != nil {
			return 0, copyErr
		}
		return 0, fmt.Errorf("speed test returned no bytes")
	}
	if elapsed <= 0 {
		elapsed = time.Millisecond
	}
	// Kilobits per second: bytes * 8 bits / elapsed ms.
	return counter.n * 8 / elapsed.Milliseconds(), nil
}

func (s *Service) locationCount(location string) (int, error) {
	pooled, err := s.proxies.ListFiltered(func(p *models.Proxy) bool {
		return p.Source != ManualSource && p.Location == location
	})
	if err != nil {
		return 0, err
	}
	return len(pooled), nil
}

func (s *Service) slowestIn(location string) (*models.Proxy, error) {
	pooled, err := s.proxies.ListFiltered(func(p *models.Proxy) bool {
		return p.Source != ManualSource && p.Location == location
	})
	if err != nil {
		return nil, err
	}
	if len(pooled) == 0 {
		return nil, nil
	}
	sort.Slice(pooled, func(i, j int) bool { return lessProxy(pooled[j], pooled[i]) })
	return pooled[0], nil
}

// updateRecord applies the update rule to an already-pooled proxy:
// dead entries are deleted; slow entries are deleted once their location
// holds more than the cap; the rest store the fresh numbers.
func (s *Service) updateRecord(ctx context.Context, p *models.Proxy, minSpeedKbps int64, maxPerLocation int) {
	res, err := s.probe(ctx, p.URL)
	if err != nil {
		_ = s.Delete(p.ID)
		return
	}
	n, err := s.locationCount(p.Location)
	if err == nil && res.speedKbps < minSpeedKbps && n > maxPerLocation {
		_ = s.Delete(p.ID)
		return
	}
	p.HandshakeMs = res.handshakeMs
	p.SpeedKbps = res.speedKbps
	p.LastCheckAt = util.Now()
	_ = s.proxies.Put(p.ID, p)
}

// Checking reports whether probe work is running right now (a rotation or
// candidate adds). Dashboard polling keys off it.
func (s *Service) Checking() bool { return s.checking.Load() }

// notify wakes RankWait waiters: the pool changed, re-evaluate.
func (s *Service) notify() {
	s.bcastMu.Lock()
	defer s.bcastMu.Unlock()
	close(s.bcastCh)
	s.bcastCh = make(chan struct{})
}

func (s *Service) changed() <-chan struct{} {
	s.bcastMu.Lock()
	defer s.bcastMu.Unlock()
	return s.bcastCh
}

// UpdateOne re-probes one pooled proxy by ID.
func (s *Service) UpdateOne(ctx context.Context, id string) (*models.Proxy, error) {
	p, err := s.proxies.Get(id)
	if err != nil {
		return nil, err
	}
	minSpeedKbps, maxPerLocation := s.settings()
	s.updateRecord(ctx, p, minSpeedKbps, maxPerLocation)
	updated, err := s.proxies.Get(id)
	if err != nil || updated == nil {
		return nil, fmt.Errorf("proxy %s culled by rotation", id)
	}
	return updated, nil
}
