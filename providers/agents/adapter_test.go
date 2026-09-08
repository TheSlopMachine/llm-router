package agents

import (
	"context"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/agent"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func TestGetModelInfosUsesAgentID(t *testing.T) {
	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	credSvc := credential.New(database, providerSvc)
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)
	agentSvc := agent.New(database, providerSvc, modelInfoSvc)

	a := &models.Agent{Name: "My Helper", IsDraft: true}
	if err := agentSvc.Create(a); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	adapter := &Adapter{}
	adapter.SetAgentService(agentSvc)
	infos, err := adapter.GetModelInfos(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("model infos failed: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("infos: got %d, want 1", len(infos))
	}
	if infos[0].Name != "my-helper" {
		t.Errorf("info name: got %q, want agent ID %q", infos[0].Name, "my-helper")
	}
	if infos[0].DisplayName != "My Helper" {
		t.Errorf("display name: got %q", infos[0].DisplayName)
	}
}
