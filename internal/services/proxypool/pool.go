// Package proxypool implements the outbound proxy pool.
//
// Design: two-stage probe (CONNECT handshake, download speed), one live
// state per proxy-provider pair (rate limit, block), demand-driven fetch.
// Presence in the bucket means the proxy answered the last probe;
// dead proxies are deleted, never flagged.
package proxypool

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/repository"
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
	Name        string    `json:"name"`
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

// lessProxy orders proxies fastest-first, ties by quicker handshake.
// Single source of truth for every ranking in the pool.
func lessProxy(a, b *models.Proxy) bool {
	if a.SpeedKbps != b.SpeedKbps {
		return a.SpeedKbps > b.SpeedKbps
	}
	return a.HandshakeMs < b.HandshakeMs
}
