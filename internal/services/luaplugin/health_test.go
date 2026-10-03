package luaplugin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

const healthPluginSource = `--- @plugin Health Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.3.0
--- @allow_host example.com

llm_router.register("health-type", {
  complete = function(ctx, credential, request)
    return nil, { type = "upstream", message = "boom" }
  end,

  check_health = function(ctx, credential)
    if credential.id == "dead" then
      return { status = "unhealthy", message = "key revoked" }
    elseif credential.id == "flaky" then
      return { status = "unknown", message = "try later" }
    elseif credential.id == "broken" then
      return "not-a-table"
    end
    return { status = "healthy" }
  end,

  healthcheck_cooldown = 600,
})
`

func installHealthPlugin(t *testing.T, svc *Service) {
	t.Helper()
	if _, err := svc.Install([]byte(healthPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
}

func TestCheckHealth_Verdicts(t *testing.T) {
	svc := setupService(t)
	installHealthPlugin(t, svc)
	for id, want := range map[string]HealthStatus{
		"live":  HealthHealthy,
		"dead":  HealthUnhealthy,
		"flaky": HealthUnknown,
	} {
		status, _, err := svc.CheckHealth(context.Background(), "health-type", &models.Credential{ID: id})
		if err != nil {
			t.Fatalf("id %s: %v", id, err)
		}
		if status != want {
			t.Fatalf("id %s: got %q, want %q", id, status, want)
		}
	}
	status, message, err := svc.CheckHealth(context.Background(), "health-type", &models.Credential{ID: "dead"})
	if err != nil || status != HealthUnhealthy || message != "key revoked" {
		t.Fatalf("unhealthy must carry the message: %q %q %v", status, message, err)
	}
}

func TestCheckHealth_InvalidShapeIsInternal(t *testing.T) {
	svc := setupService(t)
	installHealthPlugin(t, svc)
	_, _, err := svc.CheckHealth(context.Background(), "health-type", &models.Credential{ID: "broken"})
	var ierr *models.PluginInternalError
	if !errors.As(err, &ierr) {
		t.Fatalf("invalid verdict must be internal, got %T (%v)", err, err)
	}
}

func TestCheckHealth_MissingHandler(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(markPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, _, err := svc.CheckHealth(context.Background(), "mark-type", &models.Credential{ID: "c1"})
	var nerr *notFoundError
	if !errors.As(err, &nerr) {
		t.Fatalf("missing handler must report not found, got %T (%v)", err, err)
	}
}

func TestHealthCooldown_OverrideAndDefault(t *testing.T) {
	svc := setupService(t)
	installHealthPlugin(t, svc)
	if got := svc.HealthCooldown("health-type"); got != 10*time.Minute {
		t.Fatalf("override: got %v, want 10m", got)
	}
	if got := svc.HealthCooldown("no-such-type"); got != DefaultHealthCooldown {
		t.Fatalf("default: got %v, want %v", got, DefaultHealthCooldown)
	}
}

func TestParseHealthCooldown_Bounds(t *testing.T) {
	svc := setupService(t)
	bad := []string{"59", "86401", "1.5", `"soon"`}
	for _, v := range bad {
		src := `--- @plugin Bad Cooldown
--- @author tester
--- @version 1.0.0
--- @router_version 0.3.0
--- @allow_host example.com

llm_router.register("bad-type", {
  complete = function(ctx, credential, request) end,
  healthcheck_cooldown = ` + v + `,
})
`
		if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err == nil {
			t.Fatalf("cooldown %s must reject install", v)
		}
	}
}
