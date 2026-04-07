# Package Reference

## cmd/plexium

Single-file CLI (`main.go`, ~1200 lines). All cobra commands defined here. Key patterns:
- `buildCascadeFromConfig()` constructs the provider cascade from config
- `readPIDFile()` / `processAlive()` for daemon PID management
- Flag parsing happens in `init()`, command logic in `RunE` closures

## internal/agent

The assistive agent system.

- **`cascade.go`** — `ProviderCascade` sorts providers cheapest-first, falls through on failure. Three provider types: `OllamaProvider`, `OpenRouterProvider`, `InheritProvider`. Each has an injectable HTTP function.
- **`http.go`** — Real `net/http` transport functions: `DefaultOllamaHTTPPost`, `DefaultOpenRouterHTTPPost`. Parse Ollama and OpenAI-format JSON responses.
- **`router.go`** — `TaskRouter` classifies wiki tasks by complexity (deterministic/low/medium/high) and routes to assistive or primary cascade.
- **`ratelimit.go`** — `RateLimitTracker` persists daily per-provider usage to `.plexium/agent-state.json`. Adaptive batching delays.
- **`adapter.go`** — `CascadeLLMClient` bridges `lint.LLMClient` to the cascade.
- **`setup.go`** — Interactive setup: PKCE OAuth for OpenRouter, Ollama detection, config writing.

## internal/config

Config loading via viper. `Config` struct mirrors `.plexium/config.yml`. Key sub-structs: `AssistiveAgent`, `DaemonConfig`, `Enforcement`, `Integrations`. Environment variable overrides via `envBindings` map.

## internal/daemon

Autonomous maintenance loop.

- **`daemon.go`** — `Daemon` struct with poll loop. Four watches: staleness, lint, ingest, debt. Actions: log-only, create-issue, auto-sync/fix/ingest.
- **`runner.go`** — `RunnerAdapter` interface. Four implementations: ClaudeRunner, CodexRunner, GeminiRunner, NoOpRunner. All CLI runners shell out to the respective tool.
- **`workspace.go`** — `WorkspaceMgr` manages git worktrees under `.plexium/workspaces/`. Create, cleanup, track active count for concurrency control.
- **`tracker.go`** — `TrackerAdapter` interface. NoOpTracker, GitHubIssuesTracker (via `gh` CLI), LinearTracker (stub).

## internal/lint

- **`linter.go`** — Deterministic checks: broken links, orphan pages, staleness detection, manifest consistency.
- **`llm.go`** — LLM-augmented analysis: contradiction detection, concept extraction, cross-reference suggestions, semantic staleness. Uses `LLMClient` interface.
- **`doctor.go`** — `Doctor` runs diagnostic checks: git repo, config, manifest, wiki structure, schema, lefthook, CI, memento.

## internal/manifest

`Manager` loads/saves `.plexium/manifest.json`. `Manifest` struct with `Pages` map (path → `PageEntry`). Each entry tracks ownership, source files, content hash, timestamps. Deterministic JSON serialization (sorted keys).

## internal/wiki

- **`scaffold.go`** — `Init()` creates `.wiki/` and `.plexium/` directory structure, generates config.yml, _schema.md, Home.md, etc. Handles `--with-memento`, `--with-beads`, `--with-pageindex` flags.

## internal/integrations/pageindex

- **`index.go`** — `PageIndex` scans `.wiki/` markdown files, extracts metadata (title, section, summary, links), provides BM25-style `Search()`.
- **`retrieve.go`** — `Retriever` wraps PageIndex with fallback chain (_index.md parsing + content grep).
- **`server.go`** — MCP-compatible JSON-RPC 2.0 server. Three tools: pageindex_search, pageindex_get_page, pageindex_list_pages.

## internal/convert

Brownfield ingestion pipeline. Five phases: scour (find files), filter (include/exclude), ingest (generate pages), link (cross-references), lint (gap analysis). Configurable depth: shallow (heuristic) or deep (AST-level).
