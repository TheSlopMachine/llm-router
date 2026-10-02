package v1

import (
	"net/http"
	"testing"
)

// TestRegisterBuildsMux guards route pattern registration: net/http panics
// on invalid patterns (e.g. a mid-pattern {...} wildcard), which would take
// the whole server down at startup. Every route must register cleanly.
func TestRegisterBuildsMux(t *testing.T) {
	h := &Handler{}
	mux := http.NewServeMux()
	h.Register(mux)
}
