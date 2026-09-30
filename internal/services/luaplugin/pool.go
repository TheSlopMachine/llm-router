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

// runPool tries one attempt per credential in pool order and returns the
// first success with the redacted proxy host:port of the winning or last
// attempt ("" = direct). Every key is tried at most once; there are no
// repeat passes or backoff pauses. A missing handler fails immediately: it
// is identical for every key. Otherwise the last error is returned.
// skip bypasses credentials the exhausted store holds a live rate-limit key
// for (plugin, type, account, model).
func runPool[T any](ctx context.Context, s *Service, model string, creds []*models.Credential, attempt func(context.Context, *models.Credential) (T, string, error), isFatal func(error) bool, skip pool.SkipFunc) (T, string, error) {
	log := s.logger
	if log != nil && model != "" {
		log = log.With("model", model)
	}
	return pool.RunWithProxy(ctx, log, creds, s.usage, attempt, isFatal, skip)
}

// runPoolStream is runPool for streaming calls. Failover is allowed only
// before the first byte reaches the client. Same-key geo retries do not
// apply to streams: a geo outcome fails over to the next credential while
// pre-first-byte, exactly like any other non-fatal error. skip bypasses
// credentials the exhausted store holds a live rate-limit key for.
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
// handler, at most once per credential. It returns the redacted proxy
// host:port of the winning or last attempt ("" = direct).
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
		var res *models.ChatCompletionResponse
		var route string
		var err error
		for i := 0; ; i++ {
			res, route, err = s.CompleteRouted(ctx, withCredential(meta, cred), req)
			if err == nil || !shouldRetryGeo(err, route, geo, i) {
				return res, route, err
			}
			if ctx.Err() != nil {
				return res, route, err
			}
			if s.logger != nil {
				s.logger.Debug("pool: geo retry with same credential", "type", meta.TypeKey, "provider_id", meta.ProviderID, "attempt", i+1, "route", route, "max_proxies", geo.MaxProxies)
			}
		}
	}, isFatal, skip)
}

// CompleteStreamPool tries the credential pool in order through the
// complete_stream handler, at most once per credential. It returns the
// redacted proxy host:port of the winning or last attempt ("" = direct).
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
		return s.CompleteStreamRouted(ctx, withCredential(meta, cred), req, w)
	}, isFatal, skip)
}

// TranscribePool tries the credential pool in order through the transcribe
// handler, at most once per credential. It returns the redacted proxy
// host:port of the winning or last attempt ("" = direct).
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
		var res *models.TranscriptionResponse
		var route string
		var err error
		for i := 0; ; i++ {
			res, route, err = s.TranscribeRouted(ctx, withCredential(meta, cred), req)
			if err == nil || !shouldRetryGeo(err, route, geo, i) {
				return res, route, err
			}
			if ctx.Err() != nil {
				return res, route, err
			}
			if s.logger != nil {
				s.logger.Debug("pool: geo retry with same credential", "type", meta.TypeKey, "provider_id", meta.ProviderID, "attempt", i+1, "route", route, "max_proxies", geo.MaxProxies)
			}
		}
	}, isFatal, skip)
}

// SpeechPool tries the credential pool in order through the speech handler,
// at most once per credential. It returns the redacted proxy host:port of
// the winning or last attempt ("" = direct).
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
		var res *models.SpeechResponse
		var route string
		var err error
		for i := 0; ; i++ {
			res, route, err = s.SpeechRouted(ctx, withCredential(meta, cred), req)
			if err == nil || !shouldRetryGeo(err, route, geo, i) {
				return res, route, err
			}
			if ctx.Err() != nil {
				return res, route, err
			}
			if s.logger != nil {
				s.logger.Debug("pool: geo retry with same credential", "type", meta.TypeKey, "provider_id", meta.ProviderID, "attempt", i+1, "route", route, "max_proxies", geo.MaxProxies)
			}
		}
	}, isFatal, skip)
}

// GenerateImagePool tries the credential pool in order through the
// generate_image handler, at most once per credential. It returns the
// redacted proxy host:port of the winning or last attempt ("" = direct).
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
		var res *models.ImageGenerationResponse
		var route string
		var err error
		for i := 0; ; i++ {
			res, route, err = s.GenerateImageRouted(ctx, withCredential(meta, cred), req)
			if err == nil || !shouldRetryGeo(err, route, geo, i) {
				return res, route, err
			}
			if ctx.Err() != nil {
				return res, route, err
			}
			if s.logger != nil {
				s.logger.Debug("pool: geo retry with same credential", "type", meta.TypeKey, "provider_id", meta.ProviderID, "attempt", i+1, "route", route, "max_proxies", geo.MaxProxies)
			}
		}
	}, isFatal, skip)
}

// EmbedPool tries the credential pool in order through the embed handler, at
// most once per credential. It returns the redacted proxy host:port of the
// winning or last attempt ("" = direct).
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
		var res *models.EmbeddingsResponse
		var route string
		var err error
		for i := 0; ; i++ {
			res, route, err = s.EmbedRouted(ctx, withCredential(meta, cred), req)
			if err == nil || !shouldRetryGeo(err, route, geo, i) {
				return res, route, err
			}
			if ctx.Err() != nil {
				return res, route, err
			}
			if s.logger != nil {
				s.logger.Debug("pool: geo retry with same credential", "type", meta.TypeKey, "provider_id", meta.ProviderID, "attempt", i+1, "route", route, "max_proxies", geo.MaxProxies)
			}
		}
	}, isFatal, skip)
}
