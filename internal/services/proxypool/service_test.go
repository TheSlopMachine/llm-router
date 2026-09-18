package proxypool

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func bufioNewReader(r io.Reader) *bufio.Reader { return bufio.NewReader(r) }

func setup(t *testing.T) *Service {
	t.Helper()
	return New(testutil.SetupTestDB(t))
}

func TestAddManual_Validation(t *testing.T) {
	svc := setup(t)
	if _, err := svc.AddManual("http://127.0.0.1:8080", "us"); err != nil {
		t.Fatalf("valid url rejected: %v", err)
	}
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
	if _, err := svc.AddManual("http://127.0.0.1:8080", ""); err == nil {
		t.Error("expected dedup error on re-add")
	}
}

func TestSelect_PreferencesAndProviderHealth(t *testing.T) {
	svc := setup(t)
	us, _ := svc.AddManual("http://1.1.1.1:80", "us")
	de, _ := svc.AddManual("http://2.2.2.2:80", "de")
	svc.RecordOutcome(us.ID, "probe", true, 1)
	svc.RecordOutcome(de.ID, "probe", true, 1)
	// Mark both alive via repo update shortcut: Check is network-bound, so flip flags directly.
	for _, id := range []string{us.ID, de.ID} {
		p, _ := svc.Get(id)
		p.Alive = true
		_ = svc.repo.Put(id, p)
	}

	got := svc.Select(models.ProxyPreferences{Location: "DE"}, "groq")
	if got == nil || got.ID != de.ID {
		t.Fatalf("location preference ignored: %+v", got)
	}
	svc.RecordOutcome(de.ID, "groq", false, 1)
	got = svc.Select(models.ProxyPreferences{Location: "DE"}, "groq")
	if got == nil || got.ID != us.ID {
		t.Fatalf("provider-known-bad proxy not demoted: %+v", got)
	}
	// Same proxy stays usable for another provider.
	got = svc.Select(models.ProxyPreferences{Location: "DE"}, "google")
	if got == nil || got.ID != de.ID {
		t.Fatalf("per-provider health leaked: %+v", got)
	}
	// A known-bad proxy is still returned when it is the only alive one:
	// demotion is a priority, not a block.
	svc.RecordOutcome(us.ID, "groq", false, 1)
	got = svc.Select(models.ProxyPreferences{Location: "DE"}, "groq")
	if got == nil {
		t.Fatal("all-demoted pool selects nothing")
	}
}

func TestSelect_StickyKnownGood(t *testing.T) {
	svc := setup(t)
	us, _ := svc.AddManual("http://1.1.1.1:80", "us")
	de, _ := svc.AddManual("http://2.2.2.2:80", "de")
	for _, id := range []string{us.ID, de.ID} {
		p, _ := svc.Get(id)
		p.Alive = true
		_ = svc.repo.Put(id, p)
	}
	// The proven exit sticks even against the location preference.
	svc.RecordOutcome(us.ID, "groq", true, 1)
	got := svc.Select(models.ProxyPreferences{Location: "DE"}, "groq")
	if got == nil || got.ID != us.ID {
		t.Fatalf("sticky known-good lost to location preference: %+v", got)
	}
	// Once it fails, rotation moves on and the preference leads again.
	svc.RecordOutcome(us.ID, "groq", false, 1)
	got = svc.Select(models.ProxyPreferences{Location: "DE"}, "groq")
	if got == nil || got.ID != de.ID {
		t.Fatalf("failed sticky not rotated: %+v", got)
	}
}

// absoluteFormProxy is a minimal HTTP proxy handling absolute-form GET.
func absoluteFormProxy(t *testing.T) (addr string, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				req, err := http.ReadRequest(bufioNewReader(c))
				if err != nil {
					return
				}
				// Pretend the upstream answered: any absolute-form GET gets 204.
				if strings.HasPrefix(req.URL.String(), "http") {
					_, _ = io.WriteString(c, "HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n")
				}
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func TestCheck_LiveAndDead(t *testing.T) {
	svc := setup(t)
	svc.CheckURL = "http://example.com/generate_204"
	addr, closeFn := absoluteFormProxy(t)
	defer closeFn()

	p, err := svc.AddManual("http://"+addr, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Check(context.Background(), p.ID)
	if err != nil || !got.Alive {
		t.Fatalf("live proxy marked dead: alive=%v err=%v", got.Alive, err)
	}

	// Dead list-sourced proxy is culled immediately.
	dead, err := candidateToProxy(models.ProxyCandidate{Protocol: "http", Host: "127.0.0.1", Port: 1}, ListSource("proxifly"))
	if err != nil {
		t.Fatal(err)
	}
	dead.Alive = true
	if err := svc.repo.Put(dead.ID, dead); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Check(context.Background(), dead.ID)
	if err == nil {
		t.Fatal("expected probe failure for dead proxy")
	}
	if _, gerr := svc.Get(dead.ID); gerr == nil {
		t.Fatal("dead list-sourced proxy not culled")
	}
}

func TestTransportFor_Protocols(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:8080",
		"https://user:pass@127.0.0.1:8443",
		"socks5://127.0.0.1:1080",
		"socks4://127.0.0.1:1080",
	} {
		if _, err := TransportFor(raw, time.Second); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
	if _, err := TransportFor("ftp://127.0.0.1:21", time.Second); err == nil {
		t.Error("expected error for ftp")
	}
}

func TestSocks4Handshake(t *testing.T) {
	// Minimal SOCKS4a server: expect VN=4 CD=1, reply granted, then proxy GET.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 8)
		if _, err := io.ReadFull(conn, buf); err != nil || buf[0] != 0x04 || buf[1] != 0x01 {
			return
		}
		// read until two NULs (user id + domain for 4a)
		one := make([]byte, 1)
		nuls := 0
		for nuls < 2 {
			if _, err := conn.Read(one); err != nil {
				return
			}
			if one[0] == 0 {
				nuls++
			}
		}
		_, _ = conn.Write([]byte{0, 0x5a, 0, 0, 0, 0, 0, 0})
		req, _ := http.ReadRequest(bufioNewReader(conn))
		if req != nil && req.Method == http.MethodGet {
			_, _ = io.WriteString(conn, "HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n")
		}
	}()

	transport, err := TransportFor("socks4://"+ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	resp, err := client.Get("http://example.com/generate_204")
	if err != nil {
		t.Fatalf("socks4 round trip: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	fmt.Println("socks4 ok")
}
