package provider

import (
	"context"
	"io"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// Completer routes chat completions through single-pass delegation. It is
// implemented by the router service. Virtual models fan out through it
// instead of holding a concrete router dependency, breaking the
// provider → router → provider construction cycle.
type Completer interface {
	Complete(ctx context.Context, req *models.ChatCompletionRequest, token *models.RouterToken) (*models.ChatCompletionResponse, error)
	CompleteStream(ctx context.Context, req *models.ChatCompletionRequest, w io.Writer, token *models.RouterToken) error
	// SubmitVideo routes one video generation submit and persists the
	// router-side job row.
	SubmitVideo(ctx context.Context, req *models.VideoGenerationRequest, token *models.RouterToken) (*models.VideoGenerationResponse, error)
}
