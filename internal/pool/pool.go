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

func logTryingNext(log *slog.Logger, cred *models.Credential, proxy string, err error) {
	args := make([]any, 0, 6)
	if id := credentialID(cred); id != "" {
		args = append(args, "credential_id", id)
	}
	if proxy != "" {
		args = append(args, "proxy", proxy)
	}
	args = append(args, "error", err)
	loggerOrDefault(log).Info("credential failed, trying next", args...)
}

func logAllFailed(log *slog.Logger, proxy string, lastErr error) {
	args := make([]any, 0, 4)
	if proxy != "" {
		args = append(args, "proxy", proxy)
	}
	args = append(args, "last_error", lastErr)
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

// Run tries one attempt per credential in pool order and returns the first
// success. Fatal attempt errors (isFatal) stop the pool immediately: they
// are identical for every key.
func Run[T any](ctx context.Context, log *slog.Logger, creds []*models.Credential, tracker UsageTracker, attempt func(context.Context, *models.Credential) (T, error), isFatal func(error) bool) (T, error) {
	res, _, err := RunWithProxy(ctx, log, creds, tracker, func(ctx context.Context, cred *models.Credential) (T, string, error) {
		res, err := attempt(ctx, cred)
		return res, "", err
	}, isFatal)
	return res, err
}

// RunWithProxy is Run plus the redacted proxy host:port of the last attempt
// ("" = direct or no proxy tracking). The attempt reports the proxy used by
// that credential try; per-attempt values appear on trying-next lines, the
// last one on the all-failed line.
func RunWithProxy[T any](ctx context.Context, log *slog.Logger, creds []*models.Credential, tracker UsageTracker, attempt func(context.Context, *models.Credential) (T, string, error), isFatal func(error) bool) (T, string, error) {
	var zero T
	if len(creds) == 0 {
		return zero, "", NoCredentials
	}
	var lastErr error
	lastProxy := ""
	for i, cred := range creds {
		if err := ctx.Err(); err != nil {
			return zero, lastProxy, err
		}
		res, proxy, err := attempt(ctx, cred)
		if err == nil {
			trackSuccess(log, tracker, cred)
			return res, proxy, nil
		}
		trackFailure(log, tracker, cred, err)
		if isFatal != nil && isFatal(err) {
			return zero, proxy, err
		}
		lastErr = err
		lastProxy = proxy
		if i < len(creds)-1 {
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
func RunStream(ctx context.Context, log *slog.Logger, w io.Writer, creds []*models.Credential, tracker UsageTracker, attempt func(context.Context, *models.Credential, io.Writer) error, isFatal func(error) bool) error {
	_, err := RunStreamWithProxy(ctx, log, w, creds, tracker, func(ctx context.Context, cred *models.Credential, w io.Writer) (string, error) {
		return "", attempt(ctx, cred, w)
	}, isFatal)
	return err
}

// RunStreamWithProxy is RunStream plus the redacted proxy host:port of the
// last attempt ("" = direct or no proxy tracking).
func RunStreamWithProxy(ctx context.Context, log *slog.Logger, w io.Writer, creds []*models.Credential, tracker UsageTracker, attempt func(context.Context, *models.Credential, io.Writer) (string, error), isFatal func(error) bool) (string, error) {
	if len(creds) == 0 {
		return "", NoCredentials
	}
	gate := streamgate.New(w)
	var lastErr error
	lastProxy := ""
	for i, cred := range creds {
		if err := ctx.Err(); err != nil {
			return lastProxy, err
		}
		proxy, err := attempt(ctx, cred, gate)
		if err == nil {
			trackSuccess(log, tracker, cred)
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
		if i < len(creds)-1 {
			logTryingNext(log, cred, proxy, err)
		}
	}
	if lastErr != nil {
		logAllFailed(log, lastProxy, lastErr)
	}
	return lastProxy, lastErr
}
