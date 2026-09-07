package luaplugin

import (
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func TestUUIDv5RFCVector(t *testing.T) {
	// RFC 4122 Appendix B test vector: DNS namespace + "www.example.com".
	got, err := uuidV5("6ba7b810-9dad-11d1-80b4-00c04fd430c8", "www.example.com")
	if err != nil {
		t.Fatalf("uuid_v5: %v", err)
	}
	if got != "2ed6657d-e927-568b-95e1-2665a8aea6a2" {
		t.Fatalf("vector mismatch: got %q", got)
	}
}

func TestUUIDv5KiroNamespace(t *testing.T) {
	first, err := uuidV5("34f7193f-561d-4050-bc84-9547d953d6bf", "ping")
	if err != nil {
		t.Fatalf("uuid_v5: %v", err)
	}
	second, err := uuidV5("34f7193f-561d-4050-bc84-9547d953d6bf", "ping")
	if err != nil {
		t.Fatalf("uuid_v5: %v", err)
	}
	if first != second {
		t.Fatal("uuid_v5 must be deterministic for equal inputs")
	}
	if len(first) != 36 || first[14] != '5' {
		t.Fatalf("not a version-5 UUID: %q", first)
	}
	variant := rune(first[19])
	if !strings.ContainsRune("89ab", variant) {
		t.Fatalf("bad variant nibble in %q", first)
	}
}

func TestUUIDv5InvalidNamespace(t *testing.T) {
	if _, err := uuidV5("not-a-uuid", "x"); err == nil {
		t.Fatal("expected error for invalid namespace")
	}
}

func TestRandomHexBounds(t *testing.T) {
	s, err := randomHex(16)
	if err != nil {
		t.Fatalf("random_hex: %v", err)
	}
	if len(s) != 32 {
		t.Fatalf("expected 32 hex chars, got %q", s)
	}
	other, err := randomHex(16)
	if err != nil {
		t.Fatalf("random_hex: %v", err)
	}
	if s == other {
		t.Fatal("consecutive random_hex outputs must differ")
	}
	if _, err := randomHex(0); err == nil {
		t.Fatal("expected error for zero length")
	}
}

func TestSandboxHelpersExposedToLua(t *testing.T) {
	svc, err := New(testutil.SetupTestDB(t), nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	source := `--- @plugin helper-probe
--- @author tester
--- @version 1.0.0
--- @router_version 0.0.4
--- @allow_host example.com

llm_router.register("helper-probe", {
  complete = function() end,
  credential_schema = function()
    return {
      { type = "text", text = llm_router.uuid_v5("6ba7b810-9dad-11d1-80b4-00c04fd430c8", "www.example.com") },
      { type = "text", text = llm_router.random_hex(16) },
    }
  end,
})
`
	if _, err := svc.Install([]byte(source), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	nodes, err := svc.Schema("helper-probe", "credential_schema")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].Text != "2ed6657d-e927-568b-95e1-2665a8aea6a2" {
		t.Fatalf("uuid_v5 through Lua: got %q", nodes[0].Text)
	}
	if len(nodes[1].Text) != 32 {
		t.Fatalf("random_hex(16) through Lua: got %q", nodes[1].Text)
	}
}
