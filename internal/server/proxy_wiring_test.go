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

	seg := func(account, proxyID string) exhausted.Segments {
		return exhausted.Segments{Plugin: "plug", Provider: "type", Account: account, Proxy: proxyID}
	}

	if dropExhaustedProxy(nil, seg("", "px-1")) {
		t.Fatal("nil store must not filter")
	}
	if dropExhaustedProxy(svc, seg("", "px-1")) {
		t.Fatal("unmarked proxy must pass")
	}
	key, err := exhausted.KeyFromScope("plug", "type", "", "", "px-1", []string{"proxy"})
	if err != nil {
		t.Fatalf("scope key: %v", err)
	}
	if err := svc.Mark(key, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if !dropExhaustedProxy(svc, seg("", "px-1")) {
		t.Fatal("marked proxy must filter")
	}
	// A proxy-only key blocks the pairing for every account: the candidate
	// carrying an account doesn't narrow it away.
	if !dropExhaustedProxy(svc, seg("some-account", "px-1")) {
		t.Fatal("proxy-only key must still filter when the candidate also carries an account")
	}
	if dropExhaustedProxy(svc, seg("", "px-2")) {
		t.Fatal("other proxy must pass")
	}
	if dropExhaustedProxy(svc, exhausted.Segments{Plugin: "other-plug", Provider: "type", Proxy: "px-1"}) {
		t.Fatal("other plugin must pass")
	}

	// Joint account+proxy scope: the pairing is limited, but the same
	// account is fine through a different proxy and the same proxy is fine
	// for a different account. This is the case the resolver previously
	// could not see, since it never received the account.
	jointKey, err := exhausted.KeyFromScope("plug", "type", "acct-a", "", "px-3", []string{"account", "proxy"})
	if err != nil {
		t.Fatalf("scope key: %v", err)
	}
	if err := svc.Mark(jointKey, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("mark joint: %v", err)
	}
	if !dropExhaustedProxy(svc, seg("acct-a", "px-3")) {
		t.Fatal("joint account+proxy key must filter that exact pairing")
	}
	if dropExhaustedProxy(svc, seg("acct-a", "px-4")) {
		t.Fatal("account acct-a must still pass on a different proxy")
	}
	if dropExhaustedProxy(svc, seg("acct-b", "px-3")) {
		t.Fatal("a different account must still pass on proxy px-3")
	}
}
