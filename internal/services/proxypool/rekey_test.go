package proxypool

import (
	"testing"
)

// TestRekeySource moves pooled rows and fetch metadata to the new scheme.
func TestRekeySource(t *testing.T) {
	svc := setupPool(t)
	seedProxy(t, svc, "http://127.0.0.1:8080", "US", 10, 1000, "list:proxifly")
	seedProxy(t, svc, "http://127.0.0.1:8081", "US", 10, 1000, "list:other")

	if err := svc.meta.Put("list:proxifly", &sourceFetchMeta{Total: 7}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}

	if err := svc.RekeySource("list:proxifly", "list:manual/acme/proxifly/proxifly"); err != nil {
		t.Fatalf("rekey: %v", err)
	}

	pooled, total, err := svc.SourceProxies("manual/acme/proxifly/proxifly", 0, 0)
	if err != nil || total != 1 {
		t.Fatalf("rekeyed source: got %d %v", total, err)
	}
	if pooled[0].URL != "http://127.0.0.1:8080" {
		t.Fatalf("rekeyed row: %+v", pooled[0])
	}
	pooled, total, err = svc.SourceProxies("proxifly", 0, 0)
	if err != nil || total != 0 {
		t.Fatalf("old source must read empty: got %d %v", total, err)
	}
	meta, err := svc.meta.Get("list:manual/acme/proxifly/proxifly")
	if err != nil || meta == nil || meta.Total != 7 {
		t.Fatalf("meta: got %+v %v", meta, err)
	}
	if meta, _ := svc.meta.Get("list:proxifly"); meta != nil {
		t.Fatalf("old meta must be gone: %+v", meta)
	}

	stored, err := svc.StoredSources()
	if err != nil {
		t.Fatalf("stored: %v", err)
	}
	for _, s := range stored {
		if s == "list:proxifly" {
			t.Fatalf("bare key must be gone: %v", stored)
		}
	}
}

// TestSourceDisplayName derives the bare declared name from qualified keys.
func TestSourceDisplayName(t *testing.T) {
	cases := map[string]string{
		"manual/acme/proxifly/proxifly": "proxifly",
		"proxifly":                      "proxifly",
		"":                              "",
	}
	for in, want := range cases {
		if got := sourceDisplayName(in); got != want {
			t.Errorf("display(%q) = %q, want %q", in, got, want)
		}
	}
}
