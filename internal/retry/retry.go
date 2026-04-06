// Package retry provides exponential backoff retry policies for transient failures.
package retry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/internal/config"
)

// ErrRetryable is a sentinel error. Wrap transient errors with this to signal
// that the operation should be retried:
//
//	return fmt.Errorf("provider unavailable: %w", retry.ErrRetryable)
var ErrRetryable = errors.New("retryable")

// ErrMaxAttemptsExceeded is returned when all retry attempts have been exhausted.
var ErrMaxAttemptsExceeded = errors.New("max retry attempts exceeded")

// sleepFunc is the function used to sleep between retries.
// It is a package-level variable so tests can override it.
var sleepFunc = time.Sleep

// RetryPolicy governs exponential backoff behaviour.
type RetryPolicy struct {
	MaxAttempts       int
	InitialDelay      time.Duration
	BackoffMultiplier float64
	MaxDelay          time.Duration
}

// DefaultPolicy returns a production-ready policy:
// 3 attempts, 5 s initial delay, 2x backoff, 60 s cap.
func DefaultPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:       3,
		InitialDelay:      5 * time.Second,
		BackoffMultiplier: 2.0,
		MaxDelay:          60 * time.Second,
	}
}

// FromConfig builds a RetryPolicy from the YAML-driven RetryConfig.
// Zero-value fields fall back to DefaultPolicy values.
func FromConfig(cfg config.RetryConfig) *RetryPolicy {
	p := DefaultPolicy()
	if cfg.MaxAttempts > 0 {
		p.MaxAttempts = cfg.MaxAttempts
	}
	if cfg.InitialDelayMs > 0 {
		p.InitialDelay = time.Duration(cfg.InitialDelayMs) * time.Millisecond
	}
	if cfg.BackoffMultiplier > 0 {
		p.BackoffMultiplier = cfg.BackoffMultiplier
	}
	if cfg.MaxDelayMs > 0 {
		p.MaxDelay = time.Duration(cfg.MaxDelayMs) * time.Millisecond
	}
	return p
}

// Do executes fn with exponential backoff. It returns nil on success, or a
// wrapped error after MaxAttempts failures. Non-retryable errors are returned
// immediately without further attempts.
func (p *RetryPolicy) Do(fn func() error) error {
	return p.DoWithContext(context.Background(), fn)
}

// DoWithContext is like Do but respects context cancellation between retries.
func (p *RetryPolicy) DoWithContext(ctx context.Context, fn func() error) error {
	delay := p.InitialDelay

	var lastErr error
	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if !isRetryable(lastErr) {
			return lastErr
		}

		// Don't sleep after the last attempt.
		if attempt == p.MaxAttempts {
			break
		}

		// Respect context before sleeping.
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry aborted: %w", ctx.Err())
		default:
		}

		sleepFunc(delay)

		// Grow delay for next iteration.
		delay = time.Duration(float64(delay) * p.BackoffMultiplier)
		if delay > p.MaxDelay {
			delay = p.MaxDelay
		}
	}

	return fmt.Errorf("%w: after %d attempts: %v", ErrMaxAttemptsExceeded, p.MaxAttempts, lastErr)
}

// isRetryable classifies an error as retryable or not.
// An error is retryable if:
//   - it wraps ErrRetryable, OR
//   - its message contains "timeout", "rate limit", "503", or "429"
func isRetryable(err error) bool {
	if errors.Is(err, ErrRetryable) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, pattern := range []string{"timeout", "rate limit", "503", "429"} {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}
