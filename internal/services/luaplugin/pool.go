package luaplugin

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/pool"
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
)

// UsageTracker records per-credential outcomes. It is implemented by the
// credential pool service and injected via SetUsageTracker, keeping this
// package free of import cycles.
type UsageTracker = pool.UsageTracker

// ProxyLimitStore persists proxy-scoped limit keys in pool metadata.
type ProxyLimitStore interface {
	MarkLimit(proxyID, key string, resetsAt time.Time, reason string) error
}

// SetUsageTracker wires per-credential usage accounting for pool calls.
// Unset (nil) disables accounting; attempts still run.
func (s *Service) SetUsageTracker(t UsageTracker) { s.usage = t }

// SetExhaustedStore wires joint limit-key recording for rate/quota/
// model_unavailable outcomes. Unset (nil) disables marking; attempts still run.
func (s *Service) SetExhaustedStore(e *exhausted.Service) { s.exhausted = e }

// SetProxyLimitStore routes proxy-scoped limits to the proxy library cache.
func (s *Service) SetProxyLimitStore(p ProxyLimitStore) { s.proxyLimits = p }

// SetGeoBanStore wires indefinite geo-flag recording for geo outcomes.
// Unset (nil) disables flagging; attempts still run.
func (s *Service) SetGeoBanStore(g interface {
	Mark(plugin, provider, proxy, reason string) error
}) {
	s.geoban = g
}

// SetHealthTrigger wires failure-triggered health-check dispatch.
// Unset (nil) disables it; attempts still run.
func (s *Service) SetHealthTrigger(t HealthTrigger) { s.healthTrigger = t }

// SetMarkDead wires manual proxy exclusion for upstream-observed faults.
// Unset (nil) disables it.
func (s *Service) SetMarkDead(f func(url, reason string) bool) {
	s.markDead = f
}

// SetDumpDir wires the directory for full upstream-body spill files in
// debug mode. Empty disables spilling.
func (s *Service) SetDumpDir(dir string) { s.dumpDir = dir }

func isFatalPoolError(err error) bool {
	return isFatalWithRetry(err, models.ProxyRetryFailFast)
}

// isFatalWithRetry reports whether err stops the credential pool
// immediately. invalid_request, content_policy and structural_fault are
// identical for every key and model of the provider, so iterating only
// repeats the failure. Geo stops the pool only in fail_fast mode; in
// next_proxy mode the pool advances to the next credential after
// same-credential retries run out (the banned exit stays excluded through
// the geo flag). Rate, quota, transport and overload failures always fail
// over: the retry policy governs same-credential proxy retries, never
// credential failover.
func isFatalWithRetry(err error, retryMode string) bool {
	if errors.Is(err, ErrHandlerNotFound) {
		return true
	}
	var perr *models.ProviderError
	if errors.As(err, &perr) {
		switch perr.Type {
		case models.ErrorTypeInvalidRequest:
			// Identical for every key: the request itself is malformed, so
			// iterating the pool only repeats the failure.
			return true
		case models.ErrorTypeContentPolicy:
			// The content of this request is rejected, not the credential:
			// retrying keys cannot change the verdict. Virtual-model
			// fall-through is unaffected — it moves to the next member on
			// any error, fatal or not.
			return true
		case models.ErrorTypeStructuralFault:
			// Provider config/network is broken for all keys and models.
			// The provider is disabled by the outcome effect; stop at once.
			return true
		case models.ErrorTypeGeo:
			return retryMode != models.ProxyRetryNextProxy
		}
	}
	return false
}

// fatalReason renders the typed stop reason for pool fatal decisions.
func fatalReason(err error) string {
	if errors.Is(err, ErrHandlerNotFound) {
		return "handler_not_found"
	}
	var perr *models.ProviderError
	if errors.As(err, &perr) {
		return contractTypeName(perr.Type)
	}
	return "non_fatal"
}

// fatalWithLog reports whether err stops the credential pool, logging
// config-aware fatal decisions with the typed reason and retry mode.
func (s *Service) fatalWithLog(meta HandlerMeta, retry models.ProxyRetryConfig) func(error) bool {
	return func(err error) bool {
		fatal := isFatalWithRetry(err, retry.Mode)
		if fatal && s.logger != nil {
			s.logger.Debug("pool: fatal error, stopping pool",
				"type", meta.TypeKey, "provider_id", meta.ProviderID,
				"reason", fatalReason(err), "retry_mode", retry.Mode)
		}
		return fatal
	}
}

