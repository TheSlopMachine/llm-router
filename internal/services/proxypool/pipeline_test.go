package proxypool

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}

func waitSourceIdle(t *testing.T, svc *Service, key string) SourceInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		infos := svc.SourceInfos([]string{key})
		if len(infos) == 1 && infos[0].Status == SourceStatusIdle {
			return infos[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	infos := svc.SourceInfos([]string{key})
	t.Fatalf("source %q never settled: %+v", key, infos)
	return SourceInfo{}
}

func TestRefreshSource_RequiresWorkers(t *testing.T) {
	svc := setup(t)
	_, err := svc.RefreshSource("proxifly", []models.ProxyCandidate{{Protocol: "http", Host: "127.0.0.1", Port: 1}})
	if err == nil {
		t.Fatal("expected error before StartWorkers")
	}
}

func TestRefreshSource_PersistsOnlyVerified(t *testing.T) {
	svc := setup(t)
	svc.CheckURL = "http://example.com/generate_204"
	addr, closeFn := absoluteFormProxy(t)
	defer closeFn()
	svc.StartWorkers(context.Background())

	host, port := splitHostPort(t, addr)
	enqueued, err := svc.RefreshSource("proxifly", []models.ProxyCandidate{
		{Protocol: "http", Host: host, Port: port, Country: "us"},
		{Protocol: "http", Host: "127.0.0.1", Port: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if enqueued != 2 {
		t.Fatalf("enqueued: %d", enqueued)
	}

	info := waitSourceIdle(t, svc, "proxifly")
	if info.Total != 2 || info.Checked != 2 || info.Alive != 1 || info.Pending != 0 {
		t.Fatalf("counters: %+v", info)
	}
	if info.LastFetchAt.IsZero() {
		t.Fatal("last fetch timestamp missing")
	}

	items, total, err := svc.SourceProxies("proxifly", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].Host != host || !items[0].Alive {
		t.Fatalf("pool must hold only the verified proxy: total=%d items=%+v", total, items)
	}
}

func TestRefreshSource_DedupesPending(t *testing.T) {
	svc := setup(t)
	svc.StartWorkers(context.Background())
	cands := []models.ProxyCandidate{{Protocol: "http", Host: "127.0.0.1", Port: 1}}
	// Force an overlap: pre-seed the queue with the candidate's ID.
	p, err := candidateToProxy(cands[0], ListSource("proxifly"))
	if err != nil {
		t.Fatal(err)
	}
	svc.pipeline.mu.Lock()
	svc.pipeline.pending[p.ID] = true
	svc.pipeline.mu.Unlock()

	enqueued, err := svc.RefreshSource("proxifly", cands)
	if err != nil {
		t.Fatal(err)
	}
	if enqueued != 0 {
		t.Fatalf("pending candidate enqueued twice: %d", enqueued)
	}
	info := svc.SourceInfos([]string{"proxifly"})[0]
	if info.Status != SourceStatusIdle {
		t.Fatalf("empty refresh must settle to idle: %+v", info)
	}
}

func TestCandidateToProxy_NormalizesCountry(t *testing.T) {
	p, err := candidateToProxy(models.ProxyCandidate{Protocol: "http", Host: "10.9.9.9", Port: 8080, Country: "deu"}, ListSource("src"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Country != "DE" {
		t.Fatalf("country not normalized: %q", p.Country)
	}
}

func TestBeginFetchFailFetch(t *testing.T) {
	svc := setup(t)
	svc.BeginFetch("proxifly")
	info := svc.SourceInfos([]string{"proxifly"})[0]
	if info.Status != SourceStatusFetching {
		t.Fatalf("status: %+v", info)
	}
	svc.FailFetch("proxifly", errTestFetch)
	info = svc.SourceInfos([]string{"proxifly"})[0]
	if info.Status != SourceStatusIdle || info.LastError == "" {
		t.Fatalf("fail state: %+v", info)
	}
}

var errTestFetch = errorString("fetch exploded")

type errorString string

func (e errorString) Error() string { return string(e) }

func TestNew_CullsUnverifiedListProxies(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := New(database)
	p, err := candidateToProxy(models.ProxyCandidate{Protocol: "http", Host: "10.1.2.3", Port: 8080}, ListSource("proxifly"))
	if err != nil {
		t.Fatal(err)
	}
	p.Alive = false
	if err := svc.repo.Put(p.ID, p); err != nil {
		t.Fatal(err)
	}
	alive, _ := svc.AddManual("http://10.9.9.9:8080", "")
	alive.Alive = true
	_ = svc.repo.Put(alive.ID, alive)

	// Re-open: the unverified list row must be gone, the manual row kept.
	svc2 := New(database)
	if _, err := svc2.Get(p.ID); err == nil {
		t.Fatal("unverified list proxy survived reopen")
	}
	if _, err := svc2.Get(alive.ID); err != nil {
		t.Fatal("manual proxy culled by mistake")
	}
}

func TestSourceProxies_Pagination(t *testing.T) {
	svc := setup(t)
	for _, host := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"} {
		p, err := candidateToProxy(models.ProxyCandidate{Protocol: "http", Host: host, Port: 8080}, ListSource("proxifly"))
		if err != nil {
			t.Fatal(err)
		}
		p.Alive = true
		p.LatencyMs = int64(100 + p.Port)
		if err := svc.repo.Put(p.ID, p); err != nil {
			t.Fatal(err)
		}
	}
	// A dead sibling must not leak into the listing.
	dead, _ := candidateToProxy(models.ProxyCandidate{Protocol: "http", Host: "10.0.0.9", Port: 8080}, ListSource("proxifly"))
	_ = svc.repo.Put(dead.ID, dead)

	page, total, err := svc.SourceProxies("proxifly", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(page) != 2 {
		t.Fatalf("first page: total=%d len=%d", total, len(page))
	}
	page, total, err = svc.SourceProxies("proxifly", 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(page) != 1 {
		t.Fatalf("second page: total=%d len=%d", total, len(page))
	}
}
