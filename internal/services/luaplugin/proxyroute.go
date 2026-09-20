package luaplugin

import "context"

// Proxy rotation helpers. Every plugin HTTP request starts with a fresh
// ordered pick list, then walks it while attempts fail. Rotation never
// falls back to direct: an empty list means the resolver asked for direct,
// and an exhausted list surfaces the last error.

// beginRequest resolves the ordered picks for one HTTP request and selects
// the first. Auto mode waits for ready or no-proxies instead of silently
// going direct. A resolver error fails the request loudly.
func (ctx *execContext) beginRequest(goCtx context.Context) error {
	ctx.proxyPicks = nil
	ctx.proxyIdx = 0
	ctx.proxyID, ctx.proxyURL = "", ""
	if ctx.proxyResolver == nil {
		return nil
	}
	picks, err := ctx.proxyResolver(goCtx, ctx.proxyRec, ctx.proxyProviderConfig)
	if err != nil {
		return err
	}
	ctx.proxyPicks = picks
	if len(picks) > 0 {
		ctx.proxyID, ctx.proxyURL = picks[0].ID, picks[0].URL
	}
	return nil
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
