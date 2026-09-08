// Package router implements the Router Service.
//
// Responsibilities:
//   - Resolving a ModelId to the correct Provider
//   - Fetching live Credentials from the Credential Pool
//   - Intelligent retry with credential rotation on rate limits
//   - Delegating requests to Lua plugins or built-in Go adapters
//   - Translating backend-specific errors back to OpenAI-compatible ones
package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/retry"
)

// Service routes validated API requests to the appropriate provider.
type Service struct {
	providerSvc  *provider.Service
	credSvc      *credential.Service
	modelInfoSvc *modelinfo.Service
	mu           sync.RWMutex
	maxRetries   int
	logger       *slog.Logger
}

// New constructs a new router Service.
func New(providerSvc *provider.Service, credSvc *credential.Service, modelInfoSvc *modelinfo.Service, maxRetries int, logger *slog.Logger) *Service {
	if maxRetries < 0 || maxRetries > 20 {
		maxRetries = 7
	}
	return &Service{
		providerSvc:  providerSvc,
		credSvc:      credSvc,
		modelInfoSvc: modelInfoSvc,
		maxRetries:   maxRetries,
		logger:       logger,
	}
}

// SetMaxRetries updates the retry limit at runtime.
func (s *Service) SetMaxRetries(n int) {
	if n < 0 || n > 20 {
		return
	}
	s.mu.Lock()
	s.maxRetries = n
	s.mu.Unlock()
}

func (s *Service) getMaxRetries() int {
	s.mu.RLock()
	n := s.maxRetries
	s.mu.RUnlock()
	return n
}

