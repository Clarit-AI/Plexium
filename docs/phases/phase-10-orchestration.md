# Phase 10: Orchestration

> **Model:** Frontier — Opus 4.6 (primary), GPT 5.4 acceptable
> **Execution:** Agent-Teams (claude-code) or sub-agents (codex)
> **Status:** Pending  
> **bd Epic:** `plexium-m10`  
> **Prerequisites:** Phase 9 complete

## Objective

Implement the assistive agent (provider cascade for wiki maintenance), daemon mode (autonomous polling and work dispatch), `plexium compile` (deterministic navigation regeneration), git worktree workspace management, and bounded concurrency with retry policy. This completes the Plexium vision: proactive wiki maintenance that compounds knowledge without human triggers.

## Architecture Context

- [Scaling Considerations](../architecture/core-architecture.md#scaling-considerations) — Concurrency model, daemon behavior
- [Invariants](../architecture/core-architecture.md#invariants--failure-modes) — Never-violated rules in daemon mode
- [Configuration](../architecture/core-architecture.md#configuration) — `assistiveAgent`, `daemon`, `retry` config

## Spec Sections Covered

- §16 Tool Integrations (assistive agent, symphony, daemon)
- §10 Workflows & Operations (Operation 6: Autonomous Maintenance Daemon)
- §9 The CLI (`plexium daemon`, `plexium agent`, `plexium compile`, `plexium orchestrate`)

## Deliverables

1. **Assistive agent** — Provider cascade (local, OpenRouter, primary) for wiki tasks
2. **Task router** — Cognitive complexity classification for routing decisions
3. **`plexium agent` CLI** — start, stop, status, test, spend, benchmark
4. **Rate limit tracking** — Daily budget and adaptive batching
5. **`plexium daemon` command** — Long-running maintenance loop
6. **`plexium compile` command** — Deterministic navigation regeneration
7. **Git worktree workspace manager** — Per-issue isolated workspaces
8. **Retry policy** — Exponential backoff for transient failures
9. **Tracker adapter** — Interface for GitHub Issues, Linear, none
10. **Runner adapter** — Agent-neutral dispatch interface

## Tasks

### M10.1: Assistive Agent Provider Cascade

Implement the provider cascade for the assistive agent.

**Provider cascade (tried in order):**
1. **local** (Ollama) — Free, private, requires hardware
2. **openrouter-free** — Free tier, no hardware
3. **openrouter-budget** — Paid tier, higher rate limits
4. **primary** — Whatever the coding agent uses (inherit)

**Config in `config.yml`:**
```yaml
assistiveAgent:
  enabled: true
  providers:
    - name: local
      enabled: false
      type: ollama
      endpoint: "http://localhost:11434"
      model: "gemma4:26b-a4b"
    - name: openrouter-free
      enabled: true
      type: openai-compatible
      endpoint: "https://openrouter.ai/api/v1"
      model: "openrouter/free"
      apiKeyEnv: "OPENROUTER_API_KEY"
    - name: primary
      enabled: true
      type: inherit
```

**Implementation:**
```go
// internal/agent/cascade.go
type ProviderCascade struct {
    providers []Provider
    primary   Provider  // The coding agent's LLM
}

type Provider interface {
    Name() string
    IsAvailable() bool
    HealthCheck() error
    Complete(prompt string) (string, error)
    CostPerToken() float64
}

type CascadeResult struct {
    Provider    string
    Response    string
    TokensUsed  int
    CostUSD     float64
    LatencyMs   int
}

func (c *ProviderCascade) Complete(prompt string) (*CascadeResult, error) {
    // 1. Try cheapest available provider
    // 2. On failure (unavailable, rate-limited, health check failed), try next
    // 3. Primary agent is always final fallback
    // 4. Track costs and latency for reporting
}
```

### M10.2: Task Router

Route wiki tasks to appropriate agent tier based on cognitive complexity.

**Task routing:**

| Task Complexity | Agent | Examples |
|----------------|-------|----------|
| Low | Assistive | Frontmatter updates, `_log.md` entries, index regeneration |
| Medium | Assistive | Link validation, cross-reference suggestions, manifest updates |
| High | Primary | Architecture synthesis, ADR creation, contradiction detection |
| Deterministic | None | Hash computation, path validation, orphan detection (no LLM) |

**Implementation:**
```go
// internal/agent/router.go
type TaskRouter struct {
    cascade  *ProviderCascade
    config   *config.Config
}

type TaskComplexity string

const (
    ComplexityLow    TaskComplexity = "low"
    ComplexityMedium TaskComplexity = "medium"
    ComplexityHigh   TaskComplexity = "high"
)

type WikiTask struct {
    Type       string
    Complexity TaskComplexity
    Prompt     string
    Context    []string  // Wiki page paths for context
}

func (r *TaskRouter) Route(task WikiTask) (*CascadeResult, error)
```

**Task classification:**
```go
func ClassifyTask(taskType string) TaskComplexity {
    switch taskType {
    case "frontmatter-update", "log-entry", "index-regeneration", 
         "sidebar-regeneration", "link-validation", "manifest-update",
         "page-state-transition":
        return ComplexityLow
    case "cross-reference-suggestion", "module-summary", "staleness-check":
        return ComplexityMedium
    case "architecture-synthesis", "contradiction-detection", 
         "adr-creation", "complex-ingest", "deep-code-analysis":
        return ComplexityHigh
    default:
        return ComplexityMedium
    }
}
```

### M10.3: plexium agent CLI Commands

Manage the assistive agent.

**Commands:**
```bash
plexium agent start
plexium agent stop
plexium agent status
plexium agent test [--provider name]
plexium agent spend
plexium agent benchmark
```

**Implementation:**
```go
// cmd/agent.go
type AgentCLI struct {
    cascade *ProviderCascade
}

func (cmd *AgentCLI) Status() (*AgentStatus, error)

type AgentStatus struct {
    Enabled     bool
    Providers   []ProviderHealth
    DailyUsage  UsageStats
    DailyBudget float64
}

type UsageStats struct {
    RequestsToday int
    TokensToday   int
    CostTodayUSD  float64
    Remaining     float64
}
```

### M10.4: Rate Limit Tracking

Track daily usage and adapt batching strategy.

**Implementation:**
```go
// internal/agent/ratelimit.go
type RateLimitTracker struct {
    config     *config.Config
    stateFile  string  // .plexium/agent-state.json
}

type UsageRecord struct {
    Date          string
    Provider      string
    Requests      int
    Tokens        int
    CostUSD       float64
}

func (t *RateLimitTracker) Record(provider, response string, tokens int, cost float64)

func (t *RateLimitTracker) GetDailyUsage(provider string) *UsageRecord

func (t *RateLimitTracker) CanMakeRequest(provider string) bool

func (t *RateLimitTracker) GetBatchingDelay(provider string) time.Duration
```

**Adaptive batching:**
- Free tier: batch requests when approaching rate limit
- Track `requestsPerMinute` and `requestsPerDay` from provider config
- Cooldown between batches (configurable, default 3100ms for free tier)

### M10.5: plexium daemon Command

Start the autonomous maintenance loop.

**Command:**
```bash
plexium daemon [--poll-interval 300] [--max-concurrent 2]
```

**Configuration in `config.yml`:**
```yaml
daemon:
  enabled: true
  pollInterval: 300  # seconds
  maxConcurrent: 2
  watches:
    staleness:
      enabled: true
      threshold: 7d
      action: auto-sync  # auto-sync | create-issue | log-only
    lint:
      enabled: true
      interval: 1h
      action: auto-fix
    ingest:
      enabled: true
      watchDir: .wiki/raw/
      action: auto-ingest
    debt:
      enabled: true
      maxDebt: 10
      action: create-issue
```

**Daemon loop:**
```go
// cmd/daemon.go
type Daemon struct {
    config    *config.Config
    pollLoop  *PollLoop
    workspace *WorkspaceMgr
    runner    RunnerAdapter
    tracker   TrackerAdapter
}

func (d *Daemon) Run(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return d.cleanup()
        case <-time.After(d.pollInterval):
            d.tick()
        }
    }
}

func (d *Daemon) tick() {
    // 1. Check for stale pages
    // 2. Check for lint findings above threshold
    // 3. Check for new files in .wiki/raw/
    // 4. Check WIKI-DEBT count
    // 5. Claim eligible work items (bounded by maxConcurrent)
    // 6. Create isolated git worktree for each
    // 7. Sequence: Retriever → Coder → Documenter → Linter
    // 8. On success: PR with Wiki Integrity Report
    // 9. On failure: retry with backoff, then release
}
```

### M10.6: plexium compile Command

Deterministically regenerate shared navigation files.

**Command:**
```bash
plexium compile [--dry-run]
```

**Implementation:**
```go
// cmd/compile.go
type Compiler struct {
    manifestMgr *manifest.Manager
    wikiPath    string
    generators  *GeneratorSet
}

func (c *Compiler) Compile() error {
    // 1. Load manifest to get all pages
    // 2. Build page index (path, title, section, summary)
    // 3. Generate _index.md from page index
    // 4. Generate _Sidebar.md from page index
    // 5. Generate Home.md from architecture overview
    // 6. Merge _log.md entries from parallel runs (timestamp sort)
    // 7. Update manifest with new hashes/timestamps
}
```

**Requirements:**
- No LLM calls (purely deterministic)
- Same input always produces identical output
- Runs after each daemon tick and by CI on merge
- Must be safe to run concurrently (uses file locking)

### M10.7: Git Worktree Workspace Manager

Per-issue isolated workspaces for parallel operations.

**Implementation:**
```go
// internal/daemon/workspace.go
type WorkspaceMgr struct {
    basePath  string  // .plexium/workspaces/
    worktrees []Worktree
}

type Worktree struct {
    ID       string
    Path     string
    IssueID  string
    Status   string  // running | completed | failed
    StartedAt time.Time
}

func (m *WorkspaceMgr) Create(issueID string) (*Worktree, error) {
    // git worktree add .plexium/workspaces/<ID>
    // Returns isolated checkout for this work item
}

func (m *WorkspaceMgr) Cleanup(id string) error {
    // git worktree remove .plexium/workspaces/<ID>
    // Prune stale worktrees on startup
}
```

**Lifecycle:**
1. Daemon claims work item
2. Create worktree: `git worktree add .plexium/workspaces/<ID>`
3. Run agent sequence in worktree
4. On completion: merge changes, remove worktree
5. On failure: retry or release, cleanup worktree

### M10.8: Retry Policy

Exponential backoff for transient failures.

**Implementation:**
```go
// internal/retry/retry.go
type RetryPolicy struct {
    MaxAttempts       int
    InitialDelay     time.Duration
    BackoffMultiplier float64
    MaxDelay         time.Duration
}

func (p *RetryPolicy) Do(fn func() error) error {
    var lastErr error
    delay := p.InitialDelay
    
    for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
        if err := fn(); err != nil {
            lastErr = err
            if !isRetryable(err) {
                return err  // Non-retryable error, fail fast
            }
            if attempt < p.MaxAttempts {
                time.Sleep(delay)
                delay = time.Duration(float64(delay) * p.BackoffMultiplier)
                if delay > p.MaxDelay {
                    delay = p.MaxDelay
                }
            }
        } else {
            return nil  // Success
        }
    }
    return fmt.Errorf("max attempts (%d) exceeded: %w", p.MaxAttempts, lastErr)
}

// Default policy: 3 attempts, 5s initial, 2x backoff, 60s max
```

**Retryable errors:**
- LLM API timeout
- Rate limit hit
- GitHub API transient error
- Network timeout

**Non-retryable:**
- Invalid config
- Missing source files
- Authentication failure

### M10.9: Tracker Adapter Interface

Interface for issue tracker integration.

**Interface:**
```go
// internal/daemon/tracker.go
type TrackerAdapter interface {
    CreateIssue(title, body string) (string, error)  // Returns issue ID
    CloseIssue(id string) error
    AddLabel(issueID, label string) error
    Comment(issueID, body string) error
}

type NoOpTracker struct{}

func (n *NoOpTracker) CreateIssue(title, body string) (string, error) {
    return "", nil  // No-op when tracker is disabled
}

type GitHubIssuesTracker struct {
    owner string
    repo  string
    token string
}

type LinearTracker struct {
    // Linear API client
}
```

**Usage in daemon:**
- WIKI-DEBT exceeds threshold → create issue
- Stale pages detected → create issue
- Daemon run completed → close issue

### M10.10: Runner Adapter Interface

Agent-neutral dispatch interface.

**Interface:**
```go
// internal/daemon/runner.go
type RunnerAdapter interface {
    Run(ctx context.Context, role Role, prompt string, contextPages []string) (*RunResult, error)
}

type RunResult struct {
    Output      string
    TokensUsed  int
    CostUSD     float64
    LatencyMs   int
}

type ClaudeRunner struct {
    // Claude Code runner (shell out to claude code --print)
}

type CodexRunner struct {
    // OpenAI Codex runner
}

type GeminiRunner struct {
    // Gemini CLI runner
}
```

**Role sequencing:**
1. **Retriever**: Use PageIndex or read relevant pages
2. **Coder**: Implement changes (only if code changes needed)
3. **Documenter**: Update wiki pages
4. **Linter**: Run `plexium lint --deterministic`

## Interfaces

**Consumes from Phase 9:**
- PageIndex MCP server
- Beads task linking
- Memento transcript ingestion

**Provides to Phase 10:**
- This is the final milestone

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | Provider cascade tries cheapest tier first |
| AC2 | Cascade falls through on failure to next provider |
| AC3 | Primary agent is always final fallback |
| AC4 | Task router classifies tasks by complexity |
| AC5 | Low/medium tasks route to assistive agent |
| AC6 | High tasks route to primary agent |
| AC7 | Rate limit tracking works per provider |
| AC8 | Daily budget respected |
| AC9 | `plexium agent status` shows all provider health |
| AC10 | Daemon polls at configured interval |
| AC11 | Daemon respects maxConcurrent bound |
| AC12 | Worktrees created and cleaned up properly |
| AC13 | `plexium compile` produces deterministic output |
| AC14 | `plexium compile` produces identical output for same input |
| AC15 | Retry policy applies exponential backoff |
| AC16 | Failed work items released after max retries |
| AC17 | Tracker adapter creates/closes issues correctly |
| AC18 | Runner adapter dispatches to correct agent |

## bd Task Mapping

```
plexium-m10
├── M10.1: Assistive agent provider cascade
├── M10.2: Task router (cognitive complexity classification)
├── M10.3: plexium agent CLI (start, stop, status, test, spend, benchmark)
├── M10.4: Rate limit tracking
├── M10.5: plexium daemon command
├── M10.6: plexium compile command
├── M10.7: Git worktree workspace manager
├── M10.8: Retry policy (exponential backoff)
├── M10.9: Tracker adapter interface
└── M10.10: Runner adapter interface
```
