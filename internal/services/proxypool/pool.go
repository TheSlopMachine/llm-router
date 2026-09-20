// Package proxypool implements the outbound proxy pool.
//
// Design: two-stage probe (CONNECT handshake, download speed), one live
// state per proxy-provider pair (rate limit, block), demand-driven fetch.
// Presence in the bucket means the proxy answered the last probe;
// dead proxies are deleted, never flagged.
package proxypool

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
	"github.com/TheSlopMachine/llm-router/internal/util"
	"golang.org/x/net/proxy"
)

// ManualSource marks user-registered proxies.
const ManualSource = "manual"

// ListSource builds the source tag for a list plugin.
func ListSource(typeKey string) string { return "list:" + typeKey }

// checkConcurrency bounds parallel probes. Each probe egresses through its
// own proxy, so per-IP rate limits apply per proxy, never globally.
const checkConcurrency = 64

// handshakeTimeout bounds the CONNECT tunnel stage.
const handshakeTimeout = 10 * time.Second

// downloadTimeout bounds the speed test stage.
const downloadTimeout = 15 * time.Second

// fetchWindowSize caps candidates covered per source fetch. Coverage
// proceeds in fetchChunkSize slices while shortfall persists, so a full
// pool costs zero probes and a cold pool costs at most one window.
const fetchWindowSize = 1500

// fetchChunkSize is one probe batch inside a fetch window.
const fetchChunkSize = 150

// demandWriteThrottle bounds demand bucket writes per region.
const demandWriteThrottle = time.Hour

// demandExpiry drops demand unseen for this long. Defaults never expire.
const demandExpiry = 48 * time.Hour

// DefaultRegions seeds demand so the pool fills even before the first
// whitelisted request arrives.
var DefaultRegions = []string{"US", "DE", "NL", "GB", "FR", "CA"}

// ErrBusy reports a rotation already in progress. Rotation runs under a
// semaphore of size one: a tick or a manual refresh arriving while another
// pass runs is skipped, never queued.
var ErrBusy = errors.New("proxypool: rotation already in progress")

// ErrNoProxies reports a settled pool with no usable pick: nothing is
// running that could add one, so waiting longer is pointless.
var ErrNoProxies = errors.New("proxypool: no usable proxy")

// recheckInterval bounds staleness while waiting for a proxy (pair limit
// expiry, scheduled fetch). Waiting never gives up on its own; the request
// context still aborts it.
const recheckInterval = 5 * time.Second

var supportedProtocols = map[string]bool{"http": true, "https": true, "socks4": true, "socks5": true}

// Service manages the proxy pool.
type Service struct {
	proxies *repository.Repository[models.Proxy]
	limits  *repository.Repository[models.ProxyLimit]
	regions *repository.Repository[models.ActiveRegion]
	meta    *repository.Repository[sourceFetchMeta]
	// CheckURL is the speed test download endpoint.
	CheckURL string
	// HandshakeHost is the CONNECT tunnel target (host:port).
	HandshakeHost string
	// hsTimeout bounds the CONNECT tunnel stage; dlTimeout the download.
	// Unexported so tests can shrink them; production uses the consts.
	hsTimeout time.Duration
	dlTimeout time.Duration

	mu             sync.Mutex
	minSpeedKbps   int64
	maxPerLocation int
	rotationSem    chan struct{}
	sourceStatus   map[string]*sourceState
	// checking reports live probe work (rotation or candidate adds).
	// It drives the dashboard busy signal; transitions own the details.
	checking atomic.Bool
	// bcastCh wakes RankWait waiters on every pool change. It is closed
	// and replaced under bcastMu; waiters hold the channel, never the lock.
	bcastMu sync.Mutex
	bcastCh chan struct{}
}

// sourceFetchMeta persists the last fetch outcome per source. Offset rotates
// the fetch window across fetches.
type sourceFetchMeta struct {
	Total       int       `json:"total"`
	Offset      int       `json:"offset"`
	LastFetchAt time.Time `json:"last_fetch_at"`
	LastError   string    `json:"last_error,omitempty"`
}

// sourceState is the dashboard-facing runtime state of one source.
type sourceState struct {
	status      string
	total       int
	lastFetchAt time.Time
	lastError   string
}

