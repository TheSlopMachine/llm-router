// Package router implements the Router Service.
//
// Responsibilities:
//   - Resolving a ModelId to the correct Provider
//   - Fetching live Credentials from the Credential Pool
//   - Intelligent retry with credential rotation on rate limits
//   - Delegating requests to Provider Adapters
//   - Translating adapter-specific errors back to OpenAI-compatible ones
package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/compaction"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

// Service routes validated API requests to the appropriate provider.
type Service struct {
	providerSvc   *provider.Service
	credSvc       *credential.Service
	modelInfoSvc  *modelinfo.Service
	compactionSvc *compaction.Service
	maxRetries    int
	logger        *slog.Logger
}

// New constructs a new router Service.
func New(providerSvc *provider.Service, credSvc *credential.Service, modelInfoSvc *modelinfo.Service, compactionSvc *compaction.Service, maxRetries int, logger *slog.Logger) *Service {
	if maxRetries <= 0 {
		maxRetries = 7
	}
	return &Service{
		providerSvc:   providerSvc,
		credSvc:       credSvc,
		modelInfoSvc:  modelInfoSvc,
		compactionSvc: compactionSvc,
		maxRetries:    maxRetries,
		logger:        logger,
	}
}

// ─────────────────────────────────────────────
// Core routing
// ─────────────────────────────────────────────

// Complete routes a non-streaming chat completion request.
//
// Resolution order:
//  1. Parse the ModelId to extract the provider prefix.
//  2. Find a registered Provider whose Type matches the prefix.
//  3. Get all available Credentials from the pool (sorted by priority/LRU).
//  4. Try each credential with intelligent retry on rate limits.
//  5. Apply exponential backoff when all credentials exhausted.
func (s *Service) Complete(
	ctx context.Context,
	req *models.ChatCompletionRequest,
) (*models.ChatCompletionResponse, error) {
	// Parse composite provider ID from model ID
	providerID, _, err := req.Model.Parse()
	if err != nil {
		return nil, fmt.Errorf("invalid model id: %w", err)
	}

	// Direct lookup by composite ID
	p, err := s.providerSvc.Get(providerID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrProviderNotFound, providerID)
	}

	// Get adapter by type
	adapter, err := provider.Lookup(p.Type)
	if err != nil {
		return nil, err
	}

	// Compact messages if needed
	modelInfo, err := s.modelInfoSvc.GetModelInfo(ctx, req.Model)
	if err != nil {
		s.logger.Warn("failed to get model info, skipping compaction", "model", req.Model, "err", err)
	} else if modelInfo.ContextWindow > 0 {
		compacted, stats, err := s.compactionSvc.CompactIfNeeded(req.Messages, req.Model, modelInfo.ContextWindow)
		if err != nil {
			s.logger.Warn("compaction failed, using original messages", "err", err)
		} else if stats.WasCompacted {
			s.logger.Info("compacted conversation",
				"original_tokens", stats.OriginalTokens,
				"compacted_tokens", stats.CompactedTokens,
				"budget", stats.Budget,
				"saved", stats.TokensSaved,
				"kept", stats.MessagesKept,
				"trimmed", stats.MessagesTrimmed,
				"dropped", stats.MessagesDropped)
			req.Messages = compacted
		}
	}

	// Get all available credentials sorted by priority/LRU
	creds, err := s.credSvc.All(p.ID)
	if err != nil {
		return nil, fmt.Errorf("%w for provider %q", apierrors.ErrNoCredential, p.Name)
	}

	// Track which credentials we've tried in this cycle
	attempted := make(map[string]bool)

	for cycle := 0; cycle <= s.maxRetries; cycle++ {
		if cycle > 0 {
			// All credentials exhausted, apply exponential backoff
			delay := time.Duration(1<<(cycle-1)) * time.Second
			s.logger.Warn("all credentials rate limited, backing off",
				"cycle", cycle, "max", s.maxRetries, "delay", delay)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			// Clear attempted set (rate limits may have reset)
			attempted = make(map[string]bool)

			// Refresh credential list (quota may have reset)
			creds, err = s.credSvc.All(p.ID)
			if err != nil {
				return nil, fmt.Errorf("%w for provider %q", apierrors.ErrNoCredential, p.Name)
			}
		}

		// Try each credential once per cycle
		for _, cred := range creds {
			if attempted[cred.ID] {
				continue
			}

			attempted[cred.ID] = true

		// Attempt request
		resp, err := adapter.Complete(ctx, cred.ToSDK(), req)

			if err == nil {
				// Success
				_ = s.credSvc.UpdateUsage(cred.ID, true)
				return resp, nil
			}

			// Check if this is a retryable provider error
			var provErr *provider.ProviderError
			if errors.As(err, &provErr) && provErr.IsRetryable() {
				s.logger.Info("rate limit or quota exceeded, rotating credential",
					"cred_id", cred.ID, "error_type", provErr.Type, "status", provErr.StatusCode)

				_ = s.credSvc.UpdateUsage(cred.ID, false)

				// Mark quota exceeded if applicable
				if provErr.Type == provider.ErrorTypeQuotaExceeded && provErr.RetryAfter != nil {
					_ = s.credSvc.MarkQuotaExceeded(cred.ID, *provErr.RetryAfter)
				}

				continue // Try next credential
			}

			// Non-retryable error, fail immediately
			_ = s.credSvc.UpdateUsage(cred.ID, false)
			return nil, err
		}
	}

	return nil, fmt.Errorf("all credentials exhausted after %d retries", s.maxRetries)
}

