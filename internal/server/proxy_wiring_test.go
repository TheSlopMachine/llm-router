package server

import (
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func TestDropExhaustedProxy(t *testing.T) {
	database := testutil.SetupTestDB(t)
	svc := exhausted.New(database)

	seg := func(credentialID, proxyID string) exhausted.Segments {
		return exhausted.Segments{Plugin: "plug", Provider: "type", Credential: credentialID, Proxy: proxyID}
	}
	isLimited := func(store *exhausted.Service, candidate exhausted.Segments) bool {
		t.Helper()
		_, limited, err := checkProxyLimits(store, nil, candidate)
		if err != nil {
			t.Fatalf("check proxy limits: %v", err)
		}
		return limited
	}

	if isLimited(nil, seg("", "px-1")) {
		t.Fatal("nil store must not limit")
	}
	if isLimited(svc, seg("", "px-1")) {
		t.Fatal("unmarked proxy must pass")
	}
	key, err := exhausted.KeyFromScope("plug", "type", "", "", "px-1", []string{"proxy"})
	if err != nil {
		t.Fatalf("scope key: %v", err)
	}
	if err := svc.Mark(key, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if !isLimited(svc, seg("", "px-1")) {
		t.Fatal("marked proxy must report limited")
	}
	// A proxy-only key blocks the pairing for every credential: the candidate
	// carrying a credential doesn't narrow it away.
	if !isLimited(svc, seg("some-cred", "px-1")) {
		t.Fatal("proxy-only key must still limit when the candidate also carries a credential")
	}
	if isLimited(svc, seg("", "px-2")) {
		t.Fatal("other proxy must pass")
	}
	if isLimited(svc, exhausted.Segments{Plugin: "other-plug", Provider: "type", Proxy: "px-1"}) {
		t.Fatal("other plugin must pass")
	}

	// Joint credential+proxy scope: the pairing is limited, but the same
	// credential is fine through a different proxy and the same proxy is fine
	// for a different credential. This is the case the resolver previously
	// could not see, since it never received the credential.
	jointKey, err := exhausted.KeyFromScope("plug", "type", "cred-a", "", "px-3", []string{"account", "proxy"})
	if err != nil {
		t.Fatalf("scope key: %v", err)
	}
	if err := svc.Mark(jointKey, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("mark joint: %v", err)
	}
	if !isLimited(svc, seg("cred-a", "px-3")) {
		t.Fatal("joint credential+proxy key must limit that exact pairing")
	}
	if isLimited(svc, seg("cred-a", "px-4")) {
		t.Fatal("credential cred-a must still pass on a different proxy")
	}
	if isLimited(svc, seg("cred-b", "px-3")) {
		t.Fatal("a different credential must still pass on proxy px-3")
	}
}
