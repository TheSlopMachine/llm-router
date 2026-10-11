package virtual

import (
	"context"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func newVirtualTestStack(t *testing.T) *GormService {
	t.Helper()
	database := testutil.SetupTestSQLite(t)
	ctx := context.Background()
	providerService := provider.NewGorm(database)
	providerService.RegisterGoAdapter(testutil.NewMockAdapter(provider.TypeVirtual).WithValidateFunc(func(map[string]any) error {
		return nil
	}))
	credentialService := credential.NewGorm(database, providerService)
	modelInfoService := modelinfo.NewGorm(database, providerService, credentialService, 1*time.Hour)
	if _, err := providerService.Create(ctx, provider.CreateOptions{Name: "Demo", TypeKey: "demo"}); err != nil {
		t.Fatalf("seed demo provider: %v", err)
	}
	return NewGorm(database, providerService, modelInfoService)
}

func TestLiveMembers_ManualUsesStoredList(t *testing.T) {
	service := newVirtualTestStack(t)
	ctx := context.Background()
	agent := &models.VirtualModel{
		Name:   "manual",
		Models: []models.VirtualModelEntry{{ModelID: "demo/a"}, {ModelID: "demo/b"}},
	}
	members, err := service.LiveMembers(ctx, agent)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if len(members) != 2 || members[0] != "demo/a" || members[1] != "demo/b" {
		t.Fatalf("members: %v", members)
	}
	if members, err := service.LiveMembers(ctx, nil); err == nil || len(members) != 0 {
		t.Fatalf("nil agent: %v %v", members, err)
	}
}

func TestLiveMembers_BadMarkerResolvesNothing(t *testing.T) {
	service := newVirtualTestStack(t)
	ctx := context.Background()
	for _, marker := range []string{"provider:", "provider:no-colon"} {
		agent := &models.VirtualModel{Name: "m", ManagedBy: marker}
		if members, err := service.LiveMembers(ctx, agent); err == nil || len(members) != 0 {
			t.Fatalf("marker %q: %v %v", marker, members, err)
		}
	}
	agent := &models.VirtualModel{Name: "m", ManagedBy: "provider:missing:chat"}
	if members, err := service.LiveMembers(ctx, agent); err != nil || len(members) != 0 {
		t.Fatalf("unknown provider: %v %v", members, err)
	}
}
