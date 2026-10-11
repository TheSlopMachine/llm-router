package proxypool

import (
	"testing"
	"time"
)

func TestCandidateURL(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		want  string
		valid bool
	}{
		{name: "http", raw: "http://1.2.3.4:8080", want: "http://1.2.3.4:8080", valid: true},
		{name: "https", raw: "https://1.2.3.4:8443", want: "https://1.2.3.4:8443", valid: true},
		{name: "socks4", raw: "socks4://1.2.3.4:1080", want: "socks4://1.2.3.4:1080", valid: true},
		{name: "socks4a alias", raw: "socks4a://1.2.3.4:1080", want: "socks4a://1.2.3.4:1080", valid: true},
		{name: "socks5", raw: "socks5://1.2.3.4:1080", want: "socks5://1.2.3.4:1080", valid: true},
		{name: "credentials kept", raw: "socks5://alice:s3cret@1.2.3.4:1080", want: "socks5://alice:s3cret@1.2.3.4:1080", valid: true},
		{name: "ftp rejected", raw: "ftp://1.2.3.4:21", valid: false},
		{name: "empty rejected", raw: "", valid: false},
		{name: "missing host rejected", raw: "http://:8080", valid: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := candidateURL(Candidate{URL: testCase.raw})
			if ok != testCase.valid {
				t.Fatalf("candidateURL(%q) valid=%v, want %v", testCase.raw, ok, testCase.valid)
			}
			if ok && got != testCase.want {
				t.Fatalf("candidateURL(%q) = %q, want %q", testCase.raw, got, testCase.want)
			}
		})
	}
}

func TestTransportForSchemes(t *testing.T) {
	for _, raw := range []string{
		"http://1.2.3.4:8080",
		"https://1.2.3.4:8443",
		"socks4://1.2.3.4:1080",
		"socks5://1.2.3.4:1080",
		"socks5://alice:s3cret@1.2.3.4:1080",
	} {
		tr, err := TransportFor(raw, time.Second)
		if err != nil {
			t.Fatalf("TransportFor(%q) error: %v", raw, err)
		}
		if tr == nil || tr.DialContext == nil {
			t.Fatalf("TransportFor(%q) must return a usable transport", raw)
		}
	}
	if _, err := TransportFor("ftp://1.2.3.4:21", time.Second); err == nil {
		t.Fatal("ftp must fail")
	}
}
