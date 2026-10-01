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

const maxRouteAttempts = 3

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

// SetCredentialDisabler wires per-attempt credential auto-disable for
// auth/payment_required outcomes. Unset (nil) disables it.
func (s *Service) SetCredentialDisabler(f func(credentialID, reason string)) {
	s.credDisabler = f
}

// SetProviderDisabler wires per-attempt provider auto-disable for
// structural_fault outcomes. Unset (nil) disables it.
func (s *Service) SetProviderDisabler(f func(providerID, reason string)) {
	s.provDisabler = f
}

// SetProxyPenalizer wires forced backoff penalties for proxies blamed for
// structural network failures. Unset (nil) disables it.
func (s *Service) SetProxyPenalizer(f func(url, reason string) bool) {
	s.penalizeProxy = f
}

// SetDumpDir wires the directory for full upstream-body spill files in
// debug mode. Empty disables spilling.
func (s *Service) SetDumpDir(dir string) { s.dumpDir = dir }

func isFatalPoolError(err error) bool {
	return isFatalWithGeo(err, models.GeoModeFailFast)
}

// isFatalWithGeo reports whether err stops the credential pool immediately.
// invalid_request, content_policy and structural_fault are identical for
// every key and model of the provider, so iterating only repeats the
// failure. Geo stops the pool only in fail_fast mode; in retry_same_key
// mode the pool advances to the next credential after same-key retries
// run out (the banned exit stays excluded through the geo flag).
func isFatalWithGeo(err error, geoMode string) bool {
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
			return geoMode != models.GeoModeRetrySameKey
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
// config-aware fatal decisions with the typed reason and geo mode.
func (s *Service) fatalWithLog(meta HandlerMeta, geo models.GeoConfig) func(error) bool {
	return func(err error) bool {
		fatal := isFatalWithGeo(err, geo.Mode)
		if fatal && s.logger != nil {
			s.logger.Debug("pool: fatal error, stopping pool",
				"type", meta.TypeKey, "provider_id", meta.ProviderID,
				"reason", fatalReason(err), "geo_mode", geo.Mode)
		}
		return fatal
	}
}

// geoPolicy resolves the provider geo reaction for one pool pass.
// Unknown modes fail closed to fail_fast with a warning.
func (s *Service) geoPolicy(providerConfig map[string]any) models.GeoConfig {
	cfg, err := models.ParseGeoConfig(providerConfig)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("pool: invalid geo config, using fail_fast", "error", err)
		}
		return models.GeoConfig{Mode: models.GeoModeFailFast, MaxProxies: models.DefaultGeoMaxProxies}
	}
	return cfg
}

// shouldRetryGeo reports whether a geo failure warrants another attempt
// with the same credential: retry_same_key mode, a proxied route (direct
// geo has no other exit to try), and retries left.
func shouldRetryGeo(err error, route string, geo models.GeoConfig, attempt int) bool {
	if geo.Mode != models.GeoModeRetrySameKey || route == "" {
		return false
	}
	var perr *models.ProviderError
	if !errors.As(err, &perr) || perr.Type != models.ErrorTypeGeo {
		return false
	}
	return attempt+1 < geo.MaxProxies
}

func shouldRetryProxyLimit(err error, route string) bool {
	if route == "" {
		return false
	}
	var perr *models.ProviderError
	if !errors.As(err, &perr) || (perr.Type != models.ErrorTypeRateLimit && perr.Type != models.ErrorTypeQuotaExceeded) {
		return false
	}
	if len(perr.Scope) == 0 {
		return true
	}
	for _, scope := range perr.Scope {
		if scope == models.ExhaustedScopeProxy {
			return true
		}
	}
	return false
}

// shouldRetryTransport reports whether a connectivity failure warrants
// another attempt with the same credential on another route: a proxied
// route (direct transport failures fail over to the next credential, as
// there is no other exit to try) carrying a transport error.
func shouldRetryTransport(err error, route string) bool {
	if route == "" {
		return false
	}
	var perr *models.ProviderError
	if !errors.As(err, &perr) || perr.Type != models.ErrorTypeTransport {
		return false
	}
	return true
}

