package luaplugin

import (
	"context"
	"errors"
	"io"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/pool"
)

// UsageTracker records per-credential outcomes. It is implemented by the
// credential pool service and injected via SetUsageTracker, keeping this
// package free of import cycles.
type UsageTracker = pool.UsageTracker

// SetUsageTracker wires per-credential usage accounting for pool calls.
// Unset (nil) disables accounting; attempts still run.
func (s *Service) SetUsageTracker(t UsageTracker) { s.usage = t }

func isFatalPoolError(err error) bool {
	return errors.Is(err, ErrHandlerNotFound)
}

// runPool tries one attempt per credential in pool order and returns the
// first success. Every key is tried at most once; there are no repeat passes
// or backoff pauses. A missing handler fails immediately: it is identical
// for every key. Otherwise the last error is returned.
func runPool[T any](ctx context.Context, s *Service, creds []*models.Credential, attempt func(context.Context, *models.Credential) (T, error)) (T, error) {
	return pool.Run(ctx, s.logger, creds, s.usage, attempt, isFatalPoolError)
}

// runPoolStream is runPool for streaming calls. Failover is allowed only
// before the first byte reaches the client.
func (s *Service) runPoolStream(ctx context.Context, w io.Writer, creds []*models.Credential, attempt func(context.Context, *models.Credential, io.Writer) error) error {
	return pool.RunStream(ctx, s.logger, w, creds, s.usage, attempt, isFatalPoolError)
}

// CompletePool tries the credential pool in order through the complete
// handler, at most once per credential.
func (s *Service) CompletePool(
	ctx context.Context,
	typeKey string,
	creds []*models.Credential,
	req *models.ChatCompletionRequest,
	providerConfig map[string]any,
) (*models.ChatCompletionResponse, error) {
	return runPool(ctx, s, creds, func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
		return s.Complete(ctx, typeKey, cred, req, providerConfig)
	})
}

// CompleteStreamPool tries the credential pool in order through the
// complete_stream handler, at most once per credential.
func (s *Service) CompleteStreamPool(
	ctx context.Context,
	typeKey string,
	creds []*models.Credential,
	req *models.ChatCompletionRequest,
	w io.Writer,
	providerConfig map[string]any,
) error {
	return s.runPoolStream(ctx, w, creds, func(ctx context.Context, cred *models.Credential, w io.Writer) error {
		return s.CompleteStream(ctx, typeKey, cred, req, w, providerConfig)
	})
}

// TranscribePool tries the credential pool in order through the transcribe
// handler, at most once per credential.
func (s *Service) TranscribePool(
	ctx context.Context,
	typeKey string,
	creds []*models.Credential,
	req *models.TranscriptionRequest,
	providerConfig map[string]any,
) (*models.TranscriptionResponse, error) {
	return runPool(ctx, s, creds, func(ctx context.Context, cred *models.Credential) (*models.TranscriptionResponse, error) {
		return s.Transcribe(ctx, typeKey, cred, req, providerConfig)
	})
}

// SpeechPool tries the credential pool in order through the speech handler,
// at most once per credential.
func (s *Service) SpeechPool(
	ctx context.Context,
	typeKey string,
	creds []*models.Credential,
	req *models.SpeechRequest,
	providerConfig map[string]any,
) (*models.SpeechResponse, error) {
	return runPool(ctx, s, creds, func(ctx context.Context, cred *models.Credential) (*models.SpeechResponse, error) {
		return s.Speech(ctx, typeKey, cred, req, providerConfig)
	})
}

// GenerateImagePool tries the credential pool in order through the
// generate_image handler, at most once per credential.
func (s *Service) GenerateImagePool(
	ctx context.Context,
	typeKey string,
	creds []*models.Credential,
	req *models.ImageGenerationRequest,
	providerConfig map[string]any,
) (*models.ImageGenerationResponse, error) {
	return runPool(ctx, s, creds, func(ctx context.Context, cred *models.Credential) (*models.ImageGenerationResponse, error) {
		return s.GenerateImage(ctx, typeKey, cred, req, providerConfig)
	})
}

// EmbedPool tries the credential pool in order through the embed handler, at
// most once per credential.
func (s *Service) EmbedPool(
	ctx context.Context,
	typeKey string,
	creds []*models.Credential,
	req *models.EmbeddingsRequest,
	providerConfig map[string]any,
) (*models.EmbeddingsResponse, error) {
	return runPool(ctx, s, creds, func(ctx context.Context, cred *models.Credential) (*models.EmbeddingsResponse, error) {
		return s.Embed(ctx, typeKey, cred, req, providerConfig)
	})
}