// retryPolicy resolves the provider proxy retry reaction for one pool pass.
// Unknown modes fail closed to fail_fast with a warning.
func (s *Service) retryPolicy(providerConfig map[string]any) models.ProxyRetryConfig {
	cfg, err := models.ParseProxyRetryConfig(providerConfig)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("pool: invalid proxy retry config, using fail_fast", "error", err)
		}
		return models.ProxyRetryConfig{Mode: models.ProxyRetryFailFast, MaxAttempts: models.DefaultProxyRetryMaxAttempts}
	}
	return cfg
}

// shouldRetryProxy reports whether a proxy failure warrants another attempt
// with the same credential on an untried route: a proxied route (direct
// failures fail over to the next credential, as there is no other exit to
// try) carrying a retryable error. Geo blocks additionally require
// next_proxy mode; fail_fast stops the pool. Proxy-scoped and unscoped
// rate/quota limits retry; credential/model-only limits fail over.
func shouldRetryProxy(err error, route string, policy models.ProxyRetryConfig) bool {
	if route == "" {
		return false
	}
	var perr *models.ProviderError
	if !errors.As(err, &perr) {
		return false
	}
	switch perr.Type {
	case models.ErrorTypeGeo:
		return policy.Mode == models.ProxyRetryNextProxy
	case models.ErrorTypeRateLimit, models.ErrorTypeQuotaExceeded:
		if len(perr.Scope) == 0 {
			return true
		}
		for _, scope := range perr.Scope {
			if scope == models.ExhaustedScopeProxy {
				return true
			}
		}
		return false
	case models.ErrorTypeTransport, models.ErrorTypeOverloaded:
		return true
	default:
		return false
	}
}

// isGeoError reports whether err is a geo-block failure.
func isGeoError(err error) bool {
	var perr *models.ProviderError
	return errors.As(err, &perr) && perr.Type == models.ErrorTypeGeo
}

func streamCommitted(w io.Writer) bool {
	if w == nil {
		return false
	}
	tracker, ok := w.(interface{ Written() bool })
	return !ok || tracker.Written()
}

// runRoutedRetries attempts one credential with bounded same-credential
// proxy retries: every retryable proxy failure (geo blocks in next_proxy
// mode, proxy-scoped rate/quota limits, transport failures, overloads)
// retries on an untried route up to policy.MaxAttempts total attempts,
// stopping early when no alternate route remains, the context ends, the
// stream committed its first byte, or (streams only) the failure is a geo
// block.
func runRoutedRetries[T any](
	ctx context.Context,
	s *Service,
	meta HandlerMeta,
	policy models.ProxyRetryConfig,
	w io.Writer,
	attempt func(context.Context) (T, string, error),
) (T, string, error) {
	var lastRetryResult T
	var lastRetryRoute string
	var lastRetryErr error
	var excludedRoutes []string
	attempts := 0

	for {
		attemptCtx := withProxyRetryExclusions(ctx, excludedRoutes)
		result, route, err := attempt(attemptCtx)
		if errors.Is(err, ErrNoProxyRoute) && lastRetryErr != nil {
			if s.logger != nil {
				s.logger.Debug("pool: no alternate proxy for retry", "type", meta.TypeKey, "provider_id", meta.ProviderID)
			}
			return lastRetryResult, lastRetryRoute, lastRetryErr
		}
		if err == nil || ctx.Err() != nil || streamCommitted(w) {
			return result, route, err
		}
		if w != nil && isGeoError(err) {
			return result, route, err
		}
		if !shouldRetryProxy(err, route, policy) || attempts+1 >= policy.MaxAttempts {
			return result, route, err
		}
		attempts++
		if s.logger != nil {
			s.logger.Debug("pool: retrying proxy failure with same credential",
				"type", meta.TypeKey, "provider_id", meta.ProviderID,
				"attempt", attempts+1, "max_attempts", policy.MaxAttempts, "proxy", route)
		}
		lastRetryResult, lastRetryRoute, lastRetryErr = result, route, err
		if route != "" {
			excludedRoutes = append(excludedRoutes, route)
		}
	}
}

