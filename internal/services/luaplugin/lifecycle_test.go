package luaplugin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

func wireParks(svc *Service, store *lifecycleStore) {
	svc.SetCredentialParks(store.park, store.unpark, store.parked)
}

const lifecyclePluginSource = `--- @plugin Lifecycle Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("life-type", {
  complete = function(ctx, request)
    local mode = request.model_name
    if mode == "disable" then
      llm_router.credentials.disable("c1", "dead key")
      return nil, { message = "disabled" }
    end
    if mode == "enable" then
      llm_router.credentials.enable("c1")
      return nil, { message = "enabled" }
    end
    if mode == "park" then
      llm_router.credentials.park("c1", 60, "quota")
      return nil, { message = "parked" }
    end
    if mode == "unpark" then
      llm_router.credentials.unpark("c1")
      return nil, { message = "unparked" }
    end
    if mode == "parked" then
      local p = llm_router.credentials.parked("c1")
      if p == nil then return nil, { message = "parked=nil" } end
      return nil, { message = "parked=" .. tostring(p.reason) }
    end
    if mode == "unwired-disable" then
      llm_router.credentials.disable("c1")
      return nil, { message = "disabled" }
    end
    local ids = {}
    for _, c in ipairs(llm_router.credentials.list()) do
      table.insert(ids, c.id)
    end
    return nil, { message = "list:" .. table.concat(ids, ",") }
  end,
  credential_schema = {
    { type = "secret", name = "api_key", label = "API Key" },
  },
})
`

type lifecycleStore struct {
	creds map[string]*models.Credential
	parks map[string]*models.ParkEntry
}

func newLifecycleStore() *lifecycleStore {
	return &lifecycleStore{
		creds: map[string]*models.Credential{
			"c1": {ID: "c1", ProviderID: "life-type", Data: map[string]any{"api_key": "k"}},
			"c2": {ID: "c2", ProviderID: "life-type", Data: map[string]any{"api_key": "k"}},
		},
		parks: map[string]*models.ParkEntry{},
	}
}

func (s *lifecycleStore) expired(id string) bool {
	p, ok := s.parks[id]
	if !ok {
		return false
	}
	return time.Now().After(p.Until)
}

func (s *lifecycleStore) list(providerID string) ([]*models.Credential, error) {
	var out []*models.Credential
	for _, c := range s.creds {
		if c.ProviderID == providerID && !c.Disabled {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *lifecycleStore) park(id string, ttl time.Duration, reason string) error {
	if _, ok := s.creds[id]; !ok {
		return errors.New("unknown credential")
	}
	s.parks[id] = &models.ParkEntry{CredentialID: id, Reason: reason, Until: time.Now().Add(ttl)}
	return nil
}

func (s *lifecycleStore) unpark(id string) error {
	delete(s.parks, id)
	return nil
}

func (s *lifecycleStore) parked(id string) (*models.ParkEntry, error) {
	if s.expired(id) {
		delete(s.parks, id)
		return nil, nil
	}
	return s.parks[id], nil
}

func (s *lifecycleStore) disable(id, reason string) error {
	c, ok := s.creds[id]
	if !ok {
		return errors.New("unknown credential")
	}
	if c.Disabled {
		return nil
	}
	c.Disabled = true
	c.DisabledBy = "plugin"
	c.DisabledReason = reason
	return nil
}

func (s *lifecycleStore) enable(id string) error {
	c, ok := s.creds[id]
	if !ok {
		return errors.New("unknown credential")
	}
	if c.DisabledBy == "plugin" {
		c.Disabled = false
		c.DisabledBy = ""
		c.DisabledReason = ""
	}
	return nil
}

func lifecycleCall(t *testing.T, svc *Service, model string) *models.ProviderError {
	t.Helper()
	_, err := svc.Complete(context.Background(), testMeta("life-type", nil, models.ModelId("life-type/"+model), nil),
		&models.ChatCompletionRequest{Model: models.ModelId("life-type/" + model)})
	perr, ok := err.(*models.ProviderError)
	if !ok {
		t.Fatalf("expected terminal error, got %T (%v)", err, err)
	}
	return perr
}

func TestCredentialsDisableEnableSharedState(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(lifecyclePluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	store := newLifecycleStore()
	automate := true
	svc.SetCredentialAccess(
		store.list,
		func(id string) (*models.Credential, error) { return store.creds[id], nil },
		func(id string, data map[string]any) error { return nil },
	)
	svc.SetCredentialLifecycle(store.disable, store.enable, func(providerID string) bool { return automate })

	if got := lifecycleCall(t, svc, "disable"); got.Message != "disabled" {
		t.Fatalf("disable: %+v", got)
	}
	c1 := store.creds["c1"]
	if !c1.Disabled || c1.DisabledBy != "plugin" || c1.DisabledReason != "dead key" {
		t.Fatalf("shared state: %+v", c1)
	}
	// Disabled rows leave list results at once.
	if got := lifecycleCall(t, svc, "list"); got.Message != "list:c2" {
		t.Fatalf("list after disable: %+v", got)
	}
	if got := lifecycleCall(t, svc, "enable"); got.Message != "enabled" {
		t.Fatalf("enable: %+v", got)
	}
	if c1.Disabled || c1.DisabledBy != "" {
		t.Fatalf("enable must clear: %+v", c1)
	}
}

func TestCredentialsLifecycleGatedOff(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(lifecyclePluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	store := newLifecycleStore()
	svc.SetCredentialAccess(
		store.list,
		func(id string) (*models.Credential, error) { return store.creds[id], nil },
		func(id string, data map[string]any) error { return nil },
	)
	svc.SetCredentialLifecycle(store.disable, store.enable, func(providerID string) bool { return false })

	_, err := svc.Complete(context.Background(), testMeta("life-type", nil, "life-type/disable", nil),
		&models.ChatCompletionRequest{Model: "life-type/disable"})
	var ierr *models.PluginInternalError
	if !errors.As(err, &ierr) || !strings.Contains(ierr.Cause, "automation disabled") {
		t.Fatalf("switch off must deny loudly, got %T (%v)", err, err)
	}
	if store.creds["c1"].Disabled {
		t.Fatal("denied disable must not mutate")
	}
}

func TestCredentialsParkRoundTrip(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(lifecyclePluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	store := newLifecycleStore()
	svc.SetCredentialAccess(
		store.list,
		func(id string) (*models.Credential, error) { return store.creds[id], nil },
		func(id string, data map[string]any) error { return nil },
	)
	wireParks(svc, store)

	if got := lifecycleCall(t, svc, "park"); got.Message != "parked" {
		t.Fatalf("park: %+v", got)
	}
	if got := lifecycleCall(t, svc, "parked"); got.Message != "parked=quota" {
		t.Fatalf("parked introspection: %+v", got)
	}
	// Parked rows leave list results at once.
	if got := lifecycleCall(t, svc, "list"); got.Message != "list:c2" {
		t.Fatalf("list after park: %+v", got)
	}
	if got := lifecycleCall(t, svc, "unpark"); got.Message != "unparked" {
		t.Fatalf("unpark: %+v", got)
	}
	if got := lifecycleCall(t, svc, "parked"); got.Message != "parked=nil" {
		t.Fatalf("parked after unpark: %+v", got)
	}
}
