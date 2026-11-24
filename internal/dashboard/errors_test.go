package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// responseErrorCode decodes the code of a dashboard error response.
func responseErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var response models.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v, body %s", err, recorder.Body.String())
	}
	return response.Code
}

func TestServiceErrMapsSentinelsToCodes(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"duplicate", fmt.Errorf("virtual model name %q: %w", "a", apierrors.ErrAlreadyExists), http.StatusConflict, errorCodeAlreadyExists},
		{"stale version", fmt.Errorf("virtual model: %w", apierrors.ErrConcurrentUpdate), http.StatusConflict, errorCodeConcurrentUpdate},
		{"other", errors.New("member provider missing"), http.StatusBadRequest, ""},
	}
	handler := &Handler{}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.serviceErr(recorder, http.StatusBadRequest, testCase.err)
			if recorder.Code != testCase.status {
				t.Fatalf("status: got %d, want %d", recorder.Code, testCase.status)
			}
			if code := responseErrorCode(t, recorder); code != testCase.code {
				t.Fatalf("code: got %q, want %q", code, testCase.code)
			}
		})
	}
}

func TestRequireAuthAnswersUnauthenticatedCode(t *testing.T) {
	handler := &Handler{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/llm-router/dashboard/providers", nil)
	handler.requireAuth(func(http.ResponseWriter, *http.Request) { t.Fatal("handler ran without a session") })(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d", recorder.Code)
	}
	if code := responseErrorCode(t, recorder); code != errorCodeUnauthenticated {
		t.Fatalf("code: got %q", code)
	}
}
