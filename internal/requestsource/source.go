// Package requestsource identifies which listener served a request.
// The public listener requires bootstrapping and authentication; the
// loopback-only admin listener answers without either. The edge middleware
// stores the source in the request context; business logic branches on it
// where behavior differs. A missing value reads as public, so unknown
// callers face authentication, never a bypass.
package requestsource

import (
	"context"
	"net/http"
)

// RequestSource is the listener that served a request.
type RequestSource int

const (
	// RequestSourcePublic requires bootstrapping and authentication.
	RequestSourcePublic RequestSource = iota
	// RequestSourcePrivate answers without authentication or
	// bootstrapping. It binds loopback only; the bind is the protection.
	RequestSourcePrivate
)

// contextKey carries the RequestSource. Unexported: callers use
// ContextWithSource and SourceFromContext only.
type contextKey struct{}

// ContextWithSource returns ctx carrying source.
func ContextWithSource(ctx context.Context, source RequestSource) context.Context {
	return context.WithValue(ctx, contextKey{}, source)
}

// SourceFromContext returns the RequestSource in ctx, defaulting to
// RequestSourcePublic when absent so unknown callers face authentication.
func SourceFromContext(ctx context.Context) RequestSource {
	if source, ok := ctx.Value(contextKey{}).(RequestSource); ok && source == RequestSourcePrivate {
		return RequestSourcePrivate
	}
	return RequestSourcePublic
}

// Middleware stores source in every request context reaching next.
func Middleware(source RequestSource) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(ContextWithSource(r.Context(), source)))
		})
	}
}
