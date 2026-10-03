package luaplugin

import (
	"context"
	"errors"
	"net/url"

	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
)

// ErrNoProxyRoute reports that a routed request has no usable proxy.
var ErrNoProxyRoute = errors.New("no usable proxy route")

type proxyRetryExclusionsKey struct{}

func withProxyRetryExclusions(ctx context.Context, routes []string) context.Context {
	if len(routes) == 0 {
		return ctx
	}
	return context.WithValue(ctx, proxyRetryExclusionsKey{}, append([]string(nil), routes...))
}

func proxyRetryExclusions(ctx context.Context) []string {
	routes, _ := ctx.Value(proxyRetryExclusionsKey{}).([]string)
	return routes
}

func excludeProxyRoutes(picks []ProxyPick, excluded []string) []ProxyPick {
	if len(excluded) == 0 {
		return picks
	}
	blocked := make(map[string]struct{}, len(excluded))
	for _, route := range excluded {
		blocked[route] = struct{}{}
	}
	filtered := make([]ProxyPick, 0, len(picks))
	for _, pick := range picks {
		if _, found := blocked[redactedProxyHostPort(pick.URL, pick.ID)]; !found {
			filtered = append(filtered, pick)
		}
	}
	return filtered
}

// Proxy rotation helpers. Every plugin HTTP request starts with a fresh
// ordered pick list, then walks it while attempts fail. Rotation never
// falls back to direct after selecting proxies. An empty list means the
// resolver asked for direct, except during same-request retries, which fail
// when no alternate route remains.

// beginRequest resolves the ordered picks for one HTTP request and selects
// the first. Auto mode waits for ready or no-proxies instead of silently
// going direct. A resolver error fails the request loudly.
func (ctx *execContext) beginRequest(goCtx context.Context) error {
	ctx.proxyPicks = nil
	ctx.proxyIdx = 0
	ctx.proxyID, ctx.proxyURL = "", ""
	ctx.lastProxyID = ""
	var picks []ProxyPick
	if ctx.proxyRetryPicksSet {
		picks = ctx.proxyRetryPicks
		ctx.proxyRetryPicks = nil
		ctx.proxyRetryPicksSet = false
	} else {
		var err error
		picks, err = ctx.resolveProxyPicks(goCtx)
		if err != nil {
			return err
		}
		excluded := proxyRetryExclusions(goCtx)
		picks = excludeProxyRoutes(picks, excluded)
		if len(excluded) > 0 && len(picks) == 0 {
			ctx.proxyRetryUnavailable = true
			return ErrNoProxyRoute
		}
	}
	ctx.proxyPicks = picks
	if len(picks) > 0 {
		ctx.proxyID, ctx.proxyURL = picks[0].ID, picks[0].URL
	}
	return nil
}

func (ctx *execContext) resolveProxyPicks(goCtx context.Context) ([]ProxyPick, error) {
	if ctx.proxyResolver == nil {
		return nil, nil
	}
	known := exhausted.Segments{Credential: ctx.credentialID, Model: ctx.model.String(), Provider: ctx.providerID}
	if known.Provider == "" {
		known.Provider = ctx.typeKey
	}
	return ctx.proxyResolver(goCtx, ctx.proxyRec, ctx.proxyProviderConfig, known)
}

func (ctx *execContext) prepareProxyRetry(goCtx context.Context) error {
	excluded := proxyRetryExclusions(goCtx)
	if len(excluded) == 0 {
		return nil
	}
	picks, err := ctx.resolveProxyPicks(goCtx)
	if err != nil {
		return err
	}
	picks = excludeProxyRoutes(picks, excluded)
	if len(picks) > 0 {
		ctx.proxyRetryPicks = picks
		ctx.proxyRetryPicksSet = true
		return nil
	}
	return ErrNoProxyRoute
}

// rotateProxy advances to the next untried pick. It reports false when the
// route is direct or every pick was already tried: the caller then surfaces
// the last attempt error.
func (ctx *execContext) rotateProxy() bool {
	if ctx.proxyURL == "" {
		return false
	}
	next := ctx.proxyIdx + 1
	if next >= len(ctx.proxyPicks) {
		return false
	}
	ctx.proxyIdx = next
	ctx.proxyID, ctx.proxyURL = ctx.proxyPicks[next].ID, ctx.proxyPicks[next].URL
	return true
}

// redactedProxyHostPort renders a proxy URL as host:port without credentials.
// Userinfo never reaches logs: only the host portion is returned. An empty
// URL means direct and renders empty; an unparseable URL falls back to the
// proxy ID so the log still identifies the pick.
func redactedProxyHostPort(rawURL, fallbackID string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return fallbackID
	}
	return u.Host
}

// proxyDisplay reports the redacted host:port of the call's current route
// ("" = direct).
func (ctx *execContext) proxyDisplay() string {
	if ctx == nil {
		return ""
	}
	return redactedProxyHostPort(ctx.proxyURL, ctx.proxyID)
}
