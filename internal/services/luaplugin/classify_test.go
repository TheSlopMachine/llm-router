package luaplugin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func TestClassifyError_DefaultMatrix(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin Classify Matrix
--- @author tester
--- @version 1.0.0
--- @router_version 0.1.1
--- @allow_host example.com

llm_router.register("matrix-type", {
  complete = function(ctx, credential, request)
    local mode = request.user
    local err
    if mode == "auth" then
      err = llm_router.classify_error({ status = 401, body = '{"error":{"message":"bad key"}}' })
    elseif mode == "quota" then
      err = llm_router.classify_error({ status = 429, body = '{"error":{"message":"quota exceeded for today"}}' })
    elseif mode == "header" then
      err = llm_router.classify_error({ status = 429, headers = { ["retry-after"] = "120" }, body = "slow" })
    elseif mode == "bodyhint" then
      err = llm_router.classify_error({ status = 429, body = "Slow down. Please retry in 54s" })
    elseif mode == "pay" then
      err = llm_router.classify_error({ status = 402, body = "subscription required" })
    else
      err = llm_router.classify_error({ status = 429, body = '{"error":{"message":"slow down"}}' })
    end
    return nil, err
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	complete := func(mode string) *models.ProviderError {
		t.Helper()
		req := &models.ChatCompletionRequest{
			Model:    "matrix-type/m",
			Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
			User:     &mode,
		}
		_, err := svc.Complete(context.Background(), testMeta("matrix-type",
			&models.Credential{ID: "c1"}, "matrix-type/m", nil), req)
		perr, ok := err.(*models.ProviderError)
		if !ok {
			t.Fatalf("mode %s: expected ProviderError, got %T (%v)", mode, err, err)
		}
		return perr
	}

	if got := complete("rate"); got.Type != models.ErrorTypeRateLimit {
		t.Fatalf("rate: %v", got.Type)
	}
	if got := complete("pay"); got.Type != models.ErrorTypePaymentRequired {
		t.Fatalf("pay: %v", got.Type)
	}
	if got := complete("auth"); got.Type != models.ErrorTypeAuth {
		t.Fatalf("auth: %v", got.Type)
	}
	quota := complete("quota")
	if quota.Type != models.ErrorTypeQuotaExceeded {
		t.Fatalf("quota: %v", quota.Type)
	}
	if quota.RetryAfter == nil || time.Until(*quota.RetryAfter) < 50*time.Second {
		t.Fatalf("quota retry_after: %+v", quota.RetryAfter)
	}
	header := complete("header")
	if header.RetryAfter == nil {
		t.Fatal("header retry_after missing")
	} else if d := time.Until(*header.RetryAfter); d < 110*time.Second || d > 130*time.Second {
		t.Fatalf("header retry_after: %v", d)
	}
	hint := complete("bodyhint")
	if hint.RetryAfter == nil {
		t.Fatal("body hint retry_after missing")
	} else if d := time.Until(*hint.RetryAfter); d < 45*time.Second || d > 65*time.Second {
		t.Fatalf("body hint retry_after: %v", d)
	}
}

func TestClassifyError_ExtensionOverrideAndNil(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin Classify Ext
--- @author tester
--- @version 1.0.0
--- @router_version 0.1.1
--- @allow_host example.com

llm_router.register("ext-type", {
  classify_error = function(raw, default_err)
    if raw.status == 403 then
      return { type = "geo", message = "egress blocked" }
    end
    return nil
  end,
  complete = function(ctx, credential, request)
    if request.user == "geo" then
      return nil, llm_router.classify_error({ status = 403, body = "forbidden" })
    end
    return nil, llm_router.classify_error({ status = 429, body = "slow" })
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	complete := func(mode string) error {
		t.Helper()
		req := &models.ChatCompletionRequest{
			Model:    "ext-type/m",
			Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
			User:     &mode,
		}
		_, err := svc.Complete(context.Background(), testMeta("ext-type",
			&models.Credential{ID: "c1"}, "ext-type/m", nil), req)
		return err
	}
	if err := complete("geo"); !isProviderType(err, models.ErrorTypeGeo) {
		t.Fatalf("override: %v", err)
	}
	if err := complete("other"); !isProviderType(err, models.ErrorTypeRateLimit) {
		t.Fatalf("nil accepts default: %v", err)
	}
}

func isProviderType(err error, want models.ErrorType) bool {
	var perr *models.ProviderError
	return errors.As(err, &perr) && perr.Type == want
}

func TestClassifyError_ExtensionGarbageIsInternal(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin Classify Garbage
--- @author tester
--- @version 1.0.0
--- @router_version 0.1.1
--- @allow_host example.com

llm_router.register("garbage-type", {
  classify_error = function(raw, default_err)
    return "boom"
  end,
  complete = function(ctx, credential, request)
    return nil, llm_router.classify_error({ status = 500, body = "x" })
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	req := &models.ChatCompletionRequest{
		Model:    "garbage-type/m",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}
	_, err := svc.Complete(context.Background(), testMeta("garbage-type",
		&models.Credential{ID: "c1"}, "garbage-type/m", nil), req)
	var ierr *models.PluginInternalError
	if !errors.As(err, &ierr) {
		t.Fatalf("garbage extension must fail closed, got %T (%v)", err, err)
	}
}

func TestClassifyError_ExtensionRecursionIsInternal(t *testing.T) {
	svc := setupService(t)
	src := `--- @plugin Classify Recurse
--- @author tester
--- @version 1.0.0
--- @router_version 0.1.1
--- @allow_host example.com

llm_router.register("recurse-type", {
  classify_error = function(raw, default_err)
    return llm_router.classify_error({ status = 500, body = "x" })
  end,
  complete = function(ctx, credential, request)
    return nil, llm_router.classify_error({ status = 500, body = "x" })
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	req := &models.ChatCompletionRequest{
		Model:    "recurse-type/m",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}
	_, err := svc.Complete(context.Background(), testMeta("recurse-type",
		&models.Credential{ID: "c1"}, "recurse-type/m", nil), req)
	var ierr *models.PluginInternalError
	if !errors.As(err, &ierr) || !strings.Contains(ierr.Cause, "recurse") {
		t.Fatalf("recursion must fail loudly, got %v", err)
	}
}
