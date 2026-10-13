package router

import (
	"context"
	"fmt"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil/mockplugin"
)

func modelProbeFixture(source string, withCheck bool) mockplugin.Fixture {
	handlers := []string{"complete"}
	if withCheck {
		handlers = append(handlers, "check_model")
	}
	return mockplugin.Fixture{
		PluginName: "Model Probe",
		TypeKey:    "model-probe-type",
		Models:     []string{"chat"},
		Handlers:   handlers,
		Source:     source,
	}
}

func modelProbeSource(checkBody string) string {
	registration := `  complete = function(ctx, request)
    return nil, { message = "unused", code = "server_error" }
  end,
  validate_credentials = function(data)
    return true
  end,
`
	if checkBody != "" {
		registration += fmt.Sprintf(`  check_model = function(ctx, model_ref)
%s
  end,
`, checkBody)
	}
	return `--- @plugin Model Probe
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("model-probe-type", {
` + registration + `})
`
}

func setupModelProbeRouter(t *testing.T, checkBody string) (*Service, string) {
	t.Helper()
	withCheck := checkBody != ""
	service, _, _, providerID := setupLuaRouter(t, modelProbeFixture(modelProbeSource(checkBody), withCheck))
	return service, providerID
}

func TestProbeModelHealthy(t *testing.T) {
	service, providerID := setupModelProbeRouter(t, `    return { status = "healthy", response = "alive" }`)
	result := service.TestModel(context.Background(), models.ModelId(providerID+"/chat"), "chat")
	if !result.OK || result.Response != "alive" {
		t.Fatalf("healthy must pass with response: %+v", result)
	}
	if result.QuotaExceeded {
		t.Fatalf("healthy must not set quota_exceeded: %+v", result)
	}
}

func TestProbeModelQuotaStaysTemporary(t *testing.T) {
	service, providerID := setupModelProbeRouter(t, `    return { status = "quota_exceeded", message = "slow down" }`)
	result := service.TestModel(context.Background(), models.ModelId(providerID+"/chat"), "chat")
	if result.OK {
		t.Fatalf("quota must fail: %+v", result)
	}
	if result.Code != "quota_exceeded" || !result.QuotaExceeded {
		t.Fatalf("quota must carry code and flag: %+v", result)
	}
}

func TestProbeModelUnhealthyMapsToUnavailable(t *testing.T) {
	service, providerID := setupModelProbeRouter(t, `    return { status = "unhealthy", message = "retired upstream" }`)
	result := service.TestModel(context.Background(), models.ModelId(providerID+"/chat"), "chat")
	if result.OK {
		t.Fatalf("unhealthy must fail: %+v", result)
	}
	if result.Code != "model_unavailable" {
		t.Fatalf("unhealthy must map to model_unavailable: %+v", result)
	}
	if result.QuotaExceeded {
		t.Fatalf("unhealthy must not set quota_exceeded: %+v", result)
	}
}

func TestProbeModelUnknownKeeps(t *testing.T) {
	service, providerID := setupModelProbeRouter(t, `    return { status = "unknown", message = "try later" }`)
	result := service.TestModel(context.Background(), models.ModelId(providerID+"/chat"), "chat")
	if result.OK || result.Code != "unknown" {
		t.Fatalf("unknown must fail with code: %+v", result)
	}
}

func TestProbeModelInvalidShapeFails(t *testing.T) {
	service, providerID := setupModelProbeRouter(t, `    return "not-a-table"`)
	result := service.TestModel(context.Background(), models.ModelId(providerID+"/chat"), "chat")
	if result.OK || result.Code != "upstream_error" {
		t.Fatalf("invalid verdict must fail as upstream_error: %+v", result)
	}
}

func TestProbeModelUnsupported(t *testing.T) {
	service, providerID := setupModelProbeRouter(t, "")
	result := service.TestModel(context.Background(), models.ModelId(providerID+"/chat"), "chat")
	if result.OK || result.Code != "unsupported" {
		t.Fatalf("handler-less type must report unsupported: %+v", result)
	}
}

func TestProbeModelEndpointPassthrough(t *testing.T) {
	service, providerID := setupModelProbeRouter(t, `    return { status = "healthy", response = model_ref.endpoint }`)
	for _, endpoint := range []string{"", "chat", "vision", "speech", "transcription", "image", "embeddings", "video", "moderate"} {
		want := endpoint
		if want == "" {
			want = "chat"
		}
		result := service.TestModel(context.Background(), models.ModelId(providerID+"/chat"), endpoint)
		if !result.OK || result.Response != want {
			t.Fatalf("endpoint %q must pass through, got %+v", endpoint, result)
		}
	}
}

func TestProbeModelInvalidID(t *testing.T) {
	service, _ := setupModelProbeRouter(t, `    return { status = "healthy" }`)
	result := service.TestModel(context.Background(), models.ModelId("not-a-model-id"), "chat")
	if result.OK {
		t.Fatalf("invalid id must fail: %+v", result)
	}
}
