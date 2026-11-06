package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/requestsource"
)

// TestStatusReportsVersionOnlyWhenAuthenticated proves the build version
// reaches authenticated callers and never anonymous public ones.
func TestStatusReportsVersionOnlyWhenAuthenticated(t *testing.T) {
	parameters := buildWiredParams(t)
	parameters.Version = "1.2.3"
	handler, err := New(parameters)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mux := http.NewServeMux()
	handler.RegisterAPI(mux)

	serve := func(source requestsource.RequestSource) map[string]any {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/llm-router/status", nil)
		request = request.WithContext(requestsource.ContextWithSource(request.Context(), source))
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status code = %d (%s), want 200", recorder.Code, recorder.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body
	}

	private := serve(requestsource.RequestSourcePrivate)
	if private["authenticated"] != true || private["version"] != "1.2.3" {
		t.Fatalf("private status = %v, want authenticated with version 1.2.3", private)
	}

	public := serve(requestsource.RequestSourcePublic)
	if public["authenticated"] != false {
		t.Fatalf("public status = %v, want unauthenticated", public)
	}
	if _, present := public["version"]; present {
		t.Fatalf("public status = %v, must not carry version", public)
	}
}
