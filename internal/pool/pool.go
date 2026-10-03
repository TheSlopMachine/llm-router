// Package pool runs the single-pass credential failover shared by every
// backend: each key is tried at most once in pool order, the first success
// wins, otherwise the last error is returned. Streaming attempts stop
// failover once the first byte reaches the client.
package pool

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/streamgate"
)

// UsageTracker records per-credential outcomes. Implemented by the credential
// service; backends receive it by injection. Limit state lives in the
// exhausted store, never here: failures only feed usage statistics.
type UsageTracker interface {
	UpdateUsage(id string, success bool) error
}

// NoCredentials is returned for an empty pool.
var NoCredentials = errors.New("no credentials available")

func loggerOrDefault(log *slog.Logger) *slog.Logger {
	if log != nil {
		return log
	}
	return slog.Default()
}

func trackSuccess(log *slog.Logger, tracker UsageTracker, cred *models.Credential) {
	if tracker == nil || cred == nil {
		return
	}
	if err := tracker.UpdateUsage(cred.ID, true); err != nil {
		loggerOrDefault(log).Warn("pool: usage update failed", "credential_id", cred.ID, "error", err)
	}
}

func credentialID(cred *models.Credential) string {
	if cred == nil {
		return ""
	}
	return cred.ID
}

// shortError renders an error for logs: a typed provider error becomes its
// category name, everything else keeps the full message.
func shortError(err error) string {
	var perr *models.ProviderError
	if errors.As(err, &perr) {
		switch perr.Type {
		case models.ErrorTypeRateLimit:
			return "rate_limit"
		case models.ErrorTypeQuotaExceeded:
			return "quota_exceeded"
		case models.ErrorTypeAuth:
			return "auth"
		case models.ErrorTypePaymentRequired:
			return "payment_required"
		case models.ErrorTypeGeo:
			return "geo"
		case models.ErrorTypeInvalidRequest:
			return "invalid_request"
		case models.ErrorTypeContentPolicy:
			return "content_policy"
		case models.ErrorTypeModelUnavailable:
			return "model_unavailable"
		case models.ErrorTypeOverloaded:
			return "overloaded"
		case models.ErrorTypeStructuralFault:
			return "structural_fault"
		case models.ErrorTypeNotFound:
			return "not_found"
		case models.ErrorTypeTransport:
			return "transport"
		default:
			return "upstream"
		}
	}
	return err.Error()
}

func logTryingNext(log *slog.Logger, cred *models.Credential, proxy string, err error) {
	args := make([]any, 0, 6)
	if id := credentialID(cred); id != "" {
		args = append(args, "credential_id", id)
	}
	if proxy != "" {
		args = append(args, "proxy", proxy)
	}
	args = append(args, "error", shortError(err))
	loggerOrDefault(log).Info("credential failed, trying next", args...)
}

func logAllFailed(log *slog.Logger, proxy string, lastErr error) {
	args := make([]any, 0, 4)
	if proxy != "" {
		args = append(args, "proxy", proxy)
	}
	args = append(args, "last_error", shortError(lastErr))
	loggerOrDefault(log).Warn("all credentials failed", args...)
}

func trackFailure(log *slog.Logger, tracker UsageTracker, cred *models.Credential, err error) {
	if tracker == nil || cred == nil {
		return
	}
	if uerr := tracker.UpdateUsage(cred.ID, false); uerr != nil {
		loggerOrDefault(log).Warn("pool: usage update failed", "credential_id", cred.ID, "error", uerr)
	}
}

// LimitFunc reports whether a credential carries a live limit mark (e.g.
// the exhausted store holds a rate-limit key for the credential-model
// pair) plus the mark reset time. Limited credentials are deprioritized to
// the tail of the pool ordered by earliest reset first, never removed.
type LimitFunc func(cred *models.Credential) (time.Time, bool)

// orderPool deprioritizes limited credentials: unlimited first in pool
// order, limited after ordered by earliest reset first.
func orderPool(log *slog.Logger, creds []*models.Credential, limit LimitFunc) []*models.Credential {
	if limit == nil {
		return creds
	}
	type deferred struct {
		cred     *models.Credential
		resetsAt time.Time
	}
	clean := make([]*models.Credential, 0, len(creds))
	var held []deferred
	for _, cred := range creds {
		resetsAt, limited := limit(cred)
		if !limited {
			clean = append(clean, cred)
			continue
		}
		loggerOrDefault(log).Info("credential deprioritized due to rate limit",
			"credential_id", credentialID(cred), "resets_at", resetsAt)
		held = append(held, deferred{cred: cred, resetsAt: resetsAt})
	}
	if len(held) == 0 {
		return clean
	}
	sort.SliceStable(held, func(i, j int) bool {
		return held[i].resetsAt.Before(held[j].resetsAt)
	})
	pending := make([]*models.Credential, 0, len(creds))
	pending = append(pending, clean...)
	for _, h := range held {
		pending = append(pending, h.cred)
	}
	return pending
}