// runPool tries each credential once in pool order and returns the first
// success with the proxy host:port of the winning or last attempt. Bounded
// route retries happen inside one credential attempt. There are no repeat
// credential passes or backoff pauses. A missing handler fails immediately;
// otherwise the last error is returned.
// limit deprioritizes credentials the exhausted store holds a live
// rate-limit key for (plugin, type, credential, model) to the tail.
func runPool[T any](ctx context.Context, s *Service, model string, creds []*models.Credential, attempt func(context.Context, *models.Credential) (T, string, error), isFatal func(error) bool, limit pool.LimitFunc) (T, string, error) {
	log := s.logger
	if log != nil && model != "" {
		log = log.With("model", model)
	}
	return pool.RunWithProxy(ctx, log, creds, s.usage, attempt, isFatal, limit)
}

// runPoolStream is runPool for streaming calls. Same-credential proxy-limit
// retries stop after the first byte reaches the client. Geo errors do not
// retry with the same credential on streams. limit deprioritizes
// credentials the exhausted store holds a live rate-limit key for.
func (s *Service) runPoolStream(ctx context.Context, model string, w io.Writer, creds []*models.Credential, attempt func(context.Context, *models.Credential, io.Writer) (string, error), isFatal func(error) bool, limit pool.LimitFunc) (string, error) {
	log := s.logger
	if log != nil && model != "" {
		log = log.With("model", model)
	}
	return pool.RunStreamWithProxy(ctx, log, w, creds, s.usage, attempt, isFatal, limit)
}

// withCredential pins one pool credential into a copy of the base meta.
func withCredential(meta HandlerMeta, cred *models.Credential) HandlerMeta {
	meta.Credential = cred
	return meta
}

// CompletePool tries the credential pool in order through the complete
// handler. Proxy failures can retry on untried proxies within the retry
// policy. It returns the winning or last proxy host:port ("" = direct).
func (s *Service) CompletePool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.ChatCompletionRequest,
) (*models.ChatCompletionResponse, string, error) {
	retry := s.retryPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, retry)
	limit := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, req.Model.String())
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, string, error) {
		return runRoutedRetries(ctx, s, withCredential(meta, cred), retry, nil,
			func(ctx context.Context) (*models.ChatCompletionResponse, string, error) {
				return s.CompleteRouted(ctx, withCredential(meta, cred), req)
			})
	}, isFatal, limit)
}

// CompleteStreamPool tries the credential pool in order through the
// complete_stream handler. Route failures can
// retry before the first byte reaches the client. It returns the winning
// or last proxy host:port.
func (s *Service) CompleteStreamPool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.ChatCompletionRequest,
	w io.Writer,
) (string, error) {
	retry := s.retryPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, retry)
	limit := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, req.Model.String())
	return s.runPoolStream(ctx, req.Model.String(), w, creds, func(ctx context.Context, cred *models.Credential, w io.Writer) (string, error) {
		_, route, err := runRoutedRetries(ctx, s, withCredential(meta, cred), retry, w,
			func(ctx context.Context) (struct{}, string, error) {
				route, err := s.CompleteStreamRouted(ctx, withCredential(meta, cred), req, w)
				return struct{}{}, route, err
			})
		return route, err
	}, isFatal, limit)
}

// TranscribePool tries credentials through the transcribe handler. Proxy-
// scoped limits or transport failures can retry on untried proxies within the retry policy.
// It returns the winning or last proxy host:port ("" = direct).
func (s *Service) TranscribePool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.TranscriptionRequest,
) (*models.TranscriptionResponse, string, error) {
	retry := s.retryPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, retry)
	limit := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, req.Model.String())
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.TranscriptionResponse, string, error) {
		return runRoutedRetries(ctx, s, withCredential(meta, cred), retry, nil,
			func(ctx context.Context) (*models.TranscriptionResponse, string, error) {
				return s.TranscribeRouted(ctx, withCredential(meta, cred), req)
			})
	}, isFatal, limit)
}

// SpeechPool tries credentials through the speech handler. Proxy-scoped
// limits or transport failures can retry on untried proxies within the retry policy. It returns
// the winning or last proxy host:port ("" = direct).
func (s *Service) SpeechPool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.SpeechRequest,
) (*models.SpeechResponse, string, error) {
	retry := s.retryPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, retry)
	limit := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, req.Model.String())
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.SpeechResponse, string, error) {
		return runRoutedRetries(ctx, s, withCredential(meta, cred), retry, nil,
			func(ctx context.Context) (*models.SpeechResponse, string, error) {
				return s.SpeechRouted(ctx, withCredential(meta, cred), req)
			})
	}, isFatal, limit)
}

