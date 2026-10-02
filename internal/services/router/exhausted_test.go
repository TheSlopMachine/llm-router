package router

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

const exhEchoPluginSource = `--- @plugin Exh Echo
--- @author tester
--- @version 1.0.0
--- @router_version 0.3.0
--- @description Echoes the attempt credential id
--- @allow_host example.com

llm_router.register("exh-type", {
  complete = function(ctx, credential, request)
    return {
      id = "chatcmpl-exh", object = "chat.completion", created = 1700000000, model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = credential.id }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,

  validate_credentials = function(data)
    return true
  end,
})
`

func setupExhaustedRouter(t *testing.T) (*Service, *exhausted.Service, string, *models.Credential, *models.Credential) {
	t.Helper()
	database := testutil.SetupTestDB(t)

	providerSvc := provider.NewService(database)
	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("new plugin service: %v", err)
	}
	providerSvc.SetLuaService(luaSvc)
	if _, err := luaSvc.Install([]byte(exhEchoPluginSource), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install echo plugin: %v", err)
	}
	inst, err := providerSvc.Create(provider.CreateOptions{Name: "Exh", TypeKey: "exh-type"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	credSvc := credential.New(database, providerSvc)
	credA, err := credSvc.Add(credential.AddOptions{ProviderID: inst.ID, Label: "a", Data: map[string]any{"api_key": "key-a"}})
	if err != nil {
		t.Fatalf("add cred a: %v", err)
	}
	credB, err := credSvc.Add(credential.AddOptions{ProviderID: inst.ID, Label: "b", Data: map[string]any{"api_key": "key-b"}})
	if err != nil {
		t.Fatalf("add cred b: %v", err)
	}
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)
	exhaustedSvc := exhausted.New(database)
	routerSvc := New(providerSvc, credSvc, modelInfoSvc, exhaustedSvc, videojobs.New(database), slog.Default())
	return routerSvc, exhaustedSvc, inst.ID, credA, credB
}

func exhComplete(t *testing.T, svc *Service, providerID string) string {
	t.Helper()
	resp, err := svc.Complete(context.Background(), &models.ChatCompletionRequest{
		Model:    models.ModelId(providerID + "/model-a"),
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("empty choices")
	}
	return resp.Choices[0].Message.TextContent()
}

func TestRouterService_DropExhaustedCredential(t *testing.T) {
	svc, exhaustedSvc, providerID, credA, credB := setupExhaustedRouter(t)
	rec, err := svc.providerSvc.LuaService().Lookup("exh-type")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	key, err := exhausted.KeyFromScope(rec.ID, providerID, credA.ID, providerID+"/model-a", "", []string{"account"})
	if err != nil {
		t.Fatalf("scope key: %v", err)
	}
	if err := exhaustedSvc.Mark(key, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if got := exhComplete(t, svc, providerID); got != credB.ID {
		t.Fatalf("limited credential must be skipped: served by %q, want %q", got, credB.ID)
	}
}

func TestRouterService_AllLimitedKeepsPoolAsLastResort(t *testing.T) {
	svc, exhaustedSvc, providerID, credA, credB := setupExhaustedRouter(t)
	rec, err := svc.providerSvc.LuaService().Lookup("exh-type")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	for _, cred := range []*models.Credential{credA, credB} {
		key, err := exhausted.KeyFromScope(rec.ID, providerID, cred.ID, providerID+"/model-a", "", []string{"account"})
		if err != nil {
			t.Fatalf("scope key: %v", err)
		}
		if err := exhaustedSvc.Mark(key, time.Now().Add(time.Hour), "test"); err != nil {
			t.Fatalf("mark: %v", err)
		}
	}
	got := exhComplete(t, svc, providerID)
	if got != credA.ID && got != credB.ID {
		t.Fatalf("last-resort pool must still serve: got %q", got)
	}
}

func TestRouterService_LikelyExhausted(t *testing.T) {
	svc, exhaustedSvc, providerID, credA, _ := setupExhaustedRouter(t)
	model := models.ModelId(providerID + "/model-a")

	if svc.LikelyExhausted(model) {
		t.Fatal("unmarked model must not report likely exhausted")
	}

	// An account-only mark says nothing about the model as a whole: other
	// accounts could still serve it.
	rec, err := svc.providerSvc.LuaService().Lookup("exh-type")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	acctKey, err := exhausted.KeyFromScope(rec.ID, providerID, credA.ID, model.String(), "", []string{"account"})
	if err != nil {
		t.Fatalf("scope key: %v", err)
	}
	if err := exhaustedSvc.Mark(acctKey, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("mark account: %v", err)
	}
	if svc.LikelyExhausted(model) {
		t.Fatal("account-only mark must not report the model as likely exhausted")
	}

	// A model-wide mark applies regardless of account.
	modelKey, err := exhausted.KeyFromScope(rec.ID, providerID, "", model.String(), "", []string{"model"})
	if err != nil {
		t.Fatalf("scope key: %v", err)
	}
	if err := exhaustedSvc.Mark(modelKey, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("mark model: %v", err)
	}
	if !svc.LikelyExhausted(model) {
		t.Fatal("model-wide mark must report the model as likely exhausted")
	}
	// A different model on the same provider is unaffected.
	if svc.LikelyExhausted(models.ModelId(providerID + "/model-b")) {
		t.Fatal("a different model must not be reported as likely exhausted")
	}
}

func TestRouterService_InstancesOfSameTypeDontCollide(t *testing.T) {
	// Two configured instances of one adapter type — the way two "custom"
	// endpoints would be set up — have independent quotas. An account-less
	// mark written against one must not affect the other, even though both
	// share a plugin and a type key.
	svc, exhaustedSvc, providerAID, _, _ := setupExhaustedRouter(t)

	instB, err := svc.providerSvc.Create(provider.CreateOptions{Name: "Exh B", TypeKey: "exh-type", Qualifier: "b"})
	if err != nil {
		t.Fatalf("create second instance: %v", err)
	}
	if instB.ID == providerAID {
		t.Fatalf("second instance must have a distinct ID from %q, got the same", providerAID)
	}
	if _, err := svc.credSvc.Add(credential.AddOptions{ProviderID: instB.ID, Label: "b1", Data: map[string]any{"api_key": "key-b1"}}); err != nil {
		t.Fatalf("add cred to instance b: %v", err)
	}

	rec, err := svc.providerSvc.LuaService().Lookup("exh-type")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}

	modelA := models.ModelId(providerAID + "/model-a")
	modelB := models.ModelId(instB.ID + "/model-a")

	// Model-wide mark scoped to instance A only.
	keyA, err := exhausted.KeyFromScope(rec.ID, providerAID, "", modelA.String(), "", []string{"model"})
	if err != nil {
		t.Fatalf("scope key: %v", err)
	}
	if err := exhaustedSvc.Mark(keyA, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("mark: %v", err)
	}

	if !svc.LikelyExhausted(modelA) {
		t.Fatal("instance A's model must report likely exhausted")
	}
	if svc.LikelyExhausted(modelB) {
		t.Fatal("instance B must be unaffected by instance A's mark despite sharing a type key")
	}

	// End to end: a completion request against instance B's model-a must
	// still succeed normally — dropExhausted must not filter its (only)
	// credential based on instance A's mark.
	resp, err := svc.Complete(context.Background(), &models.ChatCompletionRequest{
		Model:    modelB,
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if err != nil {
		t.Fatalf("instance B complete: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("empty choices")
	}
}