// Source lifecycle statuses reported to the dashboard.
const (
	SourceStatusIdle     = "idle"
	SourceStatusFetching = "fetching"
	SourceStatusAdding   = "adding"
	SourceStatusRotating = "rotating"
)

// SourceInfo is the dashboard-facing snapshot of one source.
type SourceInfo struct {
	Key         string    `json:"key"`
	Status      string    `json:"status"`
	Total       int       `json:"total"`
	Pooled      int       `json:"pooled"`
	LastFetchAt time.Time `json:"last_fetch_at,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
}

// New constructs the proxy pool service with default pool settings.
// Tune with SetConfig once RouterConfiguration is loaded.
func New(database *db.DB) *Service {
	return &Service{
		proxies:        repository.New[models.Proxy](database, db.BucketProxies, "proxy"),
		limits:         repository.New[models.ProxyLimit](database, db.BucketProxyLimits, "proxy_limit"),
		regions:        repository.New[models.ActiveRegion](database, db.BucketActiveRegions, "active_region"),
		meta:           repository.New[sourceFetchMeta](database, db.BucketProxySourceMeta, "proxy_source_meta"),
		CheckURL:       "https://speed.cloudflare.com/__down?bytes=1048576",
		HandshakeHost:  "speed.cloudflare.com:443",
		hsTimeout:      handshakeTimeout,
		dlTimeout:      downloadTimeout,
		minSpeedKbps:   models.DefaultMinDownloadSpeedKbps,
		maxPerLocation: models.DefaultMaxProxiesPerLocation,
		rotationSem:    make(chan struct{}, 1),
		sourceStatus:   map[string]*sourceState{},
		bcastCh:        make(chan struct{}),
	}
}

// SetConfig installs the pool settings from RouterConfiguration.
func (s *Service) SetConfig(minSpeedKbps int64, maxPerLocation int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if minSpeedKbps > 0 {
		s.minSpeedKbps = minSpeedKbps
	}
	if maxPerLocation > 0 {
		s.maxPerLocation = maxPerLocation
	}
}

func (s *Service) settings() (minSpeedKbps int64, maxPerLocation int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.minSpeedKbps, s.maxPerLocation
}

// ─────────────────────────────────────────────
// Demand
// ─────────────────────────────────────────────

// NoteDemand records whitelisted regions as demanded. Throttled to one
// bucket write per region per hour; reads stay in the bucket.
func (s *Service) NoteDemand(regions []string) {
	now := util.Now()
	for _, r := range regions {
		r = NormalizeCountryCode(r)
		if r == "" {
			continue
		}
		if existing, err := s.regions.Get(r); err == nil && existing != nil {
			if now.Sub(existing.LastSeen) < demandWriteThrottle {
				continue
			}
		}
		_ = s.regions.Put(r, &models.ActiveRegion{Region: r, LastSeen: now})
	}
}

// demandSet returns defaults merged with fresh observed demand.
func (s *Service) demandSet() map[string]bool {
	demand := map[string]bool{}
	for _, r := range DefaultRegions {
		demand[r] = true
	}
	now := util.Now()
	rows, err := s.regions.List()
	if err != nil {
		return demand
	}
	for _, row := range rows {
		if now.Sub(row.LastSeen) < demandExpiry {
			demand[row.Region] = true
		}
	}
	return demand
}

// fastCount counts pooled proxies of a location at or above the speed
// floor, manual ones included: demand is satisfied by anything choosable.
func (s *Service) fastCount(location string, minSpeedKbps int64) int {
	pooled, err := s.proxies.ListFiltered(func(p *models.Proxy) bool {
		return p.Location == location && p.SpeedKbps >= minSpeedKbps
	})
	if err != nil {
		return 0
	}
	return len(pooled)
}

// NeedsSearch reports whether any demanded region lacks fast proxies.
// True means the next scheduled fetch should run; false pauses it.
func (s *Service) NeedsSearch() bool {
	minSpeedKbps, maxPerLocation := s.settings()
	for region := range s.demandSet() {
		if s.fastCount(region, minSpeedKbps) < maxPerLocation {
			return true
		}
	}
	return false
}

// sweepDemand drops demand unseen past expiry. Defaults are not stored,
// so they never expire.
func (s *Service) sweepDemand() {
	now := util.Now()
	rows, err := s.regions.List()
	if err != nil {
		return
	}
	for _, row := range rows {
		if now.Sub(row.LastSeen) >= demandExpiry {
			_ = s.regions.Delete(row.Region)
		}
	}
}

// ─────────────────────────────────────────────
// Choosing
// ─────────────────────────────────────────────

// Pick is one ranked proxy candidate.
type Pick struct {
	ID  string
	URL string
}

// Rank returns the ordered proxy list for a provider call: whitelist and
// pair-state filtered, fastest first. Disabled mode returns nil (direct).
// Manual mode follows the ids order and fails loudly when nothing usable
// is pooled. An empty whitelist allows any location.
func (s *Service) Rank(whitelist []string, mode string, ids []string, provider string) ([]Pick, error) {
	if mode != models.ProxyModeDisabled {
		s.NoteDemand(whitelist)
	}
	switch mode {
	case models.ProxyModeManual:
		return s.rankManual(ids, provider)
	case models.ProxyModeAuto:
		return s.rankAuto(whitelist, provider), nil
	default:
		return nil, nil
	}
}

// RankWait is Rank for auto mode with waiting: empty picks while probe
// work runs or a fetch is still due block until a pick appears, the pool
// settles empty (ErrNoProxies), or the context aborts. Providers wait for
// ready or no-proxies instead of silently going direct.
func (s *Service) RankWait(ctx context.Context, whitelist []string, ids []string, provider string) ([]Pick, error) {
	for {
		picks, err := s.Rank(whitelist, models.ProxyModeAuto, ids, provider)
		if err != nil {
			return nil, err
		}
		if len(picks) > 0 {
			return picks, nil
		}
		if !s.Checking() && !s.NeedsSearch() {
			return nil, ErrNoProxies
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.changed():
		case <-time.After(recheckInterval):
		}
	}
}

// Choose returns the fastest usable proxy, or ("", "", nil) for direct.
func (s *Service) Choose(whitelist []string, mode string, ids []string, provider string) (string, string, error) {
	picks, err := s.Rank(whitelist, mode, ids, provider)
	if err != nil {
		return "", "", err
	}
	if len(picks) == 0 {
		return "", "", nil
	}
	return picks[0].ID, picks[0].URL, nil
}

func (s *Service) usable(p *models.Proxy, allow map[string]bool, provider string) bool {
	if allow != nil && !allow[p.Location] {
		return false
	}
	if lim, err := s.limits.Get(limitID(p.ID, provider)); err == nil && lim != nil {
		if lim.Blocked {
			return false
		}
		if lim.ResetsAt != nil && lim.ResetsAt.After(util.Now()) {
			return false
		}
	}
	return true
}

func whitelistSet(whitelist []string) map[string]bool {
	if len(whitelist) == 0 {
		return nil
	}
	allow := map[string]bool{}
	for _, loc := range whitelist {
		if loc = NormalizeCountryCode(loc); loc != "" {
			allow[loc] = true
		}
	}
	return allow
}

func (s *Service) rankAuto(whitelist []string, provider string) []Pick {
	all, err := s.proxies.List()
	if err != nil {
		return nil
	}
	allow := whitelistSet(whitelist)
	type ranked struct {
		pick      Pick
		speed     int64
		handshake int64
	}
	rankedPicks := []ranked{}
	for _, p := range all {
		if !s.usable(p, allow, provider) {
			continue
		}
		rankedPicks = append(rankedPicks, ranked{
			pick:      Pick{ID: p.ID, URL: p.URL},
			speed:     p.SpeedKbps,
			handshake: p.HandshakeMs,
		})
	}
	sort.Slice(rankedPicks, func(i, j int) bool {
		if rankedPicks[i].speed != rankedPicks[j].speed {
			return rankedPicks[i].speed > rankedPicks[j].speed
		}
		return rankedPicks[i].handshake < rankedPicks[j].handshake
	})
	picks := make([]Pick, 0, len(rankedPicks))
	for _, r := range rankedPicks {
		picks = append(picks, r.pick)
	}
	return picks
}

func (s *Service) rankManual(ids []string, provider string) ([]Pick, error) {
	picks := []Pick{}
	for _, id := range ids {
		p, err := s.proxies.Get(id)
		if err != nil || p == nil {
			continue
		}
		if !s.usable(p, nil, provider) {
			continue
		}
		picks = append(picks, Pick{ID: p.ID, URL: p.URL})
	}
	if len(picks) == 0 {
		return nil, fmt.Errorf("provider proxy: no usable proxy among %d selected", len(ids))
	}
	return picks, nil
}

// ─────────────────────────────────────────────
// Live pair state
// ─────────────────────────────────────────────

func limitID(proxyID, provider string) string {
	return proxyID + "\x00" + provider
}

// RecordRateLimit records an upstream rate-limit for a proxy-provider pair.
// The pair is skipped until resetsAt passes.
func (s *Service) RecordRateLimit(proxyID, provider string, resetsAt time.Time) {
	if proxyID == "" {
		return
	}
	id := limitID(proxyID, provider)
	lim, err := s.limits.Get(id)
	if err != nil || lim == nil {
		lim = &models.ProxyLimit{ProxyID: proxyID, Provider: provider}
	}
	lim.ResetsAt = &resetsAt
	_ = s.limits.Put(id, lim)
	s.notify()
}

// RecordBlocked blocks a proxy for a provider after a geo-block answer.
// Blocks never expire on their own; the pair dies with the proxy.
func (s *Service) RecordBlocked(proxyID, provider, reason string) {
	if proxyID == "" {
		return
	}
	id := limitID(proxyID, provider)
	lim, err := s.limits.Get(id)
	if err != nil || lim == nil {
		lim = &models.ProxyLimit{ProxyID: proxyID, Provider: provider}
	}
	lim.Blocked = true
	lim.BlockReason = reason
	_ = s.limits.Put(id, lim)
	s.notify()
}

// ─────────────────────────────────────────────
// CRUD
// ─────────────────────────────────────────────

// Get returns one proxy.
func (s *Service) Get(id string) (*models.Proxy, error) {
	return s.proxies.Get(id)
}

// Delete removes a proxy and its pair state.
func (s *Service) Delete(id string) error {
	if err := s.proxies.Delete(id); err != nil {
		return err
	}
	limits, err := s.limits.ListFiltered(func(l *models.ProxyLimit) bool {
		return l.ProxyID == id
	})
	if err != nil {
		return nil
	}
	for _, lim := range limits {
		_ = s.limits.Delete(limitID(lim.ProxyID, lim.Provider))
	}
	s.notify()
	return nil
}

// List returns all proxies, manual first, then fastest first.
func (s *Service) List() ([]*models.Proxy, error) {
	all, err := s.proxies.List()
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool {
		mi, mj := all[i].Source == ManualSource, all[j].Source == ManualSource
		if mi != mj {
			return mi
		}
		if all[i].SpeedKbps != all[j].SpeedKbps {
			return all[i].SpeedKbps > all[j].SpeedKbps
		}
		return all[i].HandshakeMs < all[j].HandshakeMs
	})
	return all, nil
}

// PoolTotals reports the pooled proxy count.
func (s *Service) PoolTotals() (int, error) {
	all, err := s.proxies.List()
	if err != nil {
		return 0, err
	}
	return len(all), nil
}

// SourceProxies returns pooled proxies of one source, fastest first.
func (s *Service) SourceProxies(sourceKey string, offset, limit int) ([]*models.Proxy, int, error) {
	source := ListSource(sourceKey)
	pooled, err := s.proxies.ListFiltered(func(p *models.Proxy) bool {
		return p.Source == source
	})
	if err != nil {
		return nil, 0, err
	}
	sort.Slice(pooled, func(i, j int) bool {
		if pooled[i].SpeedKbps != pooled[j].SpeedKbps {
			return pooled[i].SpeedKbps > pooled[j].SpeedKbps
		}
		return pooled[i].HandshakeMs < pooled[j].HandshakeMs
	})
	total := len(pooled)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}
	page := pooled[offset:end]
	if page == nil {
		page = []*models.Proxy{}
	}
	return page, total, nil
}

// SourceInfos snapshots the runtime state of the given source keys.
func (s *Service) SourceInfos(keys []string) []SourceInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SourceInfo, 0, len(keys))
	for _, key := range keys {
		info := SourceInfo{Key: key, Status: SourceStatusIdle}
		if st, ok := s.sourceStatus[ListSource(key)]; ok {
			info.Status = st.status
			info.Total = st.total
			info.LastFetchAt = st.lastFetchAt
			info.LastError = st.lastError
		} else if meta, err := s.meta.Get(ListSource(key)); err == nil && meta != nil {
			info.Total = meta.Total
			info.LastFetchAt = meta.LastFetchAt
			info.LastError = meta.LastError
		}
		pooled, err := s.proxies.ListFiltered(func(p *models.Proxy) bool {
			return p.Source == ListSource(key)
		})
		if err == nil {
			info.Pooled = len(pooled)
		}
		out = append(out, info)
	}
	return out
}

// ─────────────────────────────────────────────
// Probing
// ─────────────────────────────────────────────

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
	sort.Slice(pooled, func(i, j int) bool {
		if pooled[i].SpeedKbps != pooled[j].SpeedKbps {
			return pooled[i].SpeedKbps < pooled[j].SpeedKbps
		}
		return pooled[i].HandshakeMs > pooled[j].HandshakeMs
	})
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

// RotateAll re-probes every pooled proxy and trims each location to the
// fastest N. It runs under a semaphore of size one: a concurrent rotation
// is skipped with ErrBusy, never queued.
func (s *Service) RotateAll(ctx context.Context) error {
	select {
	case s.rotationSem <- struct{}{}:
		defer func() { <-s.rotationSem }()
	default:
		return ErrBusy
	}
	s.checking.Store(true)
	defer s.checking.Store(false)
	defer s.notify()
	minSpeedKbps, maxPerLocation := s.settings()
	all, err := s.proxies.List()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, checkConcurrency)
	for _, p := range all {
		if p.Source == ManualSource {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(proxy *models.Proxy) {
			defer wg.Done()
			defer func() { <-sem }()
			s.updateRecord(ctx, proxy, minSpeedKbps, maxPerLocation)
		}(p)
	}
	wg.Wait()
	s.trimLocations(maxPerLocation)
	s.sweepDemand()
	// Expired pair limits carry no signal; drop them while passing.
	limits, err := s.limits.List()
	if err == nil {
		now := util.Now()
		for _, lim := range limits {
			if lim.ResetsAt != nil && !lim.Blocked && lim.ResetsAt.Before(now) {
				if _, err := s.proxies.Get(lim.ProxyID); err != nil {
					_ = s.limits.Delete(limitID(lim.ProxyID, lim.Provider))
				} else {
					lim.ResetsAt = nil
					_ = s.limits.Put(limitID(lim.ProxyID, lim.Provider), lim)
				}
			}
		}
	}
	return nil
}

// trimLocations deletes all but the fastest N list proxies per location.
// Manual proxies are sacred: never counted, never cut.
func (s *Service) trimLocations(maxPerLocation int) {
	all, err := s.proxies.List()
	if err != nil {
		return
	}
	byLocation := map[string][]*models.Proxy{}
	for _, p := range all {
		if p.Source == ManualSource {
			continue
		}
		byLocation[p.Location] = append(byLocation[p.Location], p)
	}
	for _, pooled := range byLocation {
		if len(pooled) <= maxPerLocation {
			continue
		}
		sort.Slice(pooled, func(i, j int) bool {
			if pooled[i].SpeedKbps != pooled[j].SpeedKbps {
				return pooled[i].SpeedKbps > pooled[j].SpeedKbps
			}
			return pooled[i].HandshakeMs < pooled[j].HandshakeMs
		})
		for _, extra := range pooled[maxPerLocation:] {
			_ = s.Delete(extra.ID)
		}
	}
}

// ─────────────────────────────────────────────
// Adding
// ─────────────────────────────────────────────

// AddManual registers a user-provided proxy URL. An existing URL is
// re-probed instead of duplicated. Dead entries are skipped; slow entries
// are skipped once their location is over the cap.
func (s *Service) AddManual(rawURL, location string) (*models.Proxy, error) {
	return s.add(context.Background(), rawURL, NormalizeCountryCode(location), ManualSource)
}

// AddCandidates adds fetched list candidates under the rotation semaphore.
// Candidates outside demand are skipped before probing; coverage proceeds
// in chunks while shortfall persists, capped at one window per fetch, so a
// full pool costs zero probes. A concurrent rotation is skipped with
// ErrBusy, never queued. The caller owns the final source transition:
// status stays at adding on return.
func (s *Service) AddCandidates(ctx context.Context, sourceKey string, candidates []models.ProxyCandidate) error {
	select {
	case s.rotationSem <- struct{}{}:
		defer func() { <-s.rotationSem }()
	default:
		return ErrBusy
	}
	s.checking.Store(true)
	defer s.checking.Store(false)
	defer s.notify()
	source := ListSource(sourceKey)
	demand := s.demandSet()
	filtered := make([]models.ProxyCandidate, 0, len(candidates))
	for _, c := range candidates {
		if loc := NormalizeCountryCode(c.Country); loc == "" || demand[loc] {
			filtered = append(filtered, c)
		}
	}
	s.setSourceStatus(source, SourceStatusAdding, len(filtered), "")
	s.coverFiltered(ctx, source, filtered)
	return nil
}

// shortfall counts missing fast slots across demanded regions.
func (s *Service) shortfall() int {
	minSpeedKbps, maxPerLocation := s.settings()
	missing := 0
	for region := range s.demandSet() {
		if n := s.fastCount(region, minSpeedKbps); n < maxPerLocation {
			missing += maxPerLocation - n
		}
	}
	return missing
}

// coverFiltered probes the filtered list in chunks while shortfall persists.
// Coverage starts at the persisted offset and proceeds linearly without
// wrapping inside one fetch, so no candidate is probed twice per fetch.
// Returns the covered count; the offset persists for the next fetch.
func (s *Service) coverFiltered(ctx context.Context, source string, filtered []models.ProxyCandidate) int {
	n := len(filtered)
	if n == 0 {
		return 0
	}
	offset := 0
	if meta, err := s.meta.Get(source); err == nil && meta != nil && meta.Offset > 0 {
		offset = meta.Offset % n
	}
	// Linear order from the offset: tail first, then head, no repeats.
	order := make([]models.ProxyCandidate, 0, n)
	order = append(order, filtered[offset:]...)
	order = append(order, filtered[:offset]...)
	if len(order) > fetchWindowSize {
		order = order[:fetchWindowSize]
	}
	covered := 0
	for covered < len(order) && ctx.Err() == nil {
		if s.shortfall() == 0 {
			break
		}
		end := covered + fetchChunkSize
		if end > len(order) {
			end = len(order)
		}
		s.probeChunk(ctx, source, order[covered:end])
		covered = end
	}
	_ = s.meta.Put(source, &sourceFetchMeta{
		Total:       n,
		Offset:      (offset + covered) % n,
		LastFetchAt: util.Now(),
	})
	return covered
}

// probeChunk probes one slice of candidates with a bounded worker pool.
func (s *Service) probeChunk(ctx context.Context, source string, chunk []models.ProxyCandidate) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, checkConcurrency)
	for _, c := range chunk {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(cand models.ProxyCandidate) {
			defer wg.Done()
			defer func() { <-sem }()
			scheme := strings.ToLower(strings.TrimSpace(cand.Protocol))
			if !supportedProtocols[scheme] || cand.Host == "" || cand.Port <= 0 {
				return
			}
			rawURL := fmt.Sprintf("%s://%s:%d", scheme, cand.Host, cand.Port)
			_, _ = s.add(ctx, rawURL, NormalizeCountryCode(cand.Country), source)
		}(c)
	}
	wg.Wait()
}

// add probes one URL and applies the adding rule: too slow for a saturated
// location or outside demand is skipped; a faster newcomer displaces the
// slowest of a full location one-for-one.
func (s *Service) add(ctx context.Context, rawURL, location, source string) (*models.Proxy, error) {
	p, err := parseProxyURL(rawURL, source)
	if err != nil {
		return nil, err
	}
	if existing, err := s.proxies.Get(p.ID); err == nil && existing != nil {
		// Manual proxies are sacred: a re-add returns the record untouched.
		if existing.Source == ManualSource {
			return existing, nil
		}
		minSpeedKbps, maxPerLocation := s.settings()
		s.updateRecord(ctx, existing, minSpeedKbps, maxPerLocation)
		updated, err := s.proxies.Get(p.ID)
		if err != nil || updated == nil {
			return nil, fmt.Errorf("proxy %s culled by rotation", p.URL)
		}
		return updated, nil
	}
	minSpeedKbps, maxPerLocation := s.settings()
	res, err := s.probe(ctx, p.URL)
	if err != nil {
		return nil, err
	}
	// Exit location is ground truth from a probe through the candidate;
	// list metadata and manual input are only a fallback.
	if country, derr := DetectExitCountry(context.WithoutCancel(ctx), p.URL); derr == nil && country != "" {
		location = country
	}
	if location != "" && !s.demandSet()[location] {
		return nil, fmt.Errorf("proxy %s outside demanded regions", p.URL)
	}
	n, err := s.locationCount(location)
	if err != nil {
		return nil, err
	}
	if res.speedKbps < minSpeedKbps {
		if n >= maxPerLocation {
			return nil, fmt.Errorf("proxy %s under the speed floor for saturated location %q", p.URL, location)
		}
	} else if n >= maxPerLocation {
		slowest, err := s.slowestIn(location)
		if err != nil || slowest == nil {
			return nil, fmt.Errorf("proxy %s overflows location %q", p.URL, location)
		}
		if res.speedKbps <= slowest.SpeedKbps {
			return nil, fmt.Errorf("proxy %s slower than pooled location %q", p.URL, location)
		}
		_ = s.Delete(slowest.ID)
	}
	p.Location = location
	p.HandshakeMs = res.handshakeMs
	p.SpeedKbps = res.speedKbps
	p.LastCheckAt = util.Now()
	p.CreatedAt = util.Now()
	if err := s.proxies.Put(p.ID, p); err != nil {
		return nil, err
	}
	s.notify()
	return p, nil
}

func (s *Service) setSourceStatus(source, status string, total int, lastError string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.sourceStatus[source]
	if !ok {
		st = &sourceState{}
		s.sourceStatus[source] = st
	}
	st.status = status
	st.total = total
	st.lastError = lastError
	if status != SourceStatusIdle {
		st.lastFetchAt = util.Now()
	}
}

// SetSourceRotating marks a source pool check in progress for the dashboard.
func (s *Service) SetSourceRotating(sourceKey string) {
	s.setSourceStatus(ListSource(sourceKey), SourceStatusRotating, 0, "")
}

// SetSourceDone returns a source to idle without an error, keeping the
// fetched total. Used when the adds succeeded but a later step did not run.
func (s *Service) SetSourceDone(sourceKey string, total int) {
	s.setSourceStatus(ListSource(sourceKey), SourceStatusIdle, total, "")
}

// SetSourceFetching marks a source fetch in progress for the dashboard.
func (s *Service) SetSourceFetching(sourceKey string) {
	s.setSourceStatus(ListSource(sourceKey), SourceStatusFetching, 0, "")
}

// SetSourceFailed records a source fetch error for the dashboard.
func (s *Service) SetSourceFailed(sourceKey string, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	s.setSourceStatus(ListSource(sourceKey), SourceStatusIdle, 0, msg)
	_ = s.meta.Put(ListSource(sourceKey), &sourceFetchMeta{LastFetchAt: util.Now(), LastError: msg})
}

// proxyID is deterministic: the same URL maps to the same record.
func proxyID(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return "px-" + hex.EncodeToString(sum[:8])
}

func parseProxyURL(rawURL, source string) (*models.Proxy, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url: %w", err)
	}
	if !supportedProtocols[u.Scheme] {
		return nil, fmt.Errorf("unsupported proxy protocol %q (want http, https, socks4, socks5)", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("proxy url requires a host")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return nil, fmt.Errorf("proxy url requires a valid port")
	}
	return &models.Proxy{
		ID:        proxyID(u.String()),
		URL:       u.String(),
		Protocol:  u.Scheme,
		Host:      u.Hostname(),
		Port:      port,
		Source:    source,
		CreatedAt: util.Now(),
	}, nil
}
