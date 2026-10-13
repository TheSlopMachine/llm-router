package router_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	"github.com/TheSlopMachine/llm-router/internal/testutil/mockplugin"
	virtualadapter "github.com/TheSlopMachine/llm-router/providers/virtual"
)

// setupLuaVirtualStack wires a router where the virtual member resolves
// through a Lua fixture instead of a Go adapter.
func setupLuaVirtualStack(t *testing.T) (*router.Service, string) {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()

	luaService, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("new plugin service: %v", err)
	}
	providerService := provider.NewGorm(database)
	providerService.SetLuaService(luaService)
	fixture := mockplugin.Chat()
	if _, err := luaService.InstallSQLite(ctx, []byte(fixture.Source), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install %s: %v", fixture.TypeKey, err)
	}
	member, err := providerService.Create(ctx, provider.CreateOptions{Name: "MockPlugin", TypeKey: fixture.TypeKey})
	if err != nil {
		t.Fatalf("create member provider: %v", err)
	}
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Virtual models", TypeKey: provider.TypeVirtual}); err != nil {
		t.Fatalf("create virtual provider: %v", err)
	}

	credentialService := credential.NewGorm(database, providerService)
	if _, err := credentialService.Add(ctx, credential.AddOptions{
		ProviderID: member.ID,
		Label:      "member cred",
		Data:       map[string]any{"api_key": "test-key"},
	}); err != nil {
		t.Fatalf("add member credential: %v", err)
	}

	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)
	virtualService := virtual.NewGorm(database, providerService, modelInfoService)
	routerService := router.New(providerService, credentialService, modelInfoService, videojobs.New(database), nil, nil, slog.Default())

	providerService.RegisterGoAdapter(virtualadapter.New(routerService, virtualService, slog.Default()))

	virtualModel := &models.VirtualModel{
		Name:   "LuaE2E",
		Models: []models.VirtualModelEntry{{ModelID: models.ModelId(member.ID + "/chat")}},
	}
	if err := virtualService.Create(ctx, virtualModel); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	return routerService, virtualModel.ID
}

func TestMockPluginVirtual_ChatFanOut(t *testing.T) {
	routerService, virtualID := setupLuaVirtualStack(t)

	response, err := routerService.Complete(context.Background(), &models.ChatCompletionRequest{
		Model:    models.ModelId("virtual/" + virtualID),
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if len(response.Choices) != 1 || response.Choices[0].Message.Content != "mockplugin ok" {
		t.Fatalf("content: %+v", response.Choices)
	}
}
