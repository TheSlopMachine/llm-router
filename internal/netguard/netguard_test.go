package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
	"time"
)

func TestIsPublicAddress(t *testing.T) {
	cases := map[string]bool{
		"203.0.113.7":      true,
		"8.8.8.8":          true,
		"2606:4700::1111":  true,
		"127.0.0.1":        false,
		"10.1.2.3":         false,
		"172.16.0.1":       false,
		"192.168.1.1":      false,
		"169.254.169.254":  false,
		"100.64.0.1":       false,
		"0.0.0.0":          false,
		"198.18.0.1":       false,
		"224.0.0.1":        false,
		"240.0.0.1":        false,
		"::1":              false,
		"fe80::1":          false,
		"fc00::1":          false,
		"::ffff:127.0.0.1": false,
		"64:ff9b::7f00:1":  false,
		"2002:7f00:1::":    false,
	}
	for literal, want := range cases {
		if got := IsPublicAddress(netip.MustParseAddr(literal)); got != want {
			t.Errorf("IsPublicAddress(%s) = %v, want %v", literal, got, want)
		}
	}
}

type staticResolver map[string][]net.IP

func (r staticResolver) LookupIP(_ context.Context, _ string, host string) ([]net.IP, error) {
	if ips, ok := r[host]; ok {
		return ips, nil
	}
	return nil, errors.New("no such host")
}

func TestResolvePublicRejectsPrivateAnswers(t *testing.T) {
	resolver := staticResolver{
		"rebind.example": {net.ParseIP("127.0.0.1")},
		"mixed.example":  {net.ParseIP("10.0.0.1"), net.ParseIP("203.0.113.5")},
	}
	if _, err := ResolvePublic(context.Background(), resolver, "rebind.example", false); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("private-only answer must block, got %v", err)
	}
	ip, err := ResolvePublic(context.Background(), resolver, "mixed.example", false)
	if err != nil || ip.String() != "203.0.113.5" {
		t.Fatalf("mixed answer must pick the public address, got %v %v", ip, err)
	}
	if _, err := ResolvePublic(context.Background(), resolver, "localhost", false); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("localhost must block before resolution, got %v", err)
	}
}

func TestNewHTTPClientRefusesLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := NewHTTPClient(2 * time.Second)
	_, err := client.Get(server.URL)
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("loopback fetch must block, got %v", err)
	}
}

func TestCheckURL(t *testing.T) {
	for raw, wantError := range map[string]bool{
		"https://example.com/audio.mp3": false,
		"http://127.0.0.1:38082/api":    true,
		"http://[::1]/":                 true,
		"file:///etc/passwd":            true,
		"http://router.local/":          true,
	} {
		parsed, _ := url.Parse(raw)
		if err := CheckURL(parsed); (err != nil) != wantError {
			t.Errorf("CheckURL(%s) error = %v, want error %v", raw, err, wantError)
		}
	}
}
