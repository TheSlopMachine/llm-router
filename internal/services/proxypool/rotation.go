package proxypool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

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
		sort.Slice(pooled, func(i, j int) bool { return lessProxy(pooled[i], pooled[j]) })
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
