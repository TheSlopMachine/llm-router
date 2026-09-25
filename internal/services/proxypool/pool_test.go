package proxypool

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

func setupPool(t *testing.T) *Service {
	t.Helper()
	return New(testutil.SetupTestDB(t))
}

// fileOrigin serves a 1MB file and a geo stub answering ?country=.
func fileOrigin(t *testing.T, delay time.Duration) string {
	t.Helper()
	payload := make([]byte, 1<<20)
	for i := range payload {
		payload[i] = byte(i)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		if strings.HasPrefix(r.URL.Path, "/geo") {
			country := r.URL.Query().Get("country")
			if country == "" {
				country = "US"
			}
			_, _ = io.WriteString(w, `{"status":"success","countryCode":"`+country+`"}`)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

// forwardProxy is a minimal HTTP forward proxy: CONNECT answers 200,
// absolute-form GET is relayed to the origin.
func forwardProxy(t *testing.T, origin string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(30 * time.Second))
				br := bufio.NewReader(c)
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}
				for {
					h, err := br.ReadString('\n')
					if err != nil || h == "\r\n" || h == "\n" {
						break
					}
				}
				if strings.HasPrefix(line, "CONNECT") {
					_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\n\r\n")
					return
				}
				parts := strings.Split(line, " ")
				if len(parts) < 2 {
					return
				}
				target := parts[1]
				if strings.HasPrefix(target, "http://") {
					target = strings.TrimPrefix(target, "http://"+origin)
				}
				oc, err := net.Dial("tcp", origin)
				if err != nil {
					return
				}
				defer oc.Close()
				_, _ = fmt.Fprintf(oc, "GET %s HTTP/1.0\r\nHost: %s\r\nConnection: close\r\n\r\n", target, origin)
				_, _ = io.Copy(c, oc)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

// livePool points CheckURL, HandshakeHost and detectURL at local stubs.
// The geo stub answers the given country for every exit probe.
func livePool(t *testing.T, svc *Service, proxyAddr, origin, country string) {
	t.Helper()
	svc.CheckURL = "http://" + origin + "/file"
	svc.HandshakeHost = proxyAddr
	oldDetect := detectURL
	detectURL = "http://" + origin + "/geo?country=" + country
	t.Cleanup(func() { detectURL = oldDetect })
}

func seedProxy(t *testing.T, svc *Service, url, location string, handshakeMs, speedKbps int64, source string) *models.Proxy {
	t.Helper()
	p, err := parseProxyURL(url, source)
	if err != nil {
		t.Fatal(err)
	}
	p.Location = location
	p.HandshakeMs = handshakeMs
	p.SpeedKbps = speedKbps
	if err := svc.proxies.Put(p.ID, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAddManual_Validation(t *testing.T) {
	svc := setupPool(t)
	for _, raw := range []string{
		"ftp://127.0.0.1:21",
		"http://127.0.0.1",
		"not a url at all",
		"http://:8080",
	} {
		if _, err := svc.AddManual(raw, ""); err == nil {
			t.Errorf("expected error for %q", raw)
		}
	}
}

func TestAddManual_LiveAndDead(t *testing.T) {
	svc := setupPool(t)
	origin := fileOrigin(t, 0)
	proxyAddr := forwardProxy(t, origin)
	livePool(t, svc, proxyAddr, origin, "US")

	p, err := svc.AddManual("http://"+proxyAddr, "US")
	if err != nil {
		t.Fatalf("live proxy refused: %v", err)
	}
	if p.Location != "US" {
		t.Fatalf("exit location must be detected, got %q", p.Location)
	}
	if p.SpeedKbps <= 0 || p.HandshakeMs < 0 {
		t.Fatalf("probe numbers missing: %+v", p)
	}
	if _, err := svc.AddManual("http://127.0.0.1:1", "US"); err == nil {
		t.Fatal("dead proxy must be skipped")
	}
}

func TestAddManual_ExistingUpdatesInstead(t *testing.T) {
	svc := setupPool(t)
	origin := fileOrigin(t, 0)
	proxyAddr := forwardProxy(t, origin)
	livePool(t, svc, proxyAddr, origin, "US")

	first, err := svc.AddManual("http://"+proxyAddr, "US")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.AddManual("http://"+proxyAddr, "US")
	if err != nil {
		t.Fatalf("re-add must update, not fail: %v", err)
	}
	if first.ID != second.ID {
		t.Fatal("re-add must map to the same record")
	}
}

func TestAdd_OutsideDemandSkipped(t *testing.T) {
	svc := setupPool(t)
	origin := fileOrigin(t, 0)
	proxyAddr := forwardProxy(t, origin)
	livePool(t, svc, proxyAddr, origin, "ZZ")
	if _, err := svc.AddManual("http://"+proxyAddr, "ZZ"); err == nil {
		t.Fatal("proxy outside demanded regions must be skipped")
	}
	svc.NoteDemand([]string{"ZZ"})
	if _, err := svc.AddManual("http://"+proxyAddr, "ZZ"); err != nil {
		t.Fatalf("demanded proxy must be added: %v", err)
	}
}

func TestAdd_SlowSkippedWhenSaturated(t *testing.T) {
	svc := setupPool(t)
	origin := fileOrigin(t, 0)
	proxyAddr := forwardProxy(t, origin)
	proxyAddr2 := forwardProxy(t, origin)
	livePool(t, svc, proxyAddr, origin, "YY")
	// Floor above any loopback speed: every probe counts as slow.
	svc.SetConfig(1<<60, 2)
	svc.NoteDemand([]string{"YY"})

	if _, err := svc.AddManual("http://"+proxyAddr, "YY"); err != nil {
		t.Fatalf("slow proxy with room must be added: %v", err)
	}
	// Manuals don't count toward N: saturate with two list proxies.
	seedProxy(t, svc, "http://10.0.0.1:8080", "YY", 5, 100, ListSource("s"))
	seedProxy(t, svc, "http://10.0.0.2:8080", "YY", 5, 100, ListSource("s"))
	if _, err := svc.AddManual("http://"+proxyAddr2, "YY"); err == nil {
		t.Fatal("slow proxy for a saturated location must be skipped")
	}
}

func TestAdd_FastDisplacesSlowest(t *testing.T) {
	svc := setupPool(t)
	origin := fileOrigin(t, 0)
	proxyAddr := forwardProxy(t, origin)
	livePool(t, svc, proxyAddr, origin, "QQ")
	svc.SetConfig(1, 2)
	svc.NoteDemand([]string{"QQ"})

	slow := seedProxy(t, svc, "http://10.0.0.1:8080", "QQ", 5, 100, ListSource("s"))
	seedProxy(t, svc, "http://10.0.0.2:8080", "QQ", 5, 200, ListSource("s"))
	fresh, err := svc.AddManual("http://"+proxyAddr, "QQ")
	if err != nil {
		t.Fatalf("faster newcomer must displace: %v", err)
	}
	if _, err := svc.Get(slow.ID); err == nil {
		t.Fatal("slowest of a full location must be displaced")
	}
	n, err := svc.locationCount("QQ")
	if err != nil || n != 1 {
		t.Fatalf("list count must drop to N-1 after displacement, got %d %v", n, err)
	}
	if fresh.Source != ManualSource {
		t.Fatalf("displacing newcomer must keep its source, got %+v", fresh)
	}
}

func TestAdd_SlowerThanFullSkipped(t *testing.T) {
	svc := setupPool(t)
	origin := fileOrigin(t, 0)
	proxyAddr := forwardProxy(t, origin)
	livePool(t, svc, proxyAddr, origin, "QQ")
	// Everything counts as slow; the location is already full.
	svc.SetConfig(1<<60, 1)
	svc.NoteDemand([]string{"QQ"})
	seedProxy(t, svc, "http://10.0.0.1:8080", "QQ", 5, 100, ListSource("s"))
	if _, err := svc.AddManual("http://"+proxyAddr, "QQ"); err == nil {
		t.Fatal("slow newcomer to a full location must be skipped")
	}
}

func TestUpdateOne_DeadDeleted(t *testing.T) {
	svc := setupPool(t)
	svc.CheckURL = "http://example.com/file"
	dead := seedProxy(t, svc, "http://127.0.0.1:1", "ZZ", 5, 100, ManualSource)
	if _, err := svc.UpdateOne(context.Background(), dead.ID); err == nil {
		t.Fatal("dead proxy must be culled")
	}
	if _, err := svc.Get(dead.ID); err == nil {
		t.Fatal("dead proxy row must be gone")
	}
}

func TestRotateAll_CullsDeadKeepsLive(t *testing.T) {
	svc := setupPool(t)
	origin := fileOrigin(t, 0)
	proxyAddr := forwardProxy(t, origin)
	livePool(t, svc, proxyAddr, origin, "US")

	live := seedProxy(t, svc, "http://"+proxyAddr, "US", 999, 1, ListSource("t"))
	dead := seedProxy(t, svc, "http://127.0.0.1:1", "US", 5, 100, ListSource("t"))
	if err := svc.RotateAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(dead.ID); err == nil {
		t.Fatal("dead proxy must be gone after rotation")
	}
	kept, err := svc.Get(live.ID)
	if err != nil {
		t.Fatalf("live proxy must survive rotation: %v", err)
	}
	if kept.SpeedKbps <= 1 {
		t.Fatalf("live proxy speed must refresh: %d", kept.SpeedKbps)
	}
}

func TestRotateAll_TrimsToTopN(t *testing.T) {
	svc := setupPool(t)
	origin := fileOrigin(t, 0)
	addrs := []string{forwardProxy(t, origin), forwardProxy(t, origin), forwardProxy(t, origin)}
	livePool(t, svc, addrs[0], origin, "US")
	svc.SetConfig(1, 2)

	for _, addr := range addrs {
		seedProxy(t, svc, "http://"+addr, "US", 999, 1, ListSource("t"))
	}
	if err := svc.RotateAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	n, err := svc.locationCount("US")
	if err != nil || n != 2 {
		t.Fatalf("location must trim to N, got %d %v", n, err)
	}
}

func TestRotateAll_Busy(t *testing.T) {
	svc := setupPool(t)
	origin := fileOrigin(t, 300*time.Millisecond)
	proxyAddr := forwardProxy(t, origin)
	livePool(t, svc, proxyAddr, origin, "US")
	seedProxy(t, svc, "http://"+proxyAddr, "US", 5, 100, ListSource("t"))

	done := make(chan error, 1)
	go func() { done <- svc.RotateAll(context.Background()) }()
	time.Sleep(50 * time.Millisecond)
	if err := svc.RotateAll(context.Background()); err != ErrBusy {
		t.Fatalf("concurrent rotation must report busy, got %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("first rotation must succeed: %v", err)
	}
}

func TestProbeHandshake_Socks5RespectsTimeout(t *testing.T) {
	svc := setupPool(t)
	svc.hsTimeout = 200 * time.Millisecond
	// A black-hole server: accepts and never answers. The handshake must
	// fail on the probe deadline, not the OS TCP timeout.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(io.Discard, c)
			}(conn)
		}
	}()

	start := time.Now()
	_, err = svc.probeHandshake(context.Background(), "socks5://"+ln.Addr().String())
	if err == nil {
		t.Fatal("black-hole handshake must fail")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("handshake ignored the probe deadline: %v", elapsed)
	}
}

func TestProbeDownload_SpeedMagnitude(t *testing.T) {
	svc := setupPool(t)
	// 1MB over ~300ms ranks near 28 Mbit/s. Wide bounds absorb loopback
	// jitter; a units regression (bits/s stored as kbit/s) misses by 1000x.
	origin := fileOrigin(t, 300*time.Millisecond)
	proxyAddr := forwardProxy(t, origin)
	livePool(t, svc, proxyAddr, origin, "US")

	speed, err := svc.probeDownload(context.Background(), "http://"+proxyAddr)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if speed < 10000 || speed > 60000 {
		t.Fatalf("speed out of magnitude: %d kbit/s, want ~28000", speed)
	}
}

func TestChoose_Matrix(t *testing.T) {
	svc := setupPool(t)
	us := seedProxy(t, svc, "http://10.1.0.1:8080", "US", 100, 1000, ManualSource)
	de := seedProxy(t, svc, "http://10.1.0.2:8080", "DE", 50, 500, ManualSource)

	if id, url, err := svc.Choose(nil, models.ProxyModeDisabled, nil, "groq"); err != nil || id != "" || url != "" {
		t.Fatalf("disabled: %q %q %v", id, url, err)
	}
	// Whitelist DE matches one; fastest overall is US by speed.
	if id, _, err := svc.Choose([]string{"DE"}, models.ProxyModeAuto, nil, "groq"); err != nil || id != de.ID {
		t.Fatalf("auto whitelist: %q %v", id, err)
	}
	if id, _, err := svc.Choose(nil, models.ProxyModeAuto, nil, "groq"); err != nil || id != us.ID {
		t.Fatalf("auto any picks fastest: %q %v", id, err)
	}
	if id, _, err := svc.Choose(nil, models.ProxyModeManual, []string{us.ID, de.ID}, "groq"); err != nil || id != us.ID {
		t.Fatalf("manual: %q %v", id, err)
	}
	if _, _, err := svc.Choose(nil, models.ProxyModeManual, []string{"px-missing"}, "groq"); err == nil {
		t.Fatal("manual without usable proxy must error")
	}
	empty := setupPool(t)
	if id, url, err := empty.Choose(nil, models.ProxyModeAuto, nil, "groq"); err != nil || id != "" || url != "" {
		t.Fatalf("auto empty pool: %q %q %v", id, url, err)
	}
}

func TestChoose_FastestFirst(t *testing.T) {
	svc := setupPool(t)
	fast := seedProxy(t, svc, "http://10.1.0.1:8080", "US", 10, 1000, ManualSource)
	slow := seedProxy(t, svc, "http://10.1.0.2:8080", "US", 20, 500, ManualSource)

	if id, _, _ := svc.Choose(nil, models.ProxyModeAuto, nil, "groq"); id != fast.ID {
		t.Fatalf("fastest proxy must win, got %q", id)
	}
	picks, err := svc.Rank(nil, models.ProxyModeAuto, nil, "groq")
	if err != nil || len(picks) != 2 || picks[0].ID != fast.ID || picks[1].ID != slow.ID {
		t.Fatalf("rank order: %+v, %v", picks, err)
	}
}

func TestDelete_RemovesProxy(t *testing.T) {
	svc := setupPool(t)
	p := seedProxy(t, svc, "http://10.1.0.1:8080", "US", 10, 1000, ManualSource)
	if err := svc.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(p.ID); err == nil {
		t.Fatal("proxy must be gone after delete")
	}
}

func TestNeedsSearch_Gate(t *testing.T) {
	svc := setupPool(t)
	if !svc.NeedsSearch() {
		t.Fatal("empty pool with default demand must search")
	}
	// Fill every default region with N fast proxies.
	svc.SetConfig(1, 2)
	i := 0
	for _, region := range DefaultRegions {
		for k := 0; k < 2; k++ {
			i++
			seedProxy(t, svc, fmt.Sprintf("http://10.20.%d.240:8080", i), region, 5, 100000, ListSource("t"))
		}
	}
	if svc.NeedsSearch() {
		t.Fatal("full demand must pause searching")
	}
	// A fresh observed region reopens the gate.
	svc.NoteDemand([]string{"JP"})
	if !svc.NeedsSearch() {
		t.Fatal("underfilled observed region must resume searching")
	}
}

func TestRotateAll_ManualSacred(t *testing.T) {
	svc := setupPool(t)
	origin := fileOrigin(t, 0)
	addrs := []string{forwardProxy(t, origin), forwardProxy(t, origin), forwardProxy(t, origin), forwardProxy(t, origin)}
	livePool(t, svc, addrs[0], origin, "US")
	svc.SetConfig(1, 2)

	deadManual := seedProxy(t, svc, "http://127.0.0.1:1", "US", 5, 100, ManualSource)
	frozen := seedProxy(t, svc, "http://"+addrs[3], "US", 111, 222, ManualSource)
	for _, addr := range addrs[:3] {
		seedProxy(t, svc, "http://"+addr, "US", 999, 1, ListSource("t"))
	}
	if err := svc.RotateAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(deadManual.ID); err != nil {
		t.Fatal("dead manual must survive rotation")
	}
	kept, err := svc.Get(frozen.ID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.HandshakeMs != 111 || kept.SpeedKbps != 222 {
		t.Fatalf("manual numbers must freeze, got %+v", kept)
	}
	n, err := svc.locationCount("US")
	if err != nil || n != 2 {
		t.Fatalf("trim must ignore manuals, list count=%d %v", n, err)
	}
}

func TestDemand_ExpiryAndThrottle(t *testing.T) {
	svc := setupPool(t)
	svc.NoteDemand([]string{"JP"})
	got, err := svc.regions.Get("JP")
	if err != nil || got == nil {
		t.Fatalf("demand must persist: %v", got)
	}
	first := got.LastSeen
	svc.NoteDemand([]string{"JP"})
	got, _ = svc.regions.Get("JP")
	if !got.LastSeen.Equal(first) {
		t.Fatal("demand writes must throttle within the hour")
	}
	stale := &models.ActiveRegion{Region: "XX", LastSeen: util.Now().Add(-49 * time.Hour)}
	if err := svc.regions.Put("XX", stale); err != nil {
		t.Fatal(err)
	}
	svc.sweepDemand()
	if _, err := svc.regions.Get("XX"); err == nil {
		t.Fatal("stale demand must expire")
	}
}

func TestAddCandidates_ShortfallStopsProbing(t *testing.T) {
	svc := setupPool(t)
	svc.SetConfig(1, 2)
	// Fill every demanded region: shortfall closes before any probe.
	i := 0
	for _, region := range DefaultRegions {
		for k := 0; k < 2; k++ {
			i++
			seedProxy(t, svc, fmt.Sprintf("http://10.30.%d.240:8080", i), region, 5, 100000, ListSource("t"))
		}
	}
	cands := []models.ProxyCandidate{
		{Protocol: "http", Host: "127.0.0.1", Port: 1, Country: "US"},
		{Protocol: "http", Host: "127.0.0.1", Port: 2, Country: "US"},
	}
	if err := svc.AddCandidates(context.Background(), "src", cands); err != nil {
		t.Fatal(err)
	}
	pooled, err := svc.proxies.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(pooled) != 12 {
		t.Fatalf("full pool must cost zero probes, pooled=%d", len(pooled))
	}
}

func TestAddCandidates_WindowAndDemandFilter(t *testing.T) {
	svc := setupPool(t)
	cands := make([]models.ProxyCandidate, 0, 200)
	for i := 0; i < 200; i++ {
		country := "US"
		if i%2 == 0 {
			country = "ZZ"
		}
		cands = append(cands, models.ProxyCandidate{
			Protocol: "http", Host: "127.0.0.1", Port: 10000 + i, Country: country,
		})
	}
	// ZZ is not demanded: only the 100 US entries count.
	if err := svc.AddCandidates(context.Background(), "src", cands); err != nil {
		t.Fatal(err)
	}
	meta, err := svc.meta.Get(ListSource("src"))
	if err != nil || meta == nil {
		t.Fatalf("fetch meta missing: %v", meta)
	}
	if meta.Total != 100 {
		t.Fatalf("pre-filter must drop non-demanded entries, total=%d", meta.Total)
	}
	if meta.Offset != 0 {
		t.Fatalf("window over 100 with size 150 wraps to 0, got %d", meta.Offset)
	}
	if err := svc.AddCandidates(context.Background(), "src", cands); err != nil {
		t.Fatal(err)
	}
	// All US candidates point at refused ports: nothing pools, but the
	// window mechanics must not error.
	pooled, err := svc.proxies.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(pooled) != 0 {
		t.Fatalf("dead candidates must not pool, got %d", len(pooled))
	}
}

func TestRankWaitReadyImmediately(t *testing.T) {
	svc := setupPool(t)
	seedProxy(t, svc, "http://127.0.0.1:8080", "US", 5, 20000, ManualSource)
	picks, err := svc.RankWait(context.Background(), nil, nil, "groq")
	if err != nil || len(picks) != 1 {
		t.Fatalf("got picks=%v err=%v", picks, err)
	}
}

func TestRankWaitWaitsForNotify(t *testing.T) {
	svc := setupPool(t)
	done := make(chan []Pick, 1)
	go func() {
		picks, err := svc.RankWait(context.Background(), nil, nil, "groq")
		if err != nil {
			done <- nil
			return
		}
		done <- picks
	}()
	select {
	case <-done:
		t.Fatal("RankWait must block on an empty pool with unmet demand")
	case <-time.After(100 * time.Millisecond):
	}
	seedProxy(t, svc, "http://127.0.0.1:8081", "US", 5, 20000, ManualSource)
	svc.notify()
	select {
	case picks := <-done:
		if len(picks) != 1 {
			t.Fatalf("got picks=%v", picks)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RankWait did not wake on pool change")
	}
}

func TestRankSettledEmptyWhitelist(t *testing.T) {
	svc := setupPool(t)
	svc.SetConfig(15000, 1)
	for i, region := range DefaultRegions {
		seedProxy(t, svc, "http://127.0.0.1:91"+string(rune('0'+i)), region, 5, 20000, ManualSource)
	}
	if svc.NeedsSearch() {
		t.Fatal("demand must read as satisfied for the settled case")
	}
	// A whitelist matching nothing pooled ranks empty without error. It
	// must be asserted via Rank, not RankWait: Rank records the whitelist
	// as demand, after which the pool is no longer settled for RankWait.
	picks, err := svc.Rank([]string{"XX"}, models.ProxyModeAuto, nil, "groq")
	if err != nil || len(picks) != 0 {
		t.Fatalf("unmatched whitelist must rank empty: %+v, %v", picks, err)
	}
}

func TestRankWaitContextCancel(t *testing.T) {
	svc := setupPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.RankWait(ctx, nil, nil, "groq")
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
}