// shouldRetryOverloaded reports whether a congested-backend failure
// warrants another attempt with the same credential on another route.
// Like transport, it requires a proxied route to exclude; direct
// overloads fail over to the next credential.
func shouldRetryOverloaded(err error, route string) bool {
	if route == "" {
		return false
	}
	var perr *models.ProviderError
	if !errors.As(err, &perr) || perr.Type != models.ErrorTypeOverloaded {
		return false
	}
	return true
}

func streamCommitted(w io.Writer) bool {
	if w == nil {
		return false
	}
	tracker, ok := w.(interface{ Written() bool })
	return !ok || tracker.Written()
}

// runRoutedRetries attempts one credential with bounded same-credential
// route retries: proxy-scoped rate/quota limits, transport failures and
// overloads each retry on an untried route up to maxRouteAttempts total
// attempts, stopping early when no alternate route remains, the context
// ends, or the stream committed its first byte.
func runRoutedRetries[T any](
	ctx context.Context,
	s *Service,
	meta HandlerMeta,
	geo models.GeoConfig,
	w io.Writer,
	attempt func(context.Context) (T, string, error),
) (T, string, error) {
	var lastRetryResult T
	var lastRetryRoute string
	var lastRetryErr error
	var excludedRoutes []string
	routeAttempts := 0
	geoAttempts := 0

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

		retry := false
		if shouldRetryProxyLimit(err, route) || shouldRetryTransport(err, route) || shouldRetryOverloaded(err, route) {
			routeAttempts++
			retry = routeAttempts < maxRouteAttempts
			if retry && s.logger != nil {
				s.logger.Debug("pool: retrying route failure with same credential",
					"type", meta.TypeKey, "provider_id", meta.ProviderID,
					"attempt", routeAttempts+1, "max_attempts", maxRouteAttempts, "proxy", route)
			}
		} else if w == nil && shouldRetryGeo(err, route, geo, geoAttempts) {
			geoAttempts++
			retry = true
			if s.logger != nil {
				s.logger.Debug("pool: geo retry with same credential", "type", meta.TypeKey,
					"provider_id", meta.ProviderID, "attempt", geoAttempts+1,
					"route", route, "max_proxies", geo.MaxProxies)
			}
		}
		if !retry {
			return result, route, err
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
// skip bypasses credentials the exhausted store holds a live rate-limit key
// for (plugin, type, account, model).
func runPool[T any](ctx context.Context, s *Service, model string, creds []*models.Credential, attempt func(context.Context, *models.Credential) (T, string, error), isFatal func(error) bool, skip pool.SkipFunc) (T, string, error) {
	log := s.logger
	if log != nil && model != "" {
		log = log.With("model", model)
	}
	return pool.RunWithProxy(ctx, log, creds, s.usage, attempt, isFatal, skip)
}

// runPoolStream is runPool for streaming calls. Same-credential proxy-limit
// retries stop after the first byte reaches the client. Geo errors do not
// retry with the same credential on streams. skip bypasses credentials the
// exhausted store holds a live rate-limit key for.
func (s *Service) runPoolStream(ctx context.Context, model string, w io.Writer, creds []*models.Credential, attempt func(context.Context, *models.Credential, io.Writer) (string, error), isFatal func(error) bool, skip pool.SkipFunc) (string, error) {
	log := s.logger
	if log != nil && model != "" {
		log = log.With("model", model)
	}
	return pool.RunStreamWithProxy(ctx, log, w, creds, s.usage, attempt, isFatal, skip)
}

// withCredential pins one pool credential into a copy of the base meta.
func withCredential(meta HandlerMeta, cred *models.Credential) HandlerMeta {
	meta.Credential = cred
	return meta
}

// CompletePool tries the credential pool in order through the complete
// handler. Proxy-scoped limits can retry up to three routes with the same
// credential. It returns the winning or last proxy host:port ("" = direct).
func (s *Service) CompletePool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.ChatCompletionRequest,
) (*models.ChatCompletionResponse, string, error) {
	geo := s.geoPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, geo)
	skip := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, req.Model.String())
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.ChatCompletionResponse, string, error) {
		return runRoutedRetries(ctx, s, withCredential(meta, cred), geo, nil,
			func(ctx context.Context) (*models.ChatCompletionResponse, string, error) {
				return s.CompleteRouted(ctx, withCredential(meta, cred), req)
			})
	}, isFatal, skip)
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
	geo := s.geoPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, geo)
	skip := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, req.Model.String())
	return s.runPoolStream(ctx, req.Model.String(), w, creds, func(ctx context.Context, cred *models.Credential, w io.Writer) (string, error) {
		_, route, err := runRoutedRetries(ctx, s, withCredential(meta, cred), geo, w,
			func(ctx context.Context) (struct{}, string, error) {
				route, err := s.CompleteStreamRouted(ctx, withCredential(meta, cred), req, w)
				return struct{}{}, route, err
			})
		return route, err
	}, isFatal, skip)
}

