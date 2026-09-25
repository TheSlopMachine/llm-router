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
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/streamgate"
)

// UsageTracker records per-credential outcomes. Implemented by the credential
// service; backends receive it by injection.
type UsageTracker interface {
	UpdateUsage(id string, success bool) error
	MarkQuotaExceeded(id string, resetAt time.Time) error
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

func logTryingNext(log *slog.Logger, cred *models.Credential, err error) {
	args := make([]any, 0, 4)
	if id := credentialID(cred); id != "" {
		args = append(args, "credential_id", id)
	}
	args = append(args, "error", err)
	loggerOrDefault(log).Info("credential failed, trying next", args...)
}

func logAllFailed(log *slog.Logger, lastErr error) {
	loggerOrDefault(log).Warn("all credentials failed", "last_error", lastErr)
}

func trackFailure(log *slog.Logger, tracker UsageTracker, cred *models.Credential, err error) {
	if tracker == nil || cred == nil {
		return
	}
	if uerr := tracker.UpdateUsage(cred.ID, false); uerr != nil {
		loggerOrDefault(log).Warn("pool: usage update failed", "credential_id", cred.ID, "error", uerr)
	}
	var perr *models.ProviderError
	if errors.As(err, &perr) && perr.Type == models.ErrorTypeQuotaExceeded && perr.RetryAfter != nil {
		if qerr := tracker.MarkQuotaExceeded(cred.ID, *perr.RetryAfter); qerr != nil {
			loggerOrDefault(log).Warn("pool: quota mark failed", "credential_id", cred.ID, "error", qerr)
		}
	}
}

// Run tries one attempt per credential in pool order and returns the first
// success. Fatal attempt errors (isFatal) stop the pool immediately: they
// are identical for every key.
func Run[T any](ctx context.Context, log *slog.Logger, creds []*models.Credential, tracker UsageTracker, attempt func(context.Context, *models.Credential) (T, error), isFatal func(error) bool) (T, error) {
	var zero T
	if len(creds) == 0 {
		return zero, NoCredentials
	}
	var lastErr error
	for i, cred := range creds {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		res, err := attempt(ctx, cred)
		if err == nil {
			trackSuccess(log, tracker, cred)
			return res, nil
		}
		trackFailure(log, tracker, cred, err)
		if isFatal != nil && isFatal(err) {
			return zero, err
		}
		lastErr = err
		if i < len(creds)-1 {
			logTryingNext(log, cred, err)
		}
	}
	if lastErr != nil {
		logAllFailed(log, lastErr)
	}
	return zero, lastErr
}

// RunStream is Run for streaming calls. Failover continues only while no
// byte reached the client; afterwards the stream belongs to one upstream
// and ends with its error.
func RunStream(ctx context.Context, log *slog.Logger, w io.Writer, creds []*models.Credential, tracker UsageTracker, attempt func(context.Context, *models.Credential, io.Writer) error, isFatal func(error) bool) error {
	if len(creds) == 0 {
		return NoCredentials
	}
	gate := streamgate.New(w)
	var lastErr error
	for i, cred := range creds {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := attempt(ctx, cred, gate)
		if err == nil {
			trackSuccess(log, tracker, cred)
			return nil
		}
		trackFailure(log, tracker, cred, err)
		if isFatal != nil && isFatal(err) {
			return err
		}
		lastErr = err
		if gate.Written() {
			return lastErr
		}
		if i < len(creds)-1 {
			logTryingNext(log, cred, err)
		}
	}
	if lastErr != nil {
		logAllFailed(log, lastErr)
	}
	return lastErr
}
