package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func newUnparkHandler(t *testing.T) (*Handler, *credential.Service, string) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	providerSvc.RegisterGoAdapter(testutil.NewMockAdapter("mock"))
	inst, err := providerSvc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	credSvc := credential.New(database, providerSvc)
	cred, err := credSvc.Add(credential.AddOptions{
		ProviderID: inst.ID,
		Label:      "k",
		Data:       map[string]any{"api_key": "01234567890123456789"},
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	return &Handler{providerSvc: providerSvc, credSvc: credSvc}, credSvc, cred.ID
}

func decodeOK(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestCredentialsUnparkRoundTrip(t *testing.T) {
	h, credSvc, id := newUnparkHandler(t)

	if err := credSvc.Park(id, time.Hour, "quota"); err != nil {
		t.Fatalf("park: %v", err)
	}

	list := func() []map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/credentials", nil)
		rec := httptest.NewRecorder()
		h.apiCredentialsList(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list: got %d", rec.Code)
		}
		var out []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return out
	}
	rows := list()
	if len(rows) != 1 || rows[0]["parked"] != true || rows[0]["park_reason"] != "quota" {
		t.Fatalf("parked badge: %v", rows)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/credentials/"+id+"/unpark", nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.apiCredentialsUnpark(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unpark: got %d (%s)", rec.Code, rec.Body.String())
	}
	if decodeOK(t, rec)["ok"] != true {
		t.Fatalf("unpark body: %s", rec.Body.String())
	}
	rows = list()
	if len(rows) != 1 || rows[0]["parked"] == true {
		t.Fatalf("badge cleared: %v", rows)
	}

	// Unknown IDs fail loudly.
	req = httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/credentials/missing/unpark", nil)
	req.SetPathValue("id", "missing")
	rec = httptest.NewRecorder()
	h.apiCredentialsUnpark(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing: got %d, want 404", rec.Code)
	}
}
