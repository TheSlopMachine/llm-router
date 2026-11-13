package router_test

import (
	"context"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// A token granted only the virtual model reaches its members through it,
// with every credential of a member provider the token does not name.
func TestRouterVirtualGrantCoversMembers(t *testing.T) {
	routerService, _, _ := setupVirtualStack(t)

	virtualOnly := &models.RouterToken{ID: "virtual-only", Rules: models.TokenRules{
		AllowedProviders: []string{"virtual"},
		AllowedModels:    []models.ModelId{"virtual/e2e"},
	}}
	response, err := routerService.Complete(context.Background(), virtualRequest("virtual/e2e"), virtualOnly)
	if err != nil {
		t.Fatalf("member of a granted virtual model must route: %v", err)
	}
	if string(response.Model) != "mock/test-model" {
		t.Fatalf("response model: %q", response.Model)
	}
}

// The virtual grant does not open members to direct calls.
func TestRouterVirtualGrantKeepsMembersClosedDirectly(t *testing.T) {
	routerService, _, _ := setupVirtualStack(t)

	virtualOnly := &models.RouterToken{ID: "virtual-only", Rules: models.TokenRules{
		AllowedProviders:    []string{"virtual"},
		AllowedModels:       []models.ModelId{"virtual/e2e"},
		AllowAllCredentials: true,
	}}
	if _, err := routerService.Complete(context.Background(), virtualRequest("mock/test-model"), virtualOnly); err == nil {
		t.Fatal("direct member call outside the token rules must be denied")
	}
}

// Credential rules the token sets for a member provider still apply when the
// member is reached through a granted virtual model.
func TestRouterVirtualGrantKeepsExplicitCredentialRules(t *testing.T) {
	routerService, _, _ := setupVirtualStack(t)

	pinned := &models.RouterToken{ID: "pinned", Rules: models.TokenRules{
		AllowedProviders: []string{"virtual", "mock"},
		AllowedModels:    []models.ModelId{"virtual/e2e"},
		CredentialScopes: []models.CredentialScope{{ProviderID: "mock", CredentialIDs: []string{"another-credential"}}},
	}}
	if _, err := routerService.Complete(context.Background(), virtualRequest("virtual/e2e"), pinned); err == nil {
		t.Fatal("member must obey the credential scope the token sets for its provider")
	}
}
