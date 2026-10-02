package provider

import (
	"context"
	"io"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// Completer routes chat completions through the credential pool. It is
// implemented by the router service. Virtual models fan out through it
// instead of holding a concrete router dependency, breaking the
// provider → router → provider construction cycle.
type Completer interface {
	Complete(ctx context.Context, req *models.ChatCompletionRequest, token *models.RouterToken) (*models.ChatCompletionResponse, error)
	CompleteStream(ctx context.Context, req *models.ChatCompletionRequest, w io.Writer, token *models.RouterToken) error
	// SubmitVideo routes one video generation submit through the
	// credential pool and persists the router-side job row.
	SubmitVideo(ctx context.Context, req *models.VideoGenerationRequest, token *models.RouterToken) (*models.VideoGenerationResponse, error)
	// LikelyExhausted is a cheap, best-effort pre-check: true means model
	// carries a model-wide limit key and is worth skipping without an
	// attempt. False is not a guarantee of success. Implementations with no
	// exhausted store wired always return false.
	LikelyExhausted(model models.ModelId) bool
	// HasUsableCredential reports whether at least one credential for the
	// model is not rate-limited. True means the model is worth attempting;
	// false means every credential is in cooldown. Implementations with no
	// exhausted store wired always return true.
	HasUsableCredential(model models.ModelId) bool
}
