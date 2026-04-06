package retry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSleep replaces the real sleep with a no-op and records the durations.
// It returns a cleanup function and a pointer to the recorded durations.
func stubSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	var recorded []time.Duration
	orig := sleepFunc
	sleepFunc = func(d time.Duration) { recorded = append(recorded, d) }
	t.Cleanup(func() { sleepFunc = orig })
	return &recorded
}

func testPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:       3,
		InitialDelay:      100 * time.Millisecond,
		BackoffMultiplier: 2.0,
		MaxDelay:          1 * time.Second,
	}
}

// ---------------------------------------------------------------------------
// Do / DoWithContext tests
// ---------------------------------------------------------------------------

func TestDo_SuccessOnFirstTry(t *testing.T) {
	sleeps := stubSleep(t)
	p := testPolicy()

	calls := 0
	err := p.Do(func() error {
		calls++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, calls)
	assert.Empty(t, *sleeps, "should not sleep when first attempt succeeds")
}

func TestDo_SuccessOnRetry(t *testing.T) {
	sleeps := stubSleep(t)
	p := testPolicy()

	calls := 0
	err := p.Do(func() error {
		calls++
		if calls < 3 {
			return fmt.Errorf("transient: %w", ErrRetryable)
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 3, calls)
	assert.Len(t, *sleeps, 2, "should sleep between retries")
}

func TestDo_MaxAttemptsExceeded(t *testing.T) {
	stubSleep(t)
	p := testPolicy()

	calls := 0
	err := p.Do(func() error {
		calls++
		return fmt.Errorf("still broken: %w", ErrRetryable)
	})

	require.Error(t, err)
	assert.Equal(t, 3, calls)
	assert.True(t, errors.Is(err, ErrMaxAttemptsExceeded), "should wrap ErrMaxAttemptsExceeded")
	assert.Contains(t, err.Error(), "after 3 attempts")
}

func TestDo_NonRetryableFastFail(t *testing.T) {
	sleeps := stubSleep(t)
	p := testPolicy()

	calls := 0
	permanent := errors.New("permission denied")
	err := p.Do(func() error {
		calls++
		return permanent
	})

	require.Error(t, err)
	assert.Equal(t, 1, calls, "should not retry non-retryable errors")
	assert.Empty(t, *sleeps)
	assert.Equal(t, permanent, err, "should return the original error unwrapped")
}

func TestDo_BackoffDelayGrowth(t *testing.T) {
	sleeps := stubSleep(t)
	p := &RetryPolicy{
		MaxAttempts:       5,
		InitialDelay:      100 * time.Millisecond,
		BackoffMultiplier: 2.0,
		MaxDelay:          1 * time.Second,
	}

	err := p.Do(func() error {
		return fmt.Errorf("timeout connecting")
	})

	require.Error(t, err)
	require.Len(t, *sleeps, 4, "5 attempts → 4 sleeps")

	// Expected progression: 100ms, 200ms, 400ms, 800ms
	expected := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
	}
	assert.Equal(t, expected, *sleeps)
}

func TestDo_BackoffCapsAtMaxDelay(t *testing.T) {
	sleeps := stubSleep(t)
	p := &RetryPolicy{
		MaxAttempts:       6,
		InitialDelay:      100 * time.Millisecond,
		BackoffMultiplier: 4.0,
		MaxDelay:          500 * time.Millisecond,
	}

	_ = p.Do(func() error {
		return fmt.Errorf("503 service unavailable")
	})

	require.Len(t, *sleeps, 5)
	// 100, 400, 500(capped), 500(capped), 500(capped)
	assert.Equal(t, 100*time.Millisecond, (*sleeps)[0])
	assert.Equal(t, 400*time.Millisecond, (*sleeps)[1])
	for i := 2; i < 5; i++ {
		assert.Equal(t, 500*time.Millisecond, (*sleeps)[i], "delay at index %d should be capped", i)
	}
}

func TestDoWithContext_Cancellation(t *testing.T) {
	stubSleep(t)
	p := testPolicy()

	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	err := p.DoWithContext(ctx, func() error {
		calls++
		// Cancel after first attempt so the retry loop notices.
		cancel()
		return fmt.Errorf("timeout")
	})

	require.Error(t, err)
	assert.Equal(t, 1, calls)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestDoWithContext_AlreadyCancelledContext(t *testing.T) {
	stubSleep(t)
	p := testPolicy()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	calls := 0
	err := p.DoWithContext(ctx, func() error {
		calls++
		return fmt.Errorf("rate limit exceeded")
	})

	require.Error(t, err)
	// First call still executes, then cancellation is detected before sleeping.
	assert.Equal(t, 1, calls)
	assert.ErrorIs(t, err, context.Canceled)
}

// ---------------------------------------------------------------------------
// Error classification tests
// ---------------------------------------------------------------------------

func TestIsRetryable_SentinelWrapped(t *testing.T) {
	err := fmt.Errorf("LLM call failed: %w", ErrRetryable)
	assert.True(t, isRetryable(err))
}

func TestIsRetryable_TimeoutString(t *testing.T) {
	assert.True(t, isRetryable(errors.New("connection timeout after 30s")))
}

func TestIsRetryable_RateLimitString(t *testing.T) {
	assert.True(t, isRetryable(errors.New("rate limit exceeded")))
}

func TestIsRetryable_Status503(t *testing.T) {
	assert.True(t, isRetryable(errors.New("HTTP 503 Service Unavailable")))
}

func TestIsRetryable_Status429(t *testing.T) {
	assert.True(t, isRetryable(errors.New("HTTP 429 Too Many Requests")))
}

func TestIsRetryable_PermanentError(t *testing.T) {
	assert.False(t, isRetryable(errors.New("invalid API key")))
	assert.False(t, isRetryable(errors.New("file not found")))
	assert.False(t, isRetryable(errors.New("permission denied")))
}

// ---------------------------------------------------------------------------
// DefaultPolicy / FromConfig tests
// ---------------------------------------------------------------------------

func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy()
	assert.Equal(t, 3, p.MaxAttempts)
	assert.Equal(t, 5*time.Second, p.InitialDelay)
	assert.Equal(t, 2.0, p.BackoffMultiplier)
	assert.Equal(t, 60*time.Second, p.MaxDelay)
}

func TestFromConfig_FullOverride(t *testing.T) {
	cfg := config.RetryConfig{
		MaxAttempts:       5,
		InitialDelayMs:    1000,
		BackoffMultiplier: 3.0,
		MaxDelayMs:        30000,
	}
	p := FromConfig(cfg)

	assert.Equal(t, 5, p.MaxAttempts)
	assert.Equal(t, 1*time.Second, p.InitialDelay)
	assert.Equal(t, 3.0, p.BackoffMultiplier)
	assert.Equal(t, 30*time.Second, p.MaxDelay)
}

func TestFromConfig_ZeroValuesFallBackToDefaults(t *testing.T) {
	cfg := config.RetryConfig{} // all zeros
	p := FromConfig(cfg)
	def := DefaultPolicy()

	assert.Equal(t, def.MaxAttempts, p.MaxAttempts)
	assert.Equal(t, def.InitialDelay, p.InitialDelay)
	assert.Equal(t, def.BackoffMultiplier, p.BackoffMultiplier)
	assert.Equal(t, def.MaxDelay, p.MaxDelay)
}

func TestFromConfig_PartialOverride(t *testing.T) {
	cfg := config.RetryConfig{
		MaxAttempts: 10,
		// leave rest at zero → defaults
	}
	p := FromConfig(cfg)
	def := DefaultPolicy()

	assert.Equal(t, 10, p.MaxAttempts)
	assert.Equal(t, def.InitialDelay, p.InitialDelay)
	assert.Equal(t, def.BackoffMultiplier, p.BackoffMultiplier)
	assert.Equal(t, def.MaxDelay, p.MaxDelay)
}
