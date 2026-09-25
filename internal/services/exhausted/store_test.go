package exhausted

import (
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func setupService(t *testing.T) *Service {
	t.Helper()
	return New(testutil.SetupTestDB(t))
}

func TestBuildKey_FixedOrderSkipsEmpty(t *testing.T) {
	got := BuildKey(Segments{Plugin: "pl", Provider: "pr", Proxy: "px", Model: "m"})
	want := "p=pl\x00pr=pr\x00m=m\x00x=px"
	if got != want {
		t.Fatalf("key: got %q, want %q", got, want)
	}
}

func TestKeyFromScope_SelectsDimensions(t *testing.T) {
	got, err := KeyFromScope("pl", "pr", "a1", "m1", "x1", []string{"account", "model"})
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	if strings.Contains(got, "x=") {
		t.Fatalf("proxy must be absent: %q", got)
	}
	if !strings.Contains(got, "a=a1") || !strings.Contains(got, "m=m1") {
		t.Fatalf("account+model must be present: %q", got)
	}
}

func TestKeyFromScope_UnknownWordFailsClosed(t *testing.T) {
	if _, err := KeyFromScope("pl", "pr", "a", "m", "x", []string{"region"}); err == nil {
		t.Fatal("unknown scope word must fail")
	}
}

func TestKeyFromScope_EmptyScopeFails(t *testing.T) {
	if _, err := KeyFromScope("pl", "pr", "a", "m", "x", nil); err == nil {
		t.Fatal("empty scope must fail")
	}
}

func TestSubKeys_AllSubsetsMostSpecificFirst(t *testing.T) {
	keys := SubKeys(Segments{Plugin: "pl", Provider: "pr", Account: "a", Model: "m", Proxy: "x"})
	if len(keys) != 7 {
		t.Fatalf("want 7 subkeys, got %d: %q", len(keys), keys)
	}
	if keys[0] != FullKey("pl", "pr", "a", "m", "x") {
		t.Fatalf("most specific first: %q", keys[0])
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			t.Fatalf("duplicate subkey %q", k)
		}
		seen[k] = true
	}
}

func TestSubKeys_SkipsMissingDims(t *testing.T) {
	keys := SubKeys(Segments{Plugin: "pl", Provider: "pr", Account: "a"})
	if len(keys) != 1 {
		t.Fatalf("want 1 subkey, got %q", keys)
	}
}

func TestMark_LimitedUntilReset(t *testing.T) {
	svc := setupService(t)
	key := FullKey("pl", "pr", "a", "m", "x")
	if err := svc.Mark(key, time.Now().Add(time.Hour), "quota"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	limited, err := svc.Limited(key)
	if err != nil || !limited {
		t.Fatalf("limited: got %v, %v", limited, err)
	}
}

func TestLimited_ExpiredDeletesOnRead(t *testing.T) {
	svc := setupService(t)
	key := FullKey("pl", "pr", "a", "m", "x")
	if err := svc.Mark(key, time.Now().Add(-time.Minute), "stale"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	limited, err := svc.Limited(key)
	if err != nil || limited {
		t.Fatalf("expired must not limit: %v, %v", limited, err)
	}
	again, err := svc.Limited(key)
	if err != nil || again {
		t.Fatalf("expired entry must stay gone: %v, %v", again, err)
	}
}

func TestLimited_MissingKey(t *testing.T) {
	svc := setupService(t)
	limited, err := svc.Limited("p=no\x00pr=such")
	if err != nil || limited {
		t.Fatalf("missing: got %v, %v", limited, err)
	}
}

func TestLimitedAny_JointFilterSemantics(t *testing.T) {
	svc := setupService(t)
	accountKey, err := KeyFromScope("pl", "pr", "a1", "m1", "x1", []string{"account"})
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	if err := svc.Mark(accountKey, time.Now().Add(time.Hour), "account quota"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	// Same account, different model and proxy: still skipped.
	hit, err := svc.LimitedAny(Segments{Plugin: "pl", Provider: "pr", Account: "a1", Model: "m2", Proxy: "x2"})
	if err != nil || hit == "" {
		t.Fatalf("account key must match other models: %q, %v", hit, err)
	}
	// Different account: passes.
	hit, err = svc.LimitedAny(Segments{Plugin: "pl", Provider: "pr", Account: "a2", Model: "m1", Proxy: "x1"})
	if err != nil || hit != "" {
		t.Fatalf("other account must pass: %q, %v", hit, err)
	}
	// Different plugin: passes.
	hit, err = svc.LimitedAny(Segments{Plugin: "other", Provider: "pr", Account: "a1", Model: "m1", Proxy: "x1"})
	if err != nil || hit != "" {
		t.Fatalf("other plugin must pass: %q, %v", hit, err)
	}
	// Different provider instance: passes.
	hit, err = svc.LimitedAny(Segments{Plugin: "pl", Provider: "other", Account: "a1", Model: "m1", Proxy: "x1"})
	if err != nil || hit != "" {
		t.Fatalf("other provider must pass: %q, %v", hit, err)
	}
}

func TestLimitedAny_PrefersMostSpecific(t *testing.T) {
	svc := setupService(t)
	broad, _ := KeyFromScope("pl", "pr", "a1", "m1", "x1", []string{"account"})
	narrow, _ := KeyFromScope("pl", "pr", "a1", "m1", "x1", []string{"account", "model"})
	if err := svc.Mark(broad, time.Now().Add(time.Hour), "b"); err != nil {
		t.Fatalf("mark broad: %v", err)
	}
	if err := svc.Mark(narrow, time.Now().Add(time.Hour), "n"); err != nil {
		t.Fatalf("mark narrow: %v", err)
	}
	hit, err := svc.LimitedAny(Segments{Plugin: "pl", Provider: "pr", Account: "a1", Model: "m1", Proxy: "x1"})
	if err != nil || hit != narrow {
		t.Fatalf("want narrowest %q, got %q, %v", narrow, hit, err)
	}
}

func TestPrune_RemovesOnlyExpired(t *testing.T) {
	svc := setupService(t)
	live := FullKey("pl", "pr", "a-live", "m", "x")
	stale := FullKey("pl", "pr", "a-stale", "m", "x")
	if err := svc.Mark(live, time.Now().Add(time.Hour), ""); err != nil {
		t.Fatalf("mark live: %v", err)
	}
	if err := svc.Mark(stale, time.Now().Add(-time.Hour), ""); err != nil {
		t.Fatalf("mark stale: %v", err)
	}
	removed, err := svc.Prune()
	if err != nil || removed != 1 {
		t.Fatalf("prune: got %d, %v", removed, err)
	}
	if limited, _ := svc.Limited(live); !limited {
		t.Fatal("live entry must survive prune")
	}
}