// filterCredentials filters the credential list according to token rules.
// A nil token means no restriction (e.g. internal agent calls).
func (s *Service) filterCredentials(creds []*models.Credential, token *models.RouterToken) []*models.Credential {
	if token == nil || token.Rules.AllowAllCredentials {
		return creds
	}
	if len(token.Rules.AllowedCredentials) == 0 {
		return nil
	}
	allow := make(map[string]bool, len(token.Rules.AllowedCredentials))
	for _, id := range token.Rules.AllowedCredentials {
		allow[id] = true
	}
	out := make([]*models.Credential, 0, len(creds))
	for _, c := range creds {
		if allow[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

func (s *Service) completeOne(ctx context.Context, resolved *provider.Resolved, cred *models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
	cfg := resolved.Instance.Config
	if resolved.IsLua() {
		return s.providerSvc.LuaService().Complete(ctx, resolved.Instance.TypeKey, cred, req, cfg)
	}
	return resolved.Go.Complete(ctx, cred, req, cfg)
}

func (s *Service) completeStreamOne(ctx context.Context, resolved *provider.Resolved, cred *models.Credential, req *models.ChatCompletionRequest, w io.Writer) error {
	cfg := resolved.Instance.Config
	if resolved.IsLua() {
		return s.providerSvc.LuaService().CompleteStream(ctx, resolved.Instance.TypeKey, cred, req, w, cfg)
	}
	return resolved.Go.CompleteStream(ctx, cred, req, w, cfg)
}

func (s *Service) loadCredentials(p *models.ProviderInstance, token *models.RouterToken) ([]*models.Credential, error) {
	creds, err := s.credSvc.All(p.ID)
	if err != nil {
		return nil, fmt.Errorf("%w for provider %q", apierrors.ErrNoCredential, p.Name)
	}
	creds = s.filterCredentials(creds, token)
	if len(creds) == 0 {
		return nil, fmt.Errorf("%w for provider %q", apierrors.ErrCredentialNotAllowed, p.Name)
	}
	return creds, nil
}

// buildCandidates maps credentials to retry candidates with usage tracking.
func (s *Service) buildCandidates(ctx context.Context, resolved *provider.Resolved, creds []*models.Credential, req *models.ChatCompletionRequest) []retry.Candidate[*models.ChatCompletionResponse] {
	candidates := make([]retry.Candidate[*models.ChatCompletionResponse], 0, len(creds))
	for _, cred := range creds {
		cred := cred
		candidates = append(candidates, retry.Candidate[*models.ChatCompletionResponse]{
			Label: cred.ID,
			Run: func(ctx context.Context) (*models.ChatCompletionResponse, error) {
				resp, err := s.completeOne(ctx, resolved, cred, req)
				if err == nil {
					_ = s.credSvc.UpdateUsage(cred.ID, true)
					return resp, nil
				}
				_ = s.credSvc.UpdateUsage(cred.ID, false)
				if perr, ok := asQuotaExceeded(err); ok && perr.RetryAfter != nil {
					_ = s.credSvc.MarkQuotaExceeded(cred.ID, *perr.RetryAfter)
				}
				return nil, err
			},
		})
	}
	return candidates
}

func asQuotaExceeded(err error) (*models.ProviderError, bool) {
	var perr *models.ProviderError
	if errors.As(err, &perr) && perr.Type == models.ErrorTypeQuotaExceeded {
		return perr, true
	}
	return nil, false
}

// Complete routes a non-streaming chat completion request with credential
// rotation via the shared retry engine and exponential backoff across cycles.
func (s *Service) Complete(
	ctx context.Context,
	req *models.ChatCompletionRequest,
	token *models.RouterToken,
) (*models.ChatCompletionResponse, error) {
	providerID, _, err := req.Model.Parse()
	if err != nil {
		return nil, fmt.Errorf("invalid model id: %w", err)
	}
	resolved, err := provider.Resolve(s.providerSvc, providerID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", apierrors.ErrProviderNotFound, providerID)
	}
	// Virtual agents resolve from the model name suffix and need no
	// credentials; token credential rules do not apply to them.
	if resolved.Instance.TypeKey == provider.TypeAgents {
		return s.completeOne(ctx, resolved, nil, req)
	}
	creds, err := s.loadCredentials(resolved.Instance, token)
	if err != nil {
		return nil, err
	}
	maxRetries := s.getMaxRetries()
	for cycle := 0; cycle <= maxRetries; cycle++ {
		if cycle > 0 {
			delay := time.Duration(1<<(cycle-1)) * time.Second
			s.logger.Warn("all credentials rate limited, backing off",
				"cycle", cycle, "max", maxRetries, "delay", delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			creds, err = s.loadCredentials(resolved.Instance, token)
			if err != nil {
				return nil, err
			}
		}
		resp, err := retry.Run(ctx, s.buildCandidates(ctx, resolved, creds, req), s.logger)
		if err == nil {
			return resp, nil
		}
		if !retry.Classify(err) {
			return nil, err
		}
		if cycle == maxRetries {
			return nil, err
		}
	}
	return nil, fmt.Errorf("all credentials exhausted after %d retries", maxRetries)
}

// CompleteStream routes a streaming chat completion request.
// Server-Sent Events are written directly to w.
func (s *Service) CompleteStream(
	ctx context.Context,
	req *models.ChatCompletionRequest,
	w io.Writer,
	token *models.RouterToken,
) error {
	providerID, _, err := req.Model.Parse()
	if err != nil {
		return fmt.Errorf("invalid model id: %w", err)
	}
	resolved, err := provider.Resolve(s.providerSvc, providerID)
	if err != nil {
		return fmt.Errorf("%w: %s", apierrors.ErrProviderNotFound, providerID)
	}
	if resolved.Instance.TypeKey == provider.TypeAgents {
		return s.completeStreamOne(ctx, resolved, nil, req, w)
	}
	creds, err := s.loadCredentials(resolved.Instance, token)
	if err != nil {
		return err
	}
	maxRetries := s.getMaxRetries()
	for cycle := 0; cycle <= maxRetries; cycle++ {
		if cycle > 0 {
			delay := time.Duration(1<<(cycle-1)) * time.Second
			s.logger.Warn("all credentials rate limited, backing off",
				"cycle", cycle, "max", maxRetries, "delay", delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
			creds, err = s.loadCredentials(resolved.Instance, token)
			if err != nil {
				return err
			}
		}
		candidates := make([]retry.StreamCandidate, 0, len(creds))
		for _, cred := range creds {
			cred := cred
			candidates = append(candidates, retry.StreamCandidate{
				Label: cred.ID,
				Run: func(ctx context.Context, w io.Writer) error {
					err := s.completeStreamOne(ctx, resolved, cred, req, w)
					if err == nil {
						_ = s.credSvc.UpdateUsage(cred.ID, true)
						return nil
					}
					_ = s.credSvc.UpdateUsage(cred.ID, false)
					if perr, ok := asQuotaExceeded(err); ok && perr.RetryAfter != nil {
						_ = s.credSvc.MarkQuotaExceeded(cred.ID, *perr.RetryAfter)
					}
					return err
				},
			})
		}
		err := retry.RunStream(ctx, candidates, w, s.logger)
		if err == nil {
			return nil
		}
		if !retry.Classify(err) {
			return err
		}
		if cycle == maxRetries {
			return err
		}
	}
	return fmt.Errorf("all credentials exhausted after %d retries", maxRetries)
}

// GetProviderIDForModel returns the composite provider ID for a given model.
func (s *Service) GetProviderIDForModel(ctx context.Context, modelID models.ModelId) (string, error) {
	providerID, _, err := modelID.Parse()
	if err != nil {
		return "", err
	}
	_, err = s.providerSvc.Get(providerID)
	if err != nil {
		return "", err
	}
	return providerID, nil
}
