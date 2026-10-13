package luaplugin

import (
	"context"
	"errors"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

const modelHealthPluginSource = `--- @plugin Model Health Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("model-health-type", {
  complete = function(ctx, request)
    return nil, { message = "boom", code = "server_error" }
  end,

  check_model = function(ctx, model_ref)
    if model_ref.model_name == "dead" then
      return { status = "unhealthy", message = "retired upstream" }
    elseif model_ref.model_name == "limited" then
      return { status = "quota_exceeded", message = "slow down" }
    elseif model_ref.model_name == "flaky" then
      return { status = "unknown", message = "try later" }
    elseif model_ref.model_name == "broken" then
      return "not-a-table"
    end
    return { status = "healthy", response = model_ref.endpoint }
  end,
})
`

func installModelHealthPlugin(t *testing.T, service *Service) {
	t.Helper()
	if _, err := service.InstallSQLite(context.Background(), []byte(modelHealthPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
}

func modelHealthMetadata(model string) HandlerMeta {
	return HandlerMeta{ProviderID: "test-provider", TypeKey: "model-health-type", Model: models.ModelId("test-provider/" + model)}
}

func TestCheckModel_Verdicts(t *testing.T) {
	service := setupService(t)
	installModelHealthPlugin(t, service)
	cases := map[string]ModelStatus{
		"live":    ModelHealthy,
		"dead":    ModelUnhealthy,
		"limited": ModelQuotaExceeded,
		"flaky":   ModelUnknown,
	}
	for name, want := range cases {
		health, err := service.CheckModel(context.Background(), modelHealthMetadata(name), "chat")
		if err != nil {
			t.Fatalf("model %s: %v", name, err)
		}
		if health.Status != want {
			t.Fatalf("model %s: got %q, want %q", name, health.Status, want)
		}
	}
	health, err := service.CheckModel(context.Background(), modelHealthMetadata("dead"), "chat")
	if err != nil || health.Status != ModelUnhealthy || health.Message != "retired upstream" {
		t.Fatalf("unhealthy must carry the message: %+v %v", health, err)
	}
	health, err = service.CheckModel(context.Background(), modelHealthMetadata("live"), "vision")
	if err != nil || health.Status != ModelHealthy || health.Response != "vision" {
		t.Fatalf("endpoint must pass through to response: %+v %v", health, err)
	}
}

func TestCheckModel_InvalidShapeIsInternal(t *testing.T) {
	service := setupService(t)
	installModelHealthPlugin(t, service)
	_, err := service.CheckModel(context.Background(), modelHealthMetadata("broken"), "chat")
	var internal *models.PluginInternalError
	if !errors.As(err, &internal) {
		t.Fatalf("invalid verdict must be internal, got %T (%v)", err, err)
	}
}

func TestCheckModel_MissingHandler(t *testing.T) {
	service := setupService(t)
	if _, err := service.InstallSQLite(context.Background(), []byte(`--- @plugin Bare Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("bare-model-type", {
  complete = function(ctx, request) return nil, { message = "no", code = "server_error" } end,
})`), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := service.CheckModel(context.Background(), HandlerMeta{TypeKey: "bare-model-type", Model: "test-provider/chat"}, "chat")
	var notFound *notFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("missing handler must report not found, got %T (%v)", err, err)
	}
}
