package provider

import (
	"context"
	"io"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// MemberRouter routes one request to one model in a single pass. It is
// implemented by the router service. Virtual models fan out to their members
// through it instead of holding a concrete router dependency, breaking the
// provider → router → provider construction cycle.
type MemberRouter interface {
	Complete(ctx context.Context, request *models.ChatCompletionRequest, token *models.RouterToken) (*models.ChatCompletionResponse, error)
	CompleteStream(ctx context.Context, request *models.ChatCompletionRequest, w io.Writer, token *models.RouterToken) error
	Transcribe(ctx context.Context, request *models.TranscriptionRequest, token *models.RouterToken) (*models.TranscriptionResponse, error)
	Speech(ctx context.Context, request *models.SpeechRequest, token *models.RouterToken) (*models.SpeechResponse, error)
	GenerateImage(ctx context.Context, request *models.ImageGenerationRequest, token *models.RouterToken) (*models.ImageGenerationResponse, error)
	Embed(ctx context.Context, request *models.EmbeddingsRequest, token *models.RouterToken) (*models.EmbeddingsResponse, error)
	Moderate(ctx context.Context, request *models.ModerationRequest, token *models.RouterToken) (*models.ModerationResponse, error)
	// SubmitVideo routes one video generation submit and persists the
	// router-side job row.
	SubmitVideo(ctx context.Context, request *models.VideoGenerationRequest, token *models.RouterToken) (*models.VideoGenerationResponse, error)
}
