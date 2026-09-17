package luaplugin

// Proxy rotation helpers. Every plugin HTTP request starts with a fresh
// resolution (pool health may have changed since the previous request),
// then rotates through untried proxies while attempts fail. Rotation never
// falls back to direct: an empty route means the resolver asked for direct,
// and an exhausted proxy list surfaces the last error.

// beginRequest resolves the route for one HTTP request and resets the
// rotation set. A resolver error fails the request loudly.
func (ctx *execContext) beginRequest() error {
	ctx.triedProxies = map[string]bool{}
	if ctx.proxyResolver == nil {
		ctx.proxyID, ctx.proxyURL = "", ""
		return nil
	}
	proxyID, proxyURL, err := ctx.proxyResolver(ctx.proxyRec, ctx.proxyProviderConfig)
	if err != nil {
		return err
	}
	ctx.proxyID, ctx.proxyURL = proxyID, proxyURL
	if proxyID != "" {
		ctx.triedProxies[proxyID] = true
	}
	return nil
}

// rotateProxy advances to the next untried pooled proxy. It reports false
// when the route is direct, resolution fails, or every resolved proxy was
// already tried: the caller then surfaces the last attempt error.
func (ctx *execContext) rotateProxy() bool {
	if ctx.proxyResolver == nil || ctx.proxyURL == "" {
		return false
	}
	proxyID, proxyURL, err := ctx.proxyResolver(ctx.proxyRec, ctx.proxyProviderConfig)
	if err != nil || proxyURL == "" || ctx.triedProxies[proxyID] {
		return false
	}
	ctx.proxyID, ctx.proxyURL = proxyID, proxyURL
	ctx.triedProxies[proxyID] = true
	return true
}
