package dashboard

import (
	"net/http"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// TestRegisterBuildsMux guards route pattern registration: net/http panics
// on invalid patterns (e.g. a mid-pattern {...} wildcard), which would take
// the whole server down at startup. Every route must register cleanly.
func TestRegisterBuildsMux(t *testing.T) {
	database := testutil.SetupTestDB(t)
	h := &Handler{}
	mux := http.NewServeMux()
	h.Register(mux, database)
}
