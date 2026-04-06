package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/internal/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock provider
// ---------------------------------------------------------------------------

type mockProvider struct {
	name        string
	available  bool
	cost       float64
	response   string
	tokens     int
	err        error
	callCount  int
	lastPrompt string
}

func (m *mockProvider) Name() string          { return m.name }
func (m *mockProvider) IsAvailable() bool     { return m.available }
func (m *mockProvider) HealthCheck() error     { return nil }
func (m *mockProvider) CostPerToken() float64 { return m.cost }

func (m *mockProvider) Complete(_ context.Context, prompt string) (*CompletionResult, error) {
	m.callCount++
	m.lastPrompt = prompt
	if m.err != nil {
		return nil, m.err
	}
	return &CompletionResult{
		Provider:   m.name,
		Response:   m.response,
		TokensUsed: m.tokens,
		CostUSD:    float64(m.tokens) * m.cost,
		LatencyMs:  1,
	}, nil
}

// noRetryPolicy returns a policy that does not retry (1 attempt, no delay).
func noRetryPolicy() *retry.RetryPolicy {
	return &retry.RetryPolicy{
		MaxAttempts:       1,
		InitialDelay:      time.Millisecond,
		BackoffMultiplier: 1.0,
		MaxDelay:          time.Millisecond,
	}
}

// ---------------------------------------------------------------------------
// Cascade: provider ordering
// ---------------------------------------------------------------------------

func TestCascade_SortsCheapestFirst(t *testing.T) {
	expensive := &mockProvider{name: "expensive", available: true, cost: 0.01, response: "exp"}
	cheap := &mockProvider{name: "cheap", available: true, cost: 0.001, response: "chp"}

	cascade := NewCascade([]Provider{expensive, cheap}, noRetryPolicy())

	result, err := cascade.Complete(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, "cheap", result.Provider, "should pick cheapest available provider")
	assert.Equal(t, 1, cheap.callCount)
	assert.Equal(t, 0, expensive.callCount)
}

// ---------------------------------------------------------------------------
// Cascade: fallthrough on error
// ---------------------------------------------------------------------------

func TestCascade_FallsThroughOnError(t *testing.T) {
	failing := &mockProvider{
		name:      "failing",
		available: true,
		cost:      0.0,
		err:       errors.New("connection refused"),
	}
	backup := &mockProvider{
		name:      "backup",
		available: true,
		cost:      0.01,
		response:  "backup response",
		tokens:    10,
	}

	cascade := NewCascade([]Provider{failing, backup}, noRetryPolicy())

	result, err := cascade.Complete(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "backup", result.Provider)
	assert.Equal(t, "backup response", result.Response)
	assert.Equal(t, 1, failing.callCount)
	assert.Equal(t, 1, backup.callCount)
}

// ---------------------------------------------------------------------------
// Cascade: skips unavailable
// ---------------------------------------------------------------------------

func TestCascade_SkipsUnavailable(t *testing.T) {
	down := &mockProvider{name: "down", available: false, cost: 0.0}
	up := &mockProvider{name: "up", available: true, cost: 0.01, response: "ok", tokens: 5}

	cascade := NewCascade([]Provider{down, up}, noRetryPolicy())

	result, err := cascade.Complete(context.Background(), "prompt")
	require.NoError(t, err)
	assert.Equal(t, "up", result.Provider)
	assert.Equal(t, 0, down.callCount, "unavailable provider should not be called")
}

// ---------------------------------------------------------------------------
// Cascade: all fail
// ---------------------------------------------------------------------------

func TestCascade_AllFail_ReturnsErrNoProvider(t *testing.T) {
	p1 := &mockProvider{name: "p1", available: true, cost: 0.0, err: errors.New("fail1")}
	p2 := &mockProvider{name: "p2", available: true, cost: 0.01, err: errors.New("fail2")}

	cascade := NewCascade([]Provider{p1, p2}, noRetryPolicy())

	_, err := cascade.Complete(context.Background(), "prompt")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoProvider))
}

// ---------------------------------------------------------------------------
// Cascade: no providers available
// ---------------------------------------------------------------------------

func TestCascade_NoAvailableProviders(t *testing.T) {
	p := &mockProvider{name: "p", available: false}
	cascade := NewCascade([]Provider{p}, noRetryPolicy())

	_, err := cascade.Complete(context.Background(), "prompt")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoProvider))
}

// ---------------------------------------------------------------------------
// Cascade: empty provider list
// ---------------------------------------------------------------------------

func TestCascade_EmptyProviders(t *testing.T) {
	cascade := NewCascade(nil, noRetryPolicy())

	_, err := cascade.Complete(context.Background(), "prompt")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoProvider))
}

// ---------------------------------------------------------------------------
// OllamaProvider
// ---------------------------------------------------------------------------

func TestOllamaProvider_Name(t *testing.T) {
	p := NewOllamaProvider("http://localhost:11434", "llama3", nil)
	assert.Equal(t, "ollama", p.Name())
}

