package router_test

import (
	"context"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// embeddingAdapter is the mock member backend with an embeddings capability.
type embeddingAdapter struct{ *testutil.MockAdapter }

func (embeddingAdapter) Embed(_ context.Context, _ []*models.Credential, request *models.EmbeddingsRequest, _ map[string]any) (*models.EmbeddingsResponse, error) {
	return &models.EmbeddingsResponse{Model: request.Model.String()}, nil
}

// A virtual model serves embeddings through its members, also once the
// virtual provider's own model cards are cached: those declare no endpoints.
func TestRouterVirtualServesEmbeddings(t *testing.T) {
	ctx := context.Background()
	stack := setupVirtualStackWithAdapter(t, embeddingAdapter{testutil.NewMockAdapter("mock")})
	if _, err := stack.modelInfoService.Refresh(ctx, provider.TypeVirtual); err != nil {
		t.Fatalf("refresh virtual model cards: %v", err)
	}
	if len(stack.modelInfoService.PeekModelInfos(ctx, provider.TypeVirtual)) == 0 {
		t.Fatal("virtual model cards must be cached for this test")
	}

	response, err := stack.routerService.Embed(ctx, &models.EmbeddingsRequest{Model: "virtual/e2e", Input: []string{"hi"}}, nil)
	if err != nil {
		t.Fatalf("virtual embeddings must route to the member: %v", err)
	}
	if response.Model != "mock/test-model" {
		t.Fatalf("response model: got %q, want the member %q", response.Model, "mock/test-model")
	}
}
