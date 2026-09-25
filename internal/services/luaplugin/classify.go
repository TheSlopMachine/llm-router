package luaplugin

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// quotaWords mark a 429 body as quota exhaustion rather than a transient
// rate limit. Kept narrow on purpose: wording is provider-specific, and a
// miss only softens quota_exceeded into rate_limit, never the reverse.
var quotaWords = []string{"per day", "perday", "daily", "quota", "free_tier", "free tier", "billing"}

// defaultClassify maps an upstream HTTP failure to the error contract.
// Status plus the structured envelope code/type decide the type; message
// text only feeds quota wording on bare 429s and the human message.
// Retry delay resolves from the retry-after header (delta seconds) or a
// "retry in N" hint in the body; quota errors without any hint default to
// now+60s, matching the contract layer backstop.
func defaultClassify(status int, headers map[string]string, body string) *models.ProviderError {
	code, errType, message := apierrors.ParseEnvelope(body)
	if message == "" {
		message = fmt.Sprintf("unexpected status %d: %s", status, body)
	}
	if status == 429 && hasQuotaWording(message) {
		code = "quota_exceeded"
	}
	perr := apierrors.MapUpstream(status, code, errType, message)
	if wait, ok := retryDelay(headers, message); ok {
		t := time.Now().Add(wait)
		perr.RetryAfter = &t
	} else if perr.Type == models.ErrorTypeQuotaExceeded && perr.RetryAfter == nil {
		t := time.Now().Add(time.Minute)
		perr.RetryAfter = &t
	}
	return perr
}

// hasQuotaWording matches provider quota phrasing in a 429 message.
func hasQuotaWording(message string) bool {
	lower := strings.ToLower(message)
	for _, w := range quotaWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// retryDelay resolves the wait before retrying: the retry-after response
// header (delta seconds) wins, then a "retry in N" hint embedded in the
// body (Google-style, seconds or milliseconds).
func retryDelay(headers map[string]string, message string) (time.Duration, bool) {
	for k, v := range headers {
		if strings.ToLower(strings.TrimSpace(k)) != "retry-after" {
			continue
		}
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && n > 0 {
			return time.Duration(n * float64(time.Second)), true
		}
	}
	lower := strings.ToLower(message)
	for _, marker := range []string{"retry in ", "retry after "} {
		if idx := strings.Index(lower, marker); idx != -1 {
			rest := lower[idx+len(marker):]
			var n float64
			var unit string
			if _, err := fmt.Sscanf(rest, "%f%s", &n, &unit); err == nil && n > 0 {
				if strings.HasPrefix(unit, "ms") {
					return time.Duration(n * float64(time.Millisecond)), true
				}
				return time.Duration(n * float64(time.Second)), true
			}
		}
	}
	return 0, false
}

// contractTypeName renders an ErrorType as its contract string.
func contractTypeName(t models.ErrorType) string {
	switch t {
	case models.ErrorTypeRateLimit:
		return "rate_limit"
	case models.ErrorTypeQuotaExceeded:
		return "quota_exceeded"
	case models.ErrorTypeAuth:
		return "auth"
	case models.ErrorTypeUpstream:
		return "upstream"
	case models.ErrorTypeTimeout:
		return "timeout"
	case models.ErrorTypeInvalidRequest:
		return "invalid_request"
	case models.ErrorTypeGeo:
		return "geo"
	case models.ErrorTypePaymentRequired:
		return "payment_required"
	case models.ErrorTypeNotFound:
		return "not_found"
	default:
		return "upstream"
	}
}

// contractToLua renders a classified error as a contract table for Lua.
func contractToLua(L *lua.LState, perr *models.ProviderError) *lua.LTable {
	tbl := L.NewTable()
	tbl.RawSetString("type", lua.LString(contractTypeName(perr.Type)))
	tbl.RawSetString("message", lua.LString(perr.Message))
	if perr.RetryAfter != nil {
		tbl.RawSetString("retry_after", lua.LNumber(perr.RetryAfter.Unix()))
	}
	if len(perr.Scope) > 0 {
		scope := L.NewTable()
		for _, s := range perr.Scope {
			scope.Append(lua.LString(s))
		}
		tbl.RawSetString("scope", scope)
	}
	return tbl
}

// classifyErrorFunc implements llm_router.classify_error: default
// classification of {status, headers, body}, then the plugin classify_error
// extension when declared. The extension receives (raw, default) and returns
// the final table or nil to accept the default. Anything else raises,
// turning into PluginInternalError at the call boundary.
func classifyErrorFunc(ctx *execContext) func(*lua.LState) int {
	return func(L *lua.LState) int {
		arg := L.CheckTable(1)
		// Extract everything before touching the stack: returns reset it.
		status := 0
		if n, ok := arg.RawGetString("status").(lua.LNumber); ok {
			status = int(n)
		}
		headers := luaTableStringMap(arg, "headers")
		body := luaTableString(arg, "body", "")
		if status <= 0 {
			L.RaiseError("classify_error: status is required")
			return 0
		}
		if ctx == nil {
			L.RaiseError("classify_error: no execution context")
			return 0
		}
		if ctx.inClassify {
			L.RaiseError("classify_error: extension must not recurse")
			return 0
		}

		dflt := defaultClassify(status, headers, body)
		ext := extensionFunc(ctx, string(HandlerClassifyError))
		if ext == nil {
			L.SetTop(0)
			L.Push(contractToLua(L, dflt))
			return 1
		}
		raw := L.NewTable()
		raw.RawSetString("status", lua.LNumber(status))
		hdrs := L.NewTable()
		for k, v := range headers {
			hdrs.RawSetString(k, lua.LString(v))
		}
		raw.RawSetString("headers", hdrs)
		raw.RawSetString("body", lua.LString(body))

		ctx.inClassify = true
		L.Push(ext)
		L.Push(raw)
		L.Push(contractToLua(L, dflt))
		callErr := L.PCall(2, 1, nil)
		ctx.inClassify = false
		if callErr != nil {
			L.SetTop(0)
			L.RaiseError("classify_error extension failed: %s", callErr.Error())
			return 0
		}
		ret := L.Get(-1)
		L.Pop(1)
		if ret == lua.LNil {
			L.SetTop(0)
			L.Push(contractToLua(L, dflt))
			return 1
		}
		tbl, ok := ret.(*lua.LTable)
		if !ok {
			L.SetTop(0)
			L.RaiseError("classify_error extension must return a table or nil")
			return 0
		}
		if _, ok := asProviderError(tbl); !ok {
			L.SetTop(0)
			L.RaiseError("classify_error extension returned an invalid error table")
			return 0
		}
		L.SetTop(0)
		L.Push(tbl)
		return 1
	}
}

// extensionFunc resolves a declared optional handler for the calling type,
// or nil when the plugin does not declare it.
func extensionFunc(ctx *execContext, handler string) *lua.LFunction {
	if ctx == nil || ctx.typeKey == "" {
		return nil
	}
	handlers, ok := ctx.registrations[ctx.typeKey]
	if !ok {
		return nil
	}
	fn, ok := handlers.RawGetString(handler).(*lua.LFunction)
	if !ok {
		return nil
	}
	return fn
}
