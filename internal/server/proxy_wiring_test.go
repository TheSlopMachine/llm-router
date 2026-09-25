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

	if dropExhaustedProxy(nil, "plug", "type", "px-1") {
		t.Fatal("nil store must not filter")
	}
	if dropExhaustedProxy(svc, "plug", "type", "px-1") {
		t.Fatal("unmarked proxy must pass")
	}
	key, err := exhausted.KeyFromScope("plug", "type", "", "", "px-1", []string{"proxy"})
	if err != nil {
		t.Fatalf("scope key: %v", err)
	}
	if err := svc.Mark(key, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if !dropExhaustedProxy(svc, "plug", "type", "px-1") {
		t.Fatal("marked proxy must filter")
	}
	// A fuller key carrying account/model dimensions never matches a
	// proxy-only candidate: the candidate proves nothing about them.
	full := exhausted.FullKey("plug", "type", "a", "m", "px-2")
	if err := svc.Mark(full, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("mark full: %v", err)
	}
	if dropExhaustedProxy(svc, "plug", "type", "px-2") {
		t.Fatal("account/model-scoped key must not filter a proxy candidate")
	}
	if dropExhaustedProxy(svc, "plug", "type", "px-2") {
		t.Fatal("other proxy must pass")
	}
	if dropExhaustedProxy(svc, "other-plug", "type", "px-1") {
		t.Fatal("other plugin must pass")
	}
}
