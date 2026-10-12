package requestsource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSourceFromContextDefaultsPublic(t *testing.T) {
	if got := SourceFromContext(context.Background()); got != RequestSourcePublic {
		t.Fatalf("empty context = %v, want public", got)
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := ContextWithSource(context.Background(), RequestSourcePrivate)
	if got := SourceFromContext(ctx); got != RequestSourcePrivate {
		t.Fatalf("round trip = %v, want private", got)
	}
}

func TestMiddlewareStoresSource(t *testing.T) {
	var seen RequestSource = RequestSourcePublic
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = SourceFromContext(r.Context())
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	Middleware(RequestSourcePrivate)(next).ServeHTTP(httptest.NewRecorder(), request)
	if seen != RequestSourcePrivate {
		t.Fatalf("middleware stored %v, want private", seen)
	}
}