// CompleteStream routes a streaming chat completion request.
// Server-Sent Events are written directly to w.
func (s *Service) CompleteStream(
	ctx context.Context,
	req *models.ChatCompletionRequest,
	w io.Writer,
) error {
	// Parse composite provider ID from model ID
	providerID, _, err := req.Model.Parse()
	if err != nil {
		return fmt.Errorf("invalid model id: %w", err)
	}

	// Direct lookup by composite ID
	p, err := s.providerSvc.Get(providerID)
	if err != nil {
		return fmt.Errorf("%w: %s", apierrors.ErrProviderNotFound, providerID)
	}

	// Get adapter by type
	adapter, err := provider.Lookup(p.Type)
	if err != nil {
		return err
	}

	// Compact messages if needed
	modelInfo, err := s.modelInfoSvc.GetModelInfo(ctx, req.Model)
	if err != nil {
		s.logger.Warn("failed to get model info, skipping compaction", "model", req.Model, "err", err)
	} else if modelInfo.ContextWindow > 0 {
		compacted, stats, err := s.compactionSvc.CompactIfNeeded(req.Messages, req.Model, modelInfo.ContextWindow)
		if err != nil {
			s.logger.Warn("compaction failed, using original messages", "err", err)
		} else if stats.WasCompacted {
			s.logger.Info("compacted conversation",
				"original_tokens", stats.OriginalTokens,
				"compacted_tokens", stats.CompactedTokens,
				"budget", stats.Budget,
				"saved", stats.TokensSaved,
				"kept", stats.MessagesKept,
				"trimmed", stats.MessagesTrimmed,
				"dropped", stats.MessagesDropped)
			req.Messages = compacted
		}
	}

	// Get all available credentials sorted by priority/LRU
	creds, err := s.credSvc.All(p.ID)
	if err != nil {
		return fmt.Errorf("%w for provider %q", apierrors.ErrNoCredential, p.Name)
	}

	// Track which credentials we've tried in this cycle
	attempted := make(map[string]bool)

	for cycle := 0; cycle <= s.maxRetries; cycle++ {
		if cycle > 0 {
			// All credentials exhausted, apply exponential backoff
			delay := time.Duration(1<<(cycle-1)) * time.Second
			s.logger.Warn("all credentials rate limited, backing off",
				"cycle", cycle, "max", s.maxRetries, "delay", delay)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}

			// Clear attempted set (rate limits may have reset)
			attempted = make(map[string]bool)

			// Refresh credential list (quota may have reset)
			creds, err = s.credSvc.All(p.ID)
			if err != nil {
				return fmt.Errorf("%w for provider %q", apierrors.ErrNoCredential, p.Name)
			}
		}

		// Try each credential once per cycle
		for _, cred := range creds {
			if attempted[cred.ID] {
				continue
			}

			attempted[cred.ID] = true

		// Attempt request
		err := adapter.CompleteStream(ctx, cred.ToSDK(), req, w)

			if err == nil {
				// Success
				_ = s.credSvc.UpdateUsage(cred.ID, true)
				return nil
			}

			// Check if this is a retryable provider error
			var provErr *provider.ProviderError
			if errors.As(err, &provErr) && provErr.IsRetryable() {
				s.logger.Info("rate limit or quota exceeded, rotating credential",
					"cred_id", cred.ID, "error_type", provErr.Type, "status", provErr.StatusCode)

				_ = s.credSvc.UpdateUsage(cred.ID, false)

				// Mark quota exceeded if applicable
				if provErr.Type == provider.ErrorTypeQuotaExceeded && provErr.RetryAfter != nil {
					_ = s.credSvc.MarkQuotaExceeded(cred.ID, *provErr.RetryAfter)
				}

				continue // Try next credential
			}

			// Non-retryable error, fail immediately
			_ = s.credSvc.UpdateUsage(cred.ID, false)
			return err
		}
	}

	return fmt.Errorf("all credentials exhausted after %d retries", s.maxRetries)
}

// ─────────────────────────────────────────────
// Helper methods
// ─────────────────────────────────────────────

// GetProviderIDForModel returns the composite provider ID for a given model.
func (s *Service) GetProviderIDForModel(ctx context.Context, modelID models.ModelId) (string, error) {
	providerID, _, err := modelID.Parse()
	if err != nil {
		return "", err
	}

	// Verify provider exists
	_, err = s.providerSvc.Get(providerID)
	if err != nil {
		return "", err
	}

	return providerID, nil
}

