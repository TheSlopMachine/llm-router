package proxypool

import (
	"encoding/json"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func TestStatusUsesLibrarySnapshotAndNonNilCollections(t *testing.T) {
	service, err := New(testutil.SetupTestDB(t))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	status, err := service.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Mode == "" {
		t.Fatal("mode must be populated")
	}
	if len(status.Lanes) != 4 {
		t.Fatalf("lanes = %d, want 4", len(status.Lanes))
	}
	if status.Sources == nil {
		t.Fatal("sources must not be nil")
	}
	if status.BanReasons == nil {
		t.Fatal("ban_reasons must not be nil")
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(encoded)
	for _, needle := range []string{`"lanes":null`, `"sources":null`, `"ban_reasons":null`} {
		if contains(text, needle) {
			t.Fatalf("status contains %s: %s", needle, text)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
