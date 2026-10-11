package provider

import (
	"context"
	"errors"
	"io"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// GoAdapter is implemented by built-in Go backends ("custom", "virtual").
// Lua plugin types are served by luaplugin.Service instead.
//
// Request methods take the whole sorted credential pool: the backend tries
// keys in order, at most once each, and returns a single result. Key-level
// failures never leave the backend; there are no repeat passes or backoff
// pauses in the request path.
type GoAdapter interface {
	TypeKey() string
	ValidateCredentials(data map[string]any) error
	Complete(ctx context.Context, credentials []*models.Credential, request *models.ChatCompletionRequest, providerConfig map[string]any) (*models.ChatCompletionResponse, error)
	CompleteStream(ctx context.Context, credentials []*models.Credential, request *models.ChatCompletionRequest, w io.Writer, providerConfig map[string]any) error
	NeedsRefresh(credential *models.Credential) bool
	RefreshCredential(ctx context.Context, credential *models.Credential) (map[string]any, error)
	GetModelInfos(ctx context.Context, credential *models.Credential, providerConfig map[string]any) ([]models.ModelInfo, error)
}

// Transcriber is an optional GoAdapter capability serving
// POST /v1/audio/transcriptions. Lua plugin types implement the equivalent
// via the transcribe handler.
type Transcriber interface {
	Transcribe(ctx context.Context, credentials []*models.Credential, request *models.TranscriptionRequest, providerConfig map[string]any) (*models.TranscriptionResponse, error)
}

// Speaker is an optional GoAdapter capability serving
// POST /v1/audio/speech. Lua plugin types implement the equivalent
// via the speech handler.
type Speaker interface {
	Speech(ctx context.Context, credentials []*models.Credential, request *models.SpeechRequest, providerConfig map[string]any) (*models.SpeechResponse, error)
}

// ImageGenerator is an optional GoAdapter capability serving
// POST /v1/images/generations. Lua plugin types implement the equivalent
// via the generate_image handler.
type ImageGenerator interface {
	GenerateImage(ctx context.Context, credentials []*models.Credential, request *models.ImageGenerationRequest, providerConfig map[string]any) (*models.ImageGenerationResponse, error)
}

// Embedder is an optional GoAdapter capability serving
// POST /v1/embeddings. Lua plugin types implement the equivalent
// via the embed handler.
type Embedder interface {
	Embed(ctx context.Context, credentials []*models.Credential, request *models.EmbeddingsRequest, providerConfig map[string]any) (*models.EmbeddingsResponse, error)
}

// Moderator is an optional GoAdapter capability serving
// POST /v1/moderations. Lua plugin types implement the equivalent
// via the moderate handler.
type Moderator interface {
	Moderate(ctx context.Context, credentials []*models.Credential, request *models.ModerationRequest, providerConfig map[string]any) (*models.ModerationResponse, error)
}

// VideoSubmitter is the narrow video capability virtual fan-out needs:
// submit one video job. Full backends implement VideoGenerator.
type VideoSubmitter interface {
	SubmitVideo(ctx context.Context, credentials []*models.Credential, request *models.VideoGenerationRequest, providerConfig map[string]any) (*models.VideoGenerationResponse, error)
}

// VideoGenerator is an optional GoAdapter capability serving
// /v1/videos. Lua plugin types implement the equivalent via the
// generate_video, poll_video and video_content handlers.
type VideoGenerator interface {
	VideoSubmitter
	PollVideo(ctx context.Context, credentials []*models.Credential, model models.ModelId, upstreamJobID string, providerConfig map[string]any) (*models.VideoGenerationResponse, error)
	VideoContent(ctx context.Context, credentials []*models.Credential, model models.ModelId, upstreamJobID string, index int, providerConfig map[string]any) (*models.VideoContentResponse, error)
}

// Built-in type keys served by Go code.
const (
	TypeCustom  = "custom"
	TypeVirtual = "virtual"
)

// ErrNotRefreshable is returned by Go adapters that never refresh credentials.
var ErrNotRefreshable = errors.New("credential type does not support refresh")

// RegisterGoAdapter registers a built-in Go backend.
func (s *GormService) RegisterGoAdapter(adapter GoAdapter) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.goAdapters[adapter.TypeKey()] = adapter
}

// GoAdapterFor returns the Go backend for a type key, if one is registered.
func (s *GormService) GoAdapterFor(typeKey string) (GoAdapter, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	adapter, ok := s.goAdapters[typeKey]
	return adapter, ok
}
