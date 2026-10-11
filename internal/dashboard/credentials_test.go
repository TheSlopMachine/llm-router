package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func newUnparkHandler(t *testing.T) (*Handler, *credential.GormService, string) {
	t.Helper()
	ctx := context.Background()
	database := testutil.SetupTestSQLite(t)
	providerService := provider.NewGorm(database)
	providerService.RegisterGoAdapter(testutil.NewMockAdapter("mock"))
	instance, err := providerService.Create(ctx, provider.CreateOptions{Name: "Mock", TypeKey: "mock"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	credentialService := credential.NewGorm(database, providerService)
	credentialRecord, err := credentialService.Add(ctx, credential.AddOptions{
		ProviderID: instance.ID,
		Label:      "k",
		Data:       map[string]any{"api_key": "01234567890123456789"},
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	return &Handler{gormProviderService: providerService, gormCredentialService: credentialService}, credentialService, credentialRecord.ID
}

func decodeOK(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return decoded
}

func TestCredentialsUnparkRoundTrip(t *testing.T) {
	h, credentialService, id := newUnparkHandler(t)

	if err := credentialService.Park(context.Background(), id, time.Hour, "quota"); err != nil {
		t.Fatalf("park: %v", err)
	}

	list := func() []map[string]any {
		request := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/credentials", nil)
		recorder := httptest.NewRecorder()
		h.apiCredentialsList(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("list: got %d", recorder.Code)
		}
		var entries []map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &entries); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return entries
	}
	rows := list()
	if len(rows) != 1 || rows[0]["parked"] != true || rows[0]["park_reason"] != "quota" {
		t.Fatalf("parked badge: %v", rows)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/credentials/"+id+"/unpark", nil)
	request.SetPathValue("id", id)
	recorder := httptest.NewRecorder()
	h.apiCredentialsUnpark(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unpark: got %d (%s)", recorder.Code, recorder.Body.String())
	}
	if decodeOK(t, recorder)["ok"] != true {
		t.Fatalf("unpark body: %s", recorder.Body.String())
	}
	rows = list()
	if len(rows) != 1 || rows[0]["parked"] == true {
		t.Fatalf("badge cleared: %v", rows)
	}

	// Unknown IDs fail loudly.
	request = httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/credentials/missing/unpark", nil)
	request.SetPathValue("id", "missing")
	recorder = httptest.NewRecorder()
	h.apiCredentialsUnpark(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing: got %d, want 404", recorder.Code)
	}
}