// TranscribePool tries credentials through the transcribe handler. Proxy-
// scoped limits or transport failures can retry up to three routes with the same credential.
// It returns the winning or last proxy host:port ("" = direct).
func (s *Service) TranscribePool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.TranscriptionRequest,
) (*models.TranscriptionResponse, string, error) {
	geo := s.geoPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, geo)
	skip := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, req.Model.String())
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.TranscriptionResponse, string, error) {
		return runRoutedRetries(ctx, s, withCredential(meta, cred), geo, nil,
			func(ctx context.Context) (*models.TranscriptionResponse, string, error) {
				return s.TranscribeRouted(ctx, withCredential(meta, cred), req)
			})
	}, isFatal, skip)
}

// SpeechPool tries credentials through the speech handler. Proxy-scoped
// limits or transport failures can retry up to three routes with the same credential. It returns
// the winning or last proxy host:port ("" = direct).
func (s *Service) SpeechPool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.SpeechRequest,
) (*models.SpeechResponse, string, error) {
	geo := s.geoPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, geo)
	skip := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, req.Model.String())
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.SpeechResponse, string, error) {
		return runRoutedRetries(ctx, s, withCredential(meta, cred), geo, nil,
			func(ctx context.Context) (*models.SpeechResponse, string, error) {
				return s.SpeechRouted(ctx, withCredential(meta, cred), req)
			})
	}, isFatal, skip)
}

// GenerateImagePool tries credentials through the generate_image handler.
// Route failures can retry up to three routes with the same credential.
// It returns the winning or last proxy host:port ("" = direct).
func (s *Service) GenerateImagePool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.ImageGenerationRequest,
) (*models.ImageGenerationResponse, string, error) {
	geo := s.geoPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, geo)
	skip := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, req.Model.String())
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.ImageGenerationResponse, string, error) {
		return runRoutedRetries(ctx, s, withCredential(meta, cred), geo, nil,
			func(ctx context.Context) (*models.ImageGenerationResponse, string, error) {
				return s.GenerateImageRouted(ctx, withCredential(meta, cred), req)
			})
	}, isFatal, skip)
}

// EmbedPool tries credentials through the embed handler. Proxy-scoped limits
// or transport failures can retry up to three routes with the same credential. It returns the
// winning or last proxy host:port ("" = direct).
func (s *Service) EmbedPool(
	ctx context.Context,
	meta HandlerMeta,
	creds []*models.Credential,
	req *models.EmbeddingsRequest,
) (*models.EmbeddingsResponse, string, error) {
	geo := s.geoPolicy(meta.ProviderConfig)
	isFatal := s.fatalWithLog(meta, geo)
	skip := s.exhaustedSkip(meta.ProviderID, meta.TypeKey, req.Model.String())
	return runPool(ctx, s, req.Model.String(), creds, func(ctx context.Context, cred *models.Credential) (*models.EmbeddingsResponse, string, error) {
		return runRoutedRetries(ctx, s, withCredential(meta, cred), geo, nil,
			func(ctx context.Context) (*models.EmbeddingsResponse, string, error) {
				return s.EmbedRouted(ctx, withCredential(meta, cred), req)
			})
	}, isFatal, skip)
}