func TestOllamaProvider_IsAvailable(t *testing.T) {
	assert.True(t, NewOllamaProvider("http://localhost", "m", nil).IsAvailable())
	assert.False(t, NewOllamaProvider("", "m", nil).IsAvailable())
}

func TestOllamaProvider_CostPerToken(t *testing.T) {
	p := NewOllamaProvider("http://localhost", "m", nil)
	assert.Equal(t, 0.0, p.CostPerToken())
}

func TestOllamaProvider_Complete(t *testing.T) {
	mock := func(_ context.Context, url, body string) (string, int, error) {
		assert.Contains(t, url, "/api/generate")
		return "hello world", 42, nil
	}

	p := NewOllamaProvider("http://localhost:11434", "llama3", mock)
	result, err := p.Complete(context.Background(), "test prompt")
	require.NoError(t, err)
	assert.Equal(t, "ollama", result.Provider)
	assert.Equal(t, "hello world", result.Response)
	assert.Equal(t, 42, result.TokensUsed)
	assert.Equal(t, 0.0, result.CostUSD)
}

func TestOllamaProvider_Complete_Error(t *testing.T) {
	mock := func(_ context.Context, _, _ string) (string, int, error) {
		return "", 0, fmt.Errorf("connection refused")
	}

	p := NewOllamaProvider("http://localhost:11434", "llama3", mock)
	_, err := p.Complete(context.Background(), "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ollama")
}

func TestOllamaProvider_Complete_NilHTTPPost(t *testing.T) {
	p := NewOllamaProvider("http://localhost", "m", nil)
	_, err := p.Complete(context.Background(), "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "httpPost not configured")
}

// ---------------------------------------------------------------------------
// OpenRouterProvider
// ---------------------------------------------------------------------------

func TestOpenRouterProvider_Name(t *testing.T) {
	p := NewOpenRouterProvider("http://api", "gpt-4", "key", 0.001, nil)
	assert.Equal(t, "openrouter", p.Name())
}

func TestOpenRouterProvider_IsAvailable(t *testing.T) {
	assert.True(t, NewOpenRouterProvider("http://api", "m", "key", 0, nil).IsAvailable())
	assert.False(t, NewOpenRouterProvider("", "m", "key", 0, nil).IsAvailable())
	assert.False(t, NewOpenRouterProvider("http://api", "m", "", 0, nil).IsAvailable())
}

func TestOpenRouterProvider_CostPerToken(t *testing.T) {
	p := NewOpenRouterProvider("http://api", "m", "k", 0.005, nil)
	assert.Equal(t, 0.005, p.CostPerToken())
}

func TestOpenRouterProvider_Complete(t *testing.T) {
	mock := func(_ context.Context, url, body string, headers map[string]string) (string, int, error) {
		assert.Contains(t, url, "/v1/chat/completions")
		assert.Equal(t, "Bearer sk-test", headers["Authorization"])
		return "generated text", 100, nil
	}

	p := NewOpenRouterProvider("http://api", "gpt-4", "sk-test", 0.001, mock)
	result, err := p.Complete(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "openrouter", result.Provider)
	assert.Equal(t, "generated text", result.Response)
	assert.Equal(t, 100, result.TokensUsed)
	assert.InDelta(t, 0.1, result.CostUSD, 0.0001)
}

func TestOpenRouterProvider_Complete_Error(t *testing.T) {
	mock := func(_ context.Context, _, _ string, _ map[string]string) (string, int, error) {
		return "", 0, fmt.Errorf("401 unauthorized")
	}

	p := NewOpenRouterProvider("http://api", "gpt-4", "bad-key", 0.001, mock)
	_, err := p.Complete(context.Background(), "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "openrouter")
}

func TestOpenRouterProvider_Complete_NilHTTPPost(t *testing.T) {
	p := NewOpenRouterProvider("http://api", "m", "k", 0, nil)
	_, err := p.Complete(context.Background(), "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "httpPost not configured")
}

// ---------------------------------------------------------------------------
// InheritProvider
// ---------------------------------------------------------------------------

func TestInheritProvider_Name(t *testing.T) {
	assert.Equal(t, "inherit", (&InheritProvider{}).Name())
}

func TestInheritProvider_IsAvailable(t *testing.T) {
	assert.True(t, (&InheritProvider{}).IsAvailable())
}

func TestInheritProvider_Complete_ReturnsError(t *testing.T) {
	_, err := (&InheritProvider{}).Complete(context.Background(), "test")
	assert.True(t, errors.Is(err, ErrNoInheritProvider))
}

func TestInheritProvider_CostPerToken(t *testing.T) {
	assert.Equal(t, 0.0, (&InheritProvider{}).CostPerToken())
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestOllamaProvider_ImplementsProvider(t *testing.T) {
	var _ Provider = (*OllamaProvider)(nil)
}

func TestOpenRouterProvider_ImplementsProvider(t *testing.T) {
	var _ Provider = (*OpenRouterProvider)(nil)
}

func TestInheritProvider_ImplementsProvider(t *testing.T) {
	var _ Provider = (*InheritProvider)(nil)
}
