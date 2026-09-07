// Package retry implements the single retry/fallthrough engine shared by
// the router (credential rotation) and the agents adapter (model rotation).
package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
)

// Classifiable is implemented by errors that know whether they are retryable.
type Classifiable interface {
	Retryable() bool
}

// Classify reports whether err is retryable via errors.As unwrapping.
func Classify(err error) bool {
	if err == nil {
		return false
	}
	var c Classifiable
	if errors.As(err, &c) {
		return c.Retryable()
	}
	return false
}

// Candidate is a single attempt for a non-streaming call.
type Candidate[T any] struct {
	Label string
	Run   func(ctx context.Context) (T, error)
}

// Run tries each candidate in order. The first success wins. A retryable
// error moves to the next candidate; a terminal error returns immediately.
// When every candidate fails with a retryable error, a single aggregated
// "all candidates exhausted" error is returned.
func Run[T any](ctx context.Context, candidates []Candidate[T], logger *slog.Logger) (T, error) {
	var zero T
	if len(candidates) == 0 {
		return zero, fmt.Errorf("all candidates exhausted: no candidates available")
	}
	var lastErr error
	for i, c := range candidates {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		res, err := c.Run(ctx)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if Classify(err) {
			if logger != nil {
				logger.Info("candidate failed with retryable error, trying next",
					"candidate", c.Label, "index", i, "total", len(candidates), "err", err)
			}
			continue
		}
		return zero, err
	}
	return zero, fmt.Errorf("all candidates exhausted after %d attempts: %w", len(candidates), lastErr)
}

// StreamCandidate is a single attempt for a streaming call.
type StreamCandidate struct {
	Label string
	Run   func(ctx context.Context, w io.Writer) error
}

// RunStream tries each candidate in order, writing SSE to w.
// Streaming candidates must only write to w on success paths where the
// headers are already committed; a retryable failure before any write
// moves to the next candidate.
func RunStream(ctx context.Context, candidates []StreamCandidate, w io.Writer, logger *slog.Logger) error {
	if len(candidates) == 0 {
		return fmt.Errorf("all candidates exhausted: no candidates available")
	}
	var lastErr error
	for i, c := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.Run(ctx, w); err == nil {
			return nil
		} else {
			lastErr = err
			if Classify(err) {
				if logger != nil {
					logger.Info("stream candidate failed with retryable error, trying next",
						"candidate", c.Label, "index", i, "total", len(candidates), "err", err)
				}
				continue
			}
			return err
		}
	}
	return fmt.Errorf("all candidates exhausted after %d attempts: %w", len(candidates), lastErr)
}
