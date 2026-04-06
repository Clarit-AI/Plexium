// Package agent provides the LLM provider cascade, rate limiting, and cost
// tracking for the Plexium assistive agent.
package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Clarit-AI/Plexium/internal/retry"
)

// ErrNoProvider is returned when no provider in the cascade is available.
var ErrNoProvider = errors.New("agent: no available provider")

// ErrNoInheritProvider is returned by InheritProvider — the primary agent
// handles completion externally, so this stub always fails.
var ErrNoInheritProvider = errors.New("agent: inherit provider requires external handling")

// Provider is the interface that LLM backends must implement.
type Provider interface {
	Name() string
	IsAvailable() bool
	HealthCheck() error
	Complete(ctx context.Context, prompt string) (*CompletionResult, error)
	CostPerToken() float64
}

// CompletionResult holds the output and telemetry from a single LLM call.
type CompletionResult struct {
	Provider   string  `json:"provider"`
	Response   string  `json:"response"`
	TokensUsed int     `json:"tokensUsed"`
	CostUSD    float64 `json:"costUSD"`
	LatencyMs  int64   `json:"latencyMs"`
}

// ProviderCascade tries providers in cost order (cheapest first), falling
// through to the next provider on failure.
type ProviderCascade struct {
	providers []Provider
	retry     *retry.RetryPolicy
}

// NewCascade creates a ProviderCascade. Providers are sorted by CostPerToken
// (cheapest first) at construction time.
func NewCascade(providers []Provider, retryPolicy *retry.RetryPolicy) *ProviderCascade {
	sorted := make([]Provider, len(providers))
	copy(sorted, providers)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CostPerToken() < sorted[j].CostPerToken()
	})
	return &ProviderCascade{
		providers: sorted,
		retry:     retryPolicy,
	}
}

// Complete tries each available provider in cost order. If a provider fails,
// the cascade falls through to the next one. Returns ErrNoProvider if all
// providers are unavailable or exhausted.
func (c *ProviderCascade) Complete(ctx context.Context, prompt string) (*CompletionResult, error) {
	var lastErr error

	for _, p := range c.providers {
		if !p.IsAvailable() {
			continue
		}

		var result *CompletionResult
		err := c.retry.DoWithContext(ctx, func() error {
			var callErr error
			result, callErr = p.Complete(ctx, prompt)
			if callErr != nil {
				return fmt.Errorf("%s: %w", p.Name(), callErr)
			}
			return nil
		})

		if err == nil {
			return result, nil
		}

		lastErr = err
	}

	if lastErr != nil {
		return nil, fmt.Errorf("%w: last error: %v", ErrNoProvider, lastErr)
	}
	return nil, ErrNoProvider
}

// ---------------------------------------------------------------------------
// OllamaProvider
// ---------------------------------------------------------------------------

// OllamaProvider calls a local Ollama HTTP API.
type OllamaProvider struct {
	endpoint string
	model    string
	httpPost func(ctx context.Context, url, body string) (string, int, error) // injectable
}

// NewOllamaProvider creates an OllamaProvider.
// httpPost is injectable for testing; pass nil for a real implementation stub.
func NewOllamaProvider(endpoint, model string, httpPost func(ctx context.Context, url, body string) (string, int, error)) *OllamaProvider {
	return &OllamaProvider{
		endpoint: endpoint,
		model:    model,
		httpPost: httpPost,
	}
}

func (o *OllamaProvider) Name() string       { return "ollama" }
func (o *OllamaProvider) CostPerToken() float64 { return 0.0 } // local, free

func (o *OllamaProvider) IsAvailable() bool {
	return o.endpoint != ""
}

func (o *OllamaProvider) HealthCheck() error {
	if o.endpoint == "" {
		return fmt.Errorf("ollama: no endpoint configured")
	}
	return nil
}

func (o *OllamaProvider) Complete(ctx context.Context, prompt string) (*CompletionResult, error) {
	if o.httpPost == nil {
		return nil, fmt.Errorf("ollama: httpPost not configured")
	}

	start := time.Now()
	body := fmt.Sprintf(`{"model":%q,"prompt":%q,"stream":false}`, o.model, prompt)
	resp, tokens, err := o.httpPost(ctx, o.endpoint+"/api/generate", body)
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}

	return &CompletionResult{
		Provider:   "ollama",
		Response:   resp,
		TokensUsed: tokens,
		CostUSD:    0.0,
		LatencyMs:  time.Since(start).Milliseconds(),
	}, nil
}

// ---------------------------------------------------------------------------
// OpenRouterProvider
// ---------------------------------------------------------------------------

// OpenRouterProvider calls an OpenAI-compatible API (e.g. OpenRouter).
type OpenRouterProvider struct {
	endpoint    string
	model       string
	apiKey      string
	costPerTok  float64
	httpPost    func(ctx context.Context, url, body string, headers map[string]string) (string, int, error) // injectable
}

// NewOpenRouterProvider creates an OpenRouterProvider.
func NewOpenRouterProvider(endpoint, model, apiKey string, costPerToken float64, httpPost func(ctx context.Context, url, body string, headers map[string]string) (string, int, error)) *OpenRouterProvider {
	return &OpenRouterProvider{
		endpoint:   endpoint,
		model:      model,
		apiKey:     apiKey,
		costPerTok: costPerToken,
		httpPost:   httpPost,
	}
}

func (r *OpenRouterProvider) Name() string          { return "openrouter" }
func (r *OpenRouterProvider) CostPerToken() float64 { return r.costPerTok }

func (r *OpenRouterProvider) IsAvailable() bool {
	return r.endpoint != "" && r.apiKey != ""
}

func (r *OpenRouterProvider) HealthCheck() error {
	if r.endpoint == "" {
		return fmt.Errorf("openrouter: no endpoint configured")
	}
	if r.apiKey == "" {
		return fmt.Errorf("openrouter: no API key configured")
	}
	return nil
}

func (r *OpenRouterProvider) Complete(ctx context.Context, prompt string) (*CompletionResult, error) {
	if r.httpPost == nil {
		return nil, fmt.Errorf("openrouter: httpPost not configured")
	}

	start := time.Now()
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":%q}]}`, r.model, prompt)
	headers := map[string]string{
		"Authorization": "Bearer " + r.apiKey,
		"Content-Type":  "application/json",
	}

	resp, tokens, err := r.httpPost(ctx, r.endpoint+"/v1/chat/completions", body, headers)
	if err != nil {
		return nil, fmt.Errorf("openrouter: %w", err)
	}

	cost := float64(tokens) * r.costPerTok

	return &CompletionResult{
		Provider:   "openrouter",
		Response:   resp,
		TokensUsed: tokens,
		CostUSD:    cost,
		LatencyMs:  time.Since(start).Milliseconds(),
	}, nil
}

// ---------------------------------------------------------------------------
// InheritProvider (stub)
// ---------------------------------------------------------------------------

// InheritProvider is a stub provider that always returns ErrNoInheritProvider.
// The primary agent (claude, codex, gemini) handles LLM calls externally —
// this provider exists so the cascade can reference it without special-casing.
type InheritProvider struct{}

func (i *InheritProvider) Name() string                                                   { return "inherit" }
func (i *InheritProvider) IsAvailable() bool                                              { return true }
func (i *InheritProvider) HealthCheck() error                                             { return nil }
func (i *InheritProvider) CostPerToken() float64                                          { return 0.0 }
func (i *InheritProvider) Complete(_ context.Context, _ string) (*CompletionResult, error) {
	return nil, ErrNoInheritProvider
}