// Run tries one attempt per credential in pool order and returns the first
// success. Fatal attempt errors (isFatal) stop the pool immediately: they
// are identical for every key.
func Run[T any](ctx context.Context, log *slog.Logger, creds []*models.Credential, tracker UsageTracker, attempt func(context.Context, *models.Credential) (T, error), isFatal func(error) bool, limit LimitFunc) (T, error) {
	res, _, err := RunWithProxy(ctx, log, creds, tracker, func(ctx context.Context, cred *models.Credential) (T, string, error) {
		res, err := attempt(ctx, cred)
		return res, "", err
	}, isFatal, limit)
	return res, err
}

// RunWithProxy is Run plus the redacted proxy host:port of the last attempt
// ("" = direct or no proxy tracking). The attempt reports the proxy used by
// that credential try; per-attempt values appear on trying-next lines, the
// last one on the all-failed line. Limited credentials move to the tail
// ordered by earliest reset first and serve as last resort.
func RunWithProxy[T any](ctx context.Context, log *slog.Logger, creds []*models.Credential, tracker UsageTracker, attempt func(context.Context, *models.Credential) (T, string, error), isFatal func(error) bool, limit LimitFunc) (T, string, error) {
	var zero T
	if len(creds) == 0 {
		return zero, "", NoCredentials
	}
	pending := orderPool(log, creds, limit)
	var lastErr error
	lastProxy := ""
	for i, cred := range pending {
		if err := ctx.Err(); err != nil {
			return zero, lastProxy, err
		}
		res, proxy, err := attempt(ctx, cred)
		if err == nil {
			trackSuccess(log, tracker, cred)
			loggerOrDefault(log).Debug("model responded successfully",
				"credential_id", credentialID(cred))
			return res, proxy, nil
		}
		trackFailure(log, tracker, cred, err)
		if isFatal != nil && isFatal(err) {
			return zero, proxy, err
		}
		lastErr = err
		lastProxy = proxy
		if i < len(pending)-1 {
			logTryingNext(log, cred, proxy, err)
		}
	}
	if lastErr != nil {
		logAllFailed(log, lastProxy, lastErr)
	}
	return zero, lastProxy, lastErr
}

// RunStream is Run for streaming calls. Failover continues only while no
// byte reached the client; afterwards the stream belongs to one upstream
// and ends with its error.
func RunStream(ctx context.Context, log *slog.Logger, w io.Writer, creds []*models.Credential, tracker UsageTracker, attempt func(context.Context, *models.Credential, io.Writer) error, isFatal func(error) bool, limit LimitFunc) error {
	_, err := RunStreamWithProxy(ctx, log, w, creds, tracker, func(ctx context.Context, cred *models.Credential, w io.Writer) (string, error) {
		return "", attempt(ctx, cred, w)
	}, isFatal, limit)
	return err
}

// RunStreamWithProxy is RunStream plus the redacted proxy host:port of the
// last attempt ("" = direct or no proxy tracking). Limited credentials move
// to the tail ordered by earliest reset first and serve as last resort.
func RunStreamWithProxy(ctx context.Context, log *slog.Logger, w io.Writer, creds []*models.Credential, tracker UsageTracker, attempt func(context.Context, *models.Credential, io.Writer) (string, error), isFatal func(error) bool, limit LimitFunc) (string, error) {
	if len(creds) == 0 {
		return "", NoCredentials
	}
	pending := orderPool(log, creds, limit)
	gate := streamgate.New(w)
	var lastErr error
	lastProxy := ""
	for i, cred := range pending {
		if err := ctx.Err(); err != nil {
			return lastProxy, err
		}
		proxy, err := attempt(ctx, cred, gate)
		if err == nil {
			trackSuccess(log, tracker, cred)
			loggerOrDefault(log).Debug("model responded successfully",
				"credential_id", credentialID(cred))
			return proxy, nil
		}
		trackFailure(log, tracker, cred, err)
		if isFatal != nil && isFatal(err) {
			return proxy, err
		}
		lastErr = err
		lastProxy = proxy
		if gate.Written() {
			return lastProxy, lastErr
		}
		if i < len(pending)-1 {
			logTryingNext(log, cred, proxy, err)
		}
	}
	if lastErr != nil {
		logAllFailed(log, lastProxy, lastErr)
	}
	return lastProxy, lastErr
}
