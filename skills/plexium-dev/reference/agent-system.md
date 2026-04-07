# Agent System Architecture

## Provider Cascade

The cascade tries providers in cost order (cheapest first). Each provider implements:

```go
type Provider interface {
    Name() string
    IsAvailable() bool
    HealthCheck() error
    Complete(ctx context.Context, prompt string) (*CompletionResult, error)
    CostPerToken() float64
}
```

Three implementations:
- **OllamaProvider** — local, cost=0. Calls `/api/generate`. Injectable `httpPost` function.
- **OpenRouterProvider** — remote, cost=configurable. Calls `/v1/chat/completions` (OpenAI format). Injectable `httpPost` function.
- **InheritProvider** — stub that always returns `ErrNoInheritProvider`. Represents the primary coding agent handling LLM calls externally.

## HTTP Transport

`http.go` provides the default HTTP functions injected into providers:
- `DefaultOllamaHTTPPost` — parses `{"response":"...", "eval_count":N}`
- `DefaultOpenRouterHTTPPost` — parses `{"choices":[{"message":{"content":"..."}}], "usage":{"total_tokens":N}}`

For testing, inject mock functions instead. All existing tests use this pattern.

## Task Router

`TaskRouter` classifies tasks and routes them:

```
Deterministic → rejected (ErrDeterministicTask — handle with code, not LLM)
Low/Medium    → assistive cascade (cheapest providers)
High          → primary cascade (coding agent's own LLM)
```

Classification is based on `taskType` string. Unknown types default to Medium.

## Rate Limiter

`RateLimitTracker` persists daily usage to `.plexium/agent-state.json`:

```json
{"date":"2026-04-06","records":{"ollama":{"requests":42,"tokens":8000,"costUSD":0}}}
```

Resets daily. `CanMakeRequest()` checks against budget. `GetBatchingDelay()` returns adaptive delays as usage approaches the cap.

## Setup Flow

`setup.go` handles interactive provider configuration:
- PKCE OAuth for OpenRouter (localhost:3000 callback, code exchange, key validation)
- Ollama detection via `/api/tags` endpoint
- Writes credentials to `.plexium/credentials.json` (mode 0600)
- Updates `.plexium/config.yml` with provider entries

## Wiring in main.go

`buildCascadeFromConfig()` reads `cfg.AssistiveAgent.Providers` and constructs the cascade:
- Passes `DefaultOllamaHTTPPost` / `DefaultOpenRouterHTTPPost` as real transport
- Creates `RateLimitTracker` pointing to `.plexium/agent-state.json`
- Used by: agent commands (test, benchmark, status), lint --full, daemon (future)
