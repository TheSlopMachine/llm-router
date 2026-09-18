package virtual

import (
	"context"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func TestGetModelInfosUsesVirtualModelID(t *testing.T) {
	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)
	virtualSvc := virtual.New(database, providerSvc, modelInfoSvc)

	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Demo", TypeKey: "demo"}); err != nil {
		t.Fatalf("seed demo provider: %v", err)
	}
	vm := &models.VirtualModel{
		Name:   "My Helper",
		Models: []models.VirtualModelEntry{{ModelID: "demo/model-a"}},
	}
	if err := virtualSvc.Create(vm); err != nil {
		t.Fatalf("create virtual model: %v", err)
	}

	adapter := &Adapter{}
	adapter.SetVirtualService(virtualSvc)
	infos, err := adapter.GetModelInfos(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("model infos failed: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("infos: got %d, want 1", len(infos))
	}
	if infos[0].Name != "my-helper" {
		t.Errorf("info name: got %q, want virtual model ID %q", infos[0].Name, "my-helper")
	}
	if infos[0].DisplayName != "My Helper" {
		t.Errorf("display name: got %q", infos[0].DisplayName)
	}
}
