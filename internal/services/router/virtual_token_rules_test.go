package router_test

import (
	"context"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// A token granted only the virtual model must not reach member models the
// token cannot call directly: members re-enter the router with the outer
// rules from ctx.
func TestRouterVirtualMembersObeyTokenRules(t *testing.T) {
	routerService, _, _ := setupVirtualStack(t)

	virtualOnly := &models.RouterToken{ID: "virtual-only", Rules: models.TokenRules{
		AllowedProviders:    []string{"virtual"},
		AllowedModels:       []models.ModelId{"virtual/e2e"},
		AllowAllCredentials: true,
	}}
	if _, err := routerService.Complete(context.Background(), virtualRequest("virtual/e2e"), virtualOnly); err == nil {
		t.Fatal("member outside the token rules must be denied")
	}

	withMember := &models.RouterToken{ID: "with-member", Rules: models.TokenRules{
		AllowedProviders:    []string{"virtual", "mock"},
		AllowedModels:       []models.ModelId{"virtual/e2e", "mock/test-model"},
		AllowAllCredentials: true,
	}}
	response, err := routerService.Complete(context.Background(), virtualRequest("virtual/e2e"), withMember)
	if err != nil {
		t.Fatalf("member inside the token rules must route: %v", err)
	}
	if string(response.Model) != "mock/test-model" {
		t.Fatalf("response model: %q", response.Model)
	}
}
