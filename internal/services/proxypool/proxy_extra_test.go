package proxypool

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestNormalizeCountryCode(t *testing.T) {
	for in, want := range map[string]string{
		"us":          "US",
		"US":          "US",
		" Us ":        "US",
		"USA":         "US",
		"usa":         "US",
		"deu":         "DE",
		"GBR":         "GB",
		"":            "",
		"XX":          "XX",
		"TOOLONGCODE": "TOOLONGCODE",
	} {
		if got := NormalizeCountryCode(in); got != want {
			t.Errorf("NormalizeCountryCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAddManual_NormalizesCountry(t *testing.T) {
	svc := setup(t)
	p, err := svc.AddManual("http://10.9.9.9:8080", "usa")
	if err != nil {
		t.Fatal(err)
	}
	if p.Country != "US" {
		t.Fatalf("country not normalized: %q", p.Country)
	}
}

func TestSyncFromSource_NormalizesCountry(t *testing.T) {
	svc := setup(t)
	_, err := svc.SyncFromSource("src", []models.ProxyCandidate{
		{Protocol: "http", Host: "10.9.9.9", Port: 8080, Country: "deu"},
	})
	if err != nil {
		t.Fatal(err)
	}
	all, _ := svc.List()
	if len(all) != 1 || all[0].Country != "DE" {
		t.Fatalf("country not normalized: %+v", all)
	}
}

// geoStubProxy answers any absolute-form GET with body, acting as both the
// egress proxy and (via detectURL override) the geo endpoint.
func geoStubProxy(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.RequestURI, "http") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestCheck_DetectsExitCountry(t *testing.T) {
	svc := setup(t)
	oldDetect := detectURL
	detectURL = "http://geo-stub.invalid/json/?fields=status,countryCode"
	defer func() { detectURL = oldDetect }()

	proxyAddr := geoStubProxy(t, `{"status":"success","countryCode":"de"}`)
	// The liveness probe also runs through the proxy, so point CheckURL at
	// a URL the stub answers to.
	svc.CheckURL = "http://geo-stub.invalid/generate_204"

	p, err := svc.AddManual(proxyAddr, "usa")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Check(context.Background(), p.ID)
	if err != nil || !got.Alive {
		t.Fatalf("proxy should be alive: %+v err=%v", got, err)
	}
	// Detection overrides the seeded list metadata.
	if got.Country != "DE" {
		t.Fatalf("exit country not detected: %q", got.Country)
	}
}

func TestDetectExitCountry(t *testing.T) {
	oldDetect := detectURL
	detectURL = "http://geo-stub.invalid/json/"
	defer func() { detectURL = oldDetect }()

	proxyAddr := geoStubProxy(t, `{"status":"success","countryCode":"usa"}`)
	country, err := DetectExitCountry(context.Background(), proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	if country != "US" {
		t.Fatalf("got %q", country)
	}

	// Unparseable proxy URL fails loudly.
	if _, err := DetectExitCountry(context.Background(), "bogus://%%"); err == nil {
		t.Fatal("expected error for bad proxy url")
	}
}

func TestSelectManual_DemotesKnownBad(t *testing.T) {
	svc := setup(t)
	a, _ := svc.AddManual("http://10.0.0.1:8080", "")
	b, _ := svc.AddManual("http://10.0.0.2:8080", "")
	for _, id := range []string{a.ID, b.ID} {
		p, _ := svc.Get(id)
		p.Alive = true
		_ = svc.repo.Put(id, p)
	}
	svc.RecordOutcome(a.ID, "groq", false, 1)
	if got := svc.SelectManual([]string{a.ID, b.ID}, "groq"); got == nil || got.ID != b.ID {
		t.Fatalf("known-bad not demoted: %+v", got)
	}
	// Still honored when it is the only alive pick.
	if got := svc.SelectManual([]string{a.ID}, "groq"); got == nil || got.ID != a.ID {
		t.Fatalf("sole known-bad pick refused: %+v", got)
	}
}