// GenerateImagePool tries credentials through the generate_image handler.
// Route failures can retry on untried proxies within the retry policy.
// It returns the winning or last proxy host:port ("" = direct).
func (s *Service) GenerateImagePool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.ImageGenerationRequest,
) (*models.ImageGenerationResponse, string, error) {
	retry := s.retryPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, retry)
	limit := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, req.Model.String())
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.ImageGenerationResponse, string, error) {
		return runRoutedRetries(ctx, s, withCredential(meta, cred), retry, nil,
			func(ctx context.Context) (*models.ImageGenerationResponse, string, error) {
				return s.GenerateImageRouted(ctx, withCredential(meta, cred), req)
			})
	}, isFatal, limit)
}

// EmbedPool tries credentials through the embed handler. Proxy-scoped limits
// or transport failures can retry on untried proxies within the retry policy. It returns the
// winning or last proxy host:port ("" = direct).
func (s *Service) EmbedPool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.EmbeddingsRequest,
) (*models.EmbeddingsResponse, string, error) {
	retry := s.retryPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, retry)
	limit := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, req.Model.String())
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.EmbeddingsResponse, string, error) {
		return runRoutedRetries(ctx, s, withCredential(meta, cred), retry, nil,
			func(ctx context.Context) (*models.EmbeddingsResponse, string, error) {
				return s.EmbedRouted(ctx, withCredential(meta, cred), req)
			})
	}, isFatal, limit)
}

// ModeratePool tries credentials through the moderate handler. Proxy-scoped
// limits or transport failures can retry on untried proxies within the
// retry policy. It returns the winning or last proxy host:port ("" = direct).
func (s *Service) ModeratePool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.ModerationRequest,
) (*models.ModerationResponse, string, error) {
	retry := s.retryPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, retry)
	limit := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, req.Model.String())
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.ModerationResponse, string, error) {
		return runRoutedRetries(ctx, s, withCredential(meta, cred), retry, nil,
			func(ctx context.Context) (*models.ModerationResponse, string, error) {
				return s.ModerateRouted(ctx, withCredential(meta, cred), req)
			})
	}, isFatal, limit)
}

// SubmitVideoPool tries credentials through the generate_video handler.
// Route failures can retry on untried proxies within the retry policy.
// It returns the winning or last proxy host:port ("" = direct).
func (s *Service) SubmitVideoPool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.VideoGenerationRequest,
) (*models.VideoGenerationResponse, string, error) {
	retry := s.retryPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, retry)
	limit := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, req.Model.String())
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.VideoGenerationResponse, string, error) {
		return runRoutedRetries(ctx, s, withCredential(meta, cred), retry, nil,
			func(ctx context.Context) (*models.VideoGenerationResponse, string, error) {
				return s.SubmitVideoRouted(ctx, withCredential(meta, cred), req)
			})
	}, isFatal, limit)
}

// PollVideoPool tries credentials through the poll_video handler for one
// upstream job ID. It returns the winning or last proxy host:port.
func (s *Service) PollVideoPool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	model models.ModelId,
	upstreamJobID string,
) (*models.VideoGenerationResponse, string, error) {
	retry := s.retryPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, retry)
	limit := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, model.String())
	return runPool(ctx, s, model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.VideoGenerationResponse, string, error) {
		return runRoutedRetries(ctx, s, withCredential(meta, cred), retry, nil,
			func(ctx context.Context) (*models.VideoGenerationResponse, string, error) {
				return s.PollVideoRouted(ctx, withCredential(meta, cred), model, upstreamJobID)
			})
	}, isFatal, limit)
}

// VideoContentPool tries credentials through the video_content handler for
// one upstream job ID and asset index. It returns the winning or last
// proxy host:port.
func (s *Service) VideoContentPool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	model models.ModelId,
	upstreamJobID string,
	index int,
) (*models.VideoContentResponse, string, error) {
	retry := s.retryPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, retry)
	limit := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, model.String())
	return runPool(ctx, s, model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.VideoContentResponse, string, error) {
		return runRoutedRetries(ctx, s, withCredential(meta, cred), retry, nil,
			func(ctx context.Context) (*models.VideoContentResponse, string, error) {
				return s.VideoContentRouted(ctx, withCredential(meta, cred), model, upstreamJobID, index)
			})
	}, isFatal, limit)
}
