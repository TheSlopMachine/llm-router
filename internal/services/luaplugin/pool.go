package luaplugin

import (
	"context"
	"errors"
	"io"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/pool"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
)

// UsageTracker records per-credential outcomes. It is implemented by the
// credential pool service and injected via SetUsageTracker, keeping this
// package free of import cycles.
type UsageTracker = pool.UsageTracker

// SetUsageTracker wires per-credential usage accounting for pool calls.
// Unset (nil) disables accounting; attempts still run.
func (s *Service) SetUsageTracker(t UsageTracker) { s.usage = t }

// SetExhaustedStore wires joint limit-key recording for rate/quota
// outcomes. Unset (nil) disables marking; attempts still run.
func (s *Service) SetExhaustedStore(e *exhausted.Service) { s.exhausted = e }

func isFatalPoolError(err error) bool {
	if errors.Is(err, ErrHandlerNotFound) {
		return true
	}
	// invalid_request is identical for every key: the request itself is
	// malformed, so iterating the pool only repeats the failure.
	var perr *models.ProviderError
	if errors.As(err, &perr) {
		return perr.Type == models.ErrorTypeInvalidRequest
	}
	return false
}

// runPool tries one attempt per credential in pool order and returns the
// first success. Every key is tried at most once; there are no repeat passes
// or backoff pauses. A missing handler fails immediately: it is identical
// for every key. Otherwise the last error is returned.
func runPool[T any](ctx context.Context, s *Service, model string, creds []*models.Credential, attempt func(context.Context, *models.Credential) (T, error)) (T, error) {
	log := s.logger
	if log != nil && model != "" {
		log = log.With("model", model)
	}
	return pool.Run(ctx, log, creds, s.usage, attempt, isFatalPoolError)
}

// runPoolStream is runPool for streaming calls. Failover is allowed only
// before the first byte reaches the client.
func (s *Service) runPoolStream(ctx context.Context, model string, w io.Writer, creds []*models.Credential, attempt func(context.Context, *models.Credential, io.Writer) error) error {
	log := s.logger
	if log != nil && model != "" {
		log = log.With("model", model)
	}
	return pool.RunStream(ctx, log, w, creds, s.usage, attempt, isFatalPoolError)
}

// withCredential pins one pool credential into a copy of the base meta.
func withCredential(meta HandlerMeta, cred *models.Credential) HandlerMeta {
	meta.Credential = cred
	return meta
}

// CompletePool tries the credential pool in order through the complete
// handler, at most once per credential.
func (s *Service) CompletePool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.ChatCompletionRequest,
) (*models.ChatCompletionResponse, error) {
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, error) {
		return s.Complete(ctx, withCredential(meta, cred), req)
	})
}

// CompleteStreamPool tries the credential pool in order through the
// complete_stream handler, at most once per credential.
func (s *Service) CompleteStreamPool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.ChatCompletionRequest,
	w io.Writer,
) error {
	return s.runPoolStream(ctx, req.Model.String(), w, creds, func(ctx context.Context, cred *models.Credential, w io.Writer) error {
		return s.CompleteStream(ctx, withCredential(meta, cred), req, w)
	})
}

// TranscribePool tries the credential pool in order through the transcribe
// handler, at most once per credential.
func (s *Service) TranscribePool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.TranscriptionRequest,
) (*models.TranscriptionResponse, error) {
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.TranscriptionResponse, error) {
		return s.Transcribe(ctx, withCredential(meta, cred), req)
	})
}

// SpeechPool tries the credential pool in order through the speech handler,
// at most once per credential.
func (s *Service) SpeechPool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.SpeechRequest,
) (*models.SpeechResponse, error) {
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.SpeechResponse, error) {
		return s.Speech(ctx, withCredential(meta, cred), req)
	})
}

// GenerateImagePool tries the credential pool in order through the
// generate_image handler, at most once per credential.
func (s *Service) GenerateImagePool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.ImageGenerationRequest,
) (*models.ImageGenerationResponse, error) {
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.ImageGenerationResponse, error) {
		return s.GenerateImage(ctx, withCredential(meta, cred), req)
	})
}

// EmbedPool tries the credential pool in order through the embed handler, at
// most once per credential.
func (s *Service) EmbedPool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.EmbeddingsRequest,
) (*models.EmbeddingsResponse, error) {
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.EmbeddingsResponse, error) {
		return s.Embed(ctx, withCredential(meta, cred), req)
	})
}
