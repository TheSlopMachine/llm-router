package router

import (
	"context"
	"log/slog"
	"testing"
	"time"

	stdErrors "errors"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
	"github.com/TheSlopMachine/llm-router/internal/testutil/mockplugin"
)

func TestDisabledPluginDeniesWithProviderDisabled(t *testing.T) {
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()
	luaService, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("new plugin service: %v", err)
	}
	fixture := mockplugin.Chat()
	installed, err := luaService.InstallSQLite(ctx, []byte(fixture.Source), luaplugin.PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	providerService := provider.NewGorm(database)
	providerService.SetLuaService(luaService)
	instance, err := providerService.Create(ctx, provider.CreateOptions{Name: "MockPlugin", TypeKey: fixture.TypeKey})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	credentialService := credential.NewGorm(database, providerService)
	if _, err := credentialService.Add(ctx, credential.AddOptions{
		ProviderID: instance.ID,
		Label:      "Mock",
		Data:       map[string]any{"api_key": "test-key"},
	}); err != nil {
		t.Fatalf("add credential: %v", err)
	}
	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)
	routerService := New(providerService, credentialService, modelInfoService, videojobs.New(database), nil, nil, slog.Default())

	if _, err := routerService.Complete(ctx, chatRequest(instance.ID+"/chat"), nil); err != nil {
		t.Fatalf("complete before disable: %v", err)
	}
	if _, err := luaService.SetDisabled(ctx, installed.ID, true); err != nil {
		t.Fatalf("disable plugin: %v", err)
	}
	_, err = routerService.Complete(ctx, chatRequest(instance.ID+"/chat"), nil)
	if !stdErrors.Is(err, apierrors.ErrProviderDisabled) {
		t.Fatalf("disabled plugin must deny with provider_disabled, got %v", err)
	}
	if _, err := luaService.SetDisabled(ctx, installed.ID, false); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	if _, err := routerService.Complete(ctx, chatRequest(instance.ID+"/chat"), nil); err != nil {
		t.Fatalf("complete after enable: %v", err)
	}
}
