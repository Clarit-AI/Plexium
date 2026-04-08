# User Guide

This guide covers Plexium workflows: how to set up, maintain, and publish a wiki, and how the system integrates with agents, CI, and task tracking.

For command-level detail, see [CLI Reference](cli-reference.md). For stability information, see [Status](status.md).

---

## How Plexium Works

LLM coding agents lose context between sessions. Every new session rediscovers the same knowledge through RAG, parsing the same files, rebuilding the same understanding, then discarding it. No learning compounds.

Plexium eliminates this by giving your repository a persistent knowledge layer. The system operates on three layers:

```
Source Layer (immutable)
  src/**, docs/**, README, ADRs
        |
State Manifest (.plexium/manifest.json)
  Bidirectional source-to-wiki mapping, content hashes, ownership
        |
Wiki Layer (.wiki/)
  _schema.md, _index.md, modules/, decisions/, concepts/
```

- **Source layer**: your code and docs. Plexium reads from this layer but never modifies it.
- **State manifest**: tracks which source files map to which wiki pages, stores content hashes for staleness detection, and records ownership metadata.
- **Wiki layer**: the synthesized knowledge surface. Agents read the wiki before working on a task and update it after every change.

Git hooks and CI pipelines enforce this discipline. The wiki is simultaneously browsable as an Obsidian vault, publishable as a GitHub Wiki, and queryable via MCP by any coding agent.

The result: every agent session starts with accumulated project knowledge instead of a cold start, and every session contributes back.

---

## Greenfield: New Repository

For a repository with no existing wiki:

```bash
# 1. Initialize
plexium init

# 2. Review and customize config
$EDITOR .plexium/config.yml

# 3. Compile navigation
plexium compile

# 4. Validate
plexium lint --deterministic
plexium doctor
```

**Customize config** to match your project:

- `sources.include`: glob patterns for files Plexium should track (default: `**/*.go`, `**/*.md`, `**/*.yml`, `**/*.yaml`)
- `sources.exclude`: patterns to skip (default: `vendor/**`, `.wiki/**`, `.plexium/**`)
- `taxonomy.sections`: wiki section names (default: Architecture, Modules, Decisions, Patterns, Concepts, Guides)
- `enforcement.strictness`: how aggressively hooks enforce wiki updates

---

## Brownfield: Existing Repository

For a repository that already has source code:

```bash
# 1. Initialize (if not done)
plexium init

# 2. Preview conversion
plexium convert --dry-run

# 3. Run conversion
plexium convert

# 4. Review generated pages
ls .wiki/modules/ .wiki/architecture/ .wiki/decisions/

# 5. Compile navigation
plexium compile

# 6. Validate
plexium lint --deterministic
```

Convert runs a five-phase pipeline: scour (find source files), filter (apply include/exclude), ingest (generate pages), link (inject cross-references), and lint (gap analysis).

**Quality caveat:** Convert uses heuristic-based content extraction. Page quality varies by codebase. Use `--depth deep` for AST-level analysis on supported languages. Review and edit generated pages, especially for complex modules.

---

## Incremental: The Daily Loop

After the initial setup, the daily workflow is:

```bash
# 1. Sync after source changes
plexium sync

# 2. Check wiki health
plexium lint --deterministic

# 3. Recompile navigation (if pages changed)
plexium compile
```

`plexium sync` compares stored source hashes against current file contents, marks stale pages, updates the manifest, and recompiles navigation. Running sync twice after a source change produces 0 stale pages on the second run (idempotent).

With git hooks enabled, step 1 happens automatically: the pre-commit hook checks whether wiki updates accompany source changes, and the post-commit hook tracks debt when commits bypass the hook.

---

## The Agent Workflow

When LLM coding agents work on your repo, the schema (`_schema.md`) instructs them to follow this loop:

1. **READ**: read `_index.md`, fetch relevant wiki pages, check `_log.md`
2. **EXECUTE**: perform the coding task
3. **DOCUMENT**: update wiki pages, create ADRs, update `_log.md`, add cross-references
4. **VALIDATE**: verify wiki-links resolve, mark uncertain claims, check for contradictions

The pre-commit hook enforces step 3. If an agent changes source files but does not update the wiki, the commit is blocked (in strict mode) or flagged (in moderate/advisory mode).

**Bypassing the hook:**

```bash
git commit --no-verify -m "emergency fix"
```

The post-commit hook tracks this as WIKI-DEBT. Run `plexium sync` later to clear it.

**Trivial change exception:** The schema defines a carve-out for single-file changes. These need only a `_log.md` entry, not a full wiki update.

---

## Ownership Model

Every wiki page has an ownership mode that controls who can modify it:

| Mode | Who Edits | Behavior |
|------|-----------|----------|
| `managed` | Agents only | Plexium regenerates this page from source files. Human edits are overwritten on next sync/convert. |
| `human-authored` | Humans only | Locked from automated changes. Agents cannot overwrite this page. Not flagged as stale. |
| `co-maintained` | Both | Agents append to existing sections but do not rewrite human-written content. |

Ownership is tracked in the manifest (`Pages[].ownership`) and in page frontmatter.

**To change a page's ownership**, edit its frontmatter:

```yaml
---
title: Authentication Architecture
ownership: human-authored
last-updated: 2026-04-06
---
```

And update the manifest entry to match. The ownership value in the manifest is authoritative for enforcement.

---

## Publishing to GitHub Wiki

### Direct publish

```bash
# Preview
plexium publish --dry-run

# Push
plexium publish
```

Publish clones the GitHub Wiki repository, replaces its content with filtered `.wiki/` files, and pushes. It respects:

- `sensitivity.neverPublish`: files that must never leave the repo
- `sensitivity.excludeExtensions`: blocked file types (`.env`, `.key`, `.pem`, `.secret`)
- `publish.preserveUnmanagedPages`: whether to include human-authored pages

### Selective sync

```bash
# Preview
plexium gh-wiki-sync --dry-run

# Sync and push
plexium gh-wiki-sync --push
```

gh-wiki-sync uses `githubWiki.publish` (include patterns) and `githubWiki.exclude` (exclude patterns) from config for fine-grained control over what reaches the GitHub Wiki.

### Configuration

```yaml
# .plexium/config.yml
githubWiki:
  enabled: true
  publish:
    - "*.md"
    - "modules/**"
    - "architecture/**"
  exclude:
    - "raw/**"
    - "_log.md"

sensitivity:
  neverPublish:
    - "internal-notes/**"
  excludeExtensions:
    - ".env"
    - ".key"
```

---

## Running Lint in CI

Add `plexium ci check` to your CI pipeline to verify that wiki updates accompany source changes:

```yaml
# .github/workflows/wiki-check.yml
name: Wiki Check
on: [pull_request]

jobs:
  wiki-lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      - name: Install Plexium
        run: go install github.com/Clarit-AI/Plexium/cmd/plexium@latest

      - name: Check wiki coverage
        run: |
          plexium ci check \
            --base ${{ github.event.pull_request.base.sha }} \
            --head ${{ github.sha }}

      - name: Run deterministic lint
        run: plexium lint --deterministic --ci
```

Exit codes: 0 (clean), 1 (errors), 2 (warnings with `--fail-on warning`).

---

## Wiki Retrieval

Plexium includes a built-in search engine that works immediately after `plexium init`. No additional setup or configuration is required.

### CLI Search

Query the wiki from the command line:

```bash
plexium retrieve "authentication flow"
plexium retrieve "database schema" --format json
```

The search engine uses BM25-style scoring across five dimensions: page titles, section headings, summaries, content bodies, and `[[wiki-links]]`. Results are ranked by relevance. When the PageIndex returns no results, it falls back to `_index.md` parsing combined with content grep to ensure queries always return something useful.

### MCP Server (Optional)

The MCP server exposes the same PageIndex search engine over JSON-RPC 2.0 stdio, making the wiki queryable by any agent that supports the [Model Context Protocol](https://modelcontextprotocol.io). This is not a separate system -- it is the same engine that powers `plexium retrieve`, accessed through a different interface.

```bash
plexium pageindex serve
```

The server exposes three tools:

| Tool | Description |
|------|-------------|
| `pageindex_search` | Search wiki pages by query string |
| `pageindex_get_page` | Retrieve a specific page by path |
| `pageindex_list_pages` | List all indexed pages |

#### Wiring the MCP server to your agent

The canonical path is the higher-level setup command:

```bash
plexium setup claude
plexium setup codex
```

Add `--write-config` to let Plexium apply the native MCP command automatically:

```bash
plexium setup claude --write-config
plexium setup codex --write-config
```

If you only want the native MCP command itself, Plexium can print it directly:

```bash
plexium pageindex connect claude
plexium pageindex connect codex
```

Add `--write-config` to have Plexium run the native command for you:

```bash
plexium pageindex connect claude --write-config
plexium pageindex connect codex --write-config
```

Current native commands:

**Claude Code** (project-scoped MCP in `.mcp.json`):

```bash
claude mcp add --scope project plexium-wiki -- plexium pageindex serve
```

**Codex** (managed by Codex `config.toml`):

```bash
codex mcp add plexium-wiki -- plexium pageindex serve
```

Once configured, your agent can call `pageindex_search`, `pageindex_get_page`, and `pageindex_list_pages` as MCP tools during its sessions.

---

## Beads Integration [Stable]

Link task tracking (bd/beads) entries to wiki pages:

```bash
# Link a task to a wiki page
plexium beads link BD-42 modules/auth.md

# Remove a link
plexium beads unlink BD-42 modules/auth.md

# Find pages for a task
plexium beads pages BD-42

# Find tasks for a page
plexium beads tasks modules/auth.md

# Build the complete mapping
plexium beads scan
```

Links are stored in wiki page frontmatter as `beads-ids: [BD-42, BD-43]`. Linking is idempotent.

---

## Plugin Adapters [Stable]

Plexium generates agent-specific instruction files via plugins. Most users should prefer the higher-level onboarding command:

```bash
plexium setup claude
plexium setup codex
```

If you want to manage adapters directly, the underlying plugin commands are still available:

```bash
# List available plugins
plexium plugin list

# Install the Claude adapter
plexium plugin add claude

# Install from a custom path
plexium plugin add my-adapter --path /path/to/plugin
```

Each plugin runs a `plugin.sh` script that generates an instruction file (e.g., `CLAUDE.md`, `AGENTS.md`, `.cursor/rules/plexium.mdc`) tailored to the wiki's structure and schema. Four adapters are included: Claude, Codex, Cursor, and Gemini.

- `claude` generates `CLAUDE.md`
- `codex` generates `AGENTS.md`
- `cursor` generates `.cursor/rules/plexium.mdc`
- `gemini` generates `.gemini/config.md`

**Plugin structure:**

```
.plexium/plugins/<name>/
  manifest.json    # Name, version, description, instruction file path
  plugin.sh        # Setup script (generates agent instructions)
```

---

## Schema Migrations [Stable]

When the `_schema.md` format changes between Plexium versions:

```bash
# Preview what migrations would run
plexium migrate --dry-run

# Apply migrations
plexium migrate

# Migrate to a specific version
plexium migrate --version 3
```

Migration scripts live in `.plexium/migrations/` and apply in order.

---

## Configuration Overview

`.plexium/config.yml` controls all Plexium behavior. Key sections:

| Section | Purpose |
|---------|---------|
| `version` | Config schema version (currently 1) |
| `repo` | Default branch, GitHub Wiki toggle |
| `sources` | Include/exclude globs for source file scanning |
| `agents` | Agent adapter list, strictness level |
| `wiki` | Wiki directory paths (root, home, sidebar, footer, log, index, schema) |
| `taxonomy` | Section names, auto-classification toggle |
| `publish` | Branch, commit message, auto-push, preserve unmanaged pages |
| `sync` | Mode (incremental/full), auto-sync triggers, idempotent flag |
| `enforcement` | Pre-commit hook, CI check, memento gate, strictness, debt thresholds |
| `integrations` | LLM provider, memento, beads, pageindex, obsidian toggles |
| `reports` | Output formats (json/markdown/both) and output directory |
| `githubWiki` | Publish/exclude patterns for GitHub Wiki sync |
| `sensitivity` | Never-publish rules, max file size, blocked extensions |
| `assistiveAgent` | Provider cascade config (ollama/openai-compatible), daily budget |
| `daemon` | Poll interval, max concurrent worktrees, watch configurations |
| `retry` | Max attempts, backoff multiplier, delay bounds |

Environment variables override config values. See [CLI Reference: Environment Variables](cli-reference.md#environment-variables) for the full list.

---

## Assistive Agent Setup

Plexium includes an assistive agent that automates wiki maintenance using a provider cascade. Providers are tried in cost order (cheapest first), falling through on failure. Setup is optional -- Plexium works without it, but the daemon, LLM lint, and autonomous maintenance features require at least one provider.

### Option A: Ollama (Local, Free)

Best for: local development, air-gapped environments, zero-cost operation.

1. Install Ollama: https://ollama.ai

2. Pull a model:

```bash
ollama pull llama3.2
```

3. Add to `.plexium/config.yml`:

```yaml
assistiveAgent:
  enabled: true
  providers:
    - name: local-ollama
      enabled: true
      type: ollama
      endpoint: http://localhost:11434
      model: llama3.2
  budget:
    dailyUSD: 0
```

4. Verify:

```bash
plexium agent test
```

### Option B: OpenRouter (Remote, Free Tier Available)

Best for: higher-quality models, teams without local GPU, free-tier models for light usage.

1. Create an account at https://openrouter.ai and get an API key.

2. Choose a setup path:

**Security note:** Never paste API keys or other secrets into an AI chat window. They can become part of the model context stream, logs, or session transcripts. In repositories using memento, that context may later be attached to commits as git notes.

If you already pasted a secret into chat:
- stop and rotate the secret if needed
- rewind the session if your client supports it
- do not commit that session to memento or publish its notes

Direct setup through Plexium:

```bash
plexium agent setup --api-key "sk-or-v1-..."
```

Or, even better, export the key in your terminal and let `plexium agent setup` pick it up automatically without ever placing the secret in chat:

```bash
export OPENROUTER_API_KEY="sk-or-v1-..."
plexium agent setup
```

3. Plexium saves the key in `.plexium/credentials.json`, writes `.plexium/.env` for convenience, and updates `.plexium/config.yml`.

If you prefer to wire the provider manually, the resulting config looks like:

```yaml
assistiveAgent:
  enabled: true
  providers:
    - name: openrouter
      enabled: true
      type: openai-compatible
      endpoint: https://openrouter.ai/api
      model: meta-llama/llama-3.1-8b-instruct:free
      apiKeyEnv: OPENROUTER_API_KEY
  budget:
    dailyUSD: 0.50
```

4. Verify:

```bash
plexium agent test
```

**Free models on OpenRouter** (no API spend): `meta-llama/llama-3.1-8b-instruct:free`, `google/gemma-2-9b-it:free`, `mistralai/mistral-7b-instruct:free`. Check https://openrouter.ai/models for current availability.

### Option C: Both (Cascade)

Use Ollama for cheap tasks and OpenRouter as fallback:

```yaml
assistiveAgent:
  enabled: true
  providers:
    - name: local-ollama
      enabled: true
      type: ollama
      endpoint: http://localhost:11434
      model: llama3.2
    - name: openrouter-fallback
      enabled: true
      type: openai-compatible
      endpoint: https://openrouter.ai/api
      model: meta-llama/llama-3.1-8b-instruct:free
      apiKeyEnv: OPENROUTER_API_KEY
  budget:
    dailyUSD: 1.00
```

The cascade sorts providers by cost (Ollama = $0, so it always goes first). If Ollama is down or fails, the request falls through to OpenRouter.

### Any OpenAI-Compatible API

The `openai-compatible` type works with any API that speaks the OpenAI `/v1/chat/completions` format: OpenRouter, Together AI, Groq, local vLLM, etc. Set the `endpoint` and `model` accordingly.

### Budget Controls

The rate limiter tracks daily spend per provider and persists it to `.plexium/agent-state.json`. When usage approaches the budget cap, the daemon applies adaptive batching delays (backs off requests) rather than hard-cutting.

```bash
plexium agent spend       # Show daily cost breakdown
plexium agent status      # Show provider health and budget usage
```

### Task Routing

The task router classifies wiki maintenance tasks by complexity and routes them to the appropriate cascade tier:

| Complexity | Example Tasks | Provider |
|------------|---------------|----------|
| Deterministic | Hash computation, path validation, orphan detection | No LLM (pure code) |
| Low | Frontmatter updates, log entries, index regeneration | Cheapest (Ollama) |
| Medium | Cross-reference suggestions, module summaries | Assistive cascade |
| High | Architecture synthesis, contradiction detection, ADR creation | Primary agent or cascade fallback |

---

## Daemon and Autonomous Maintenance

The daemon runs a background poll loop that detects wiki issues and takes action.

### Starting the Daemon

```bash
# Background (managed via PID file)
plexium agent start
plexium agent status      # Shows PID and provider state
plexium agent stop

# Foreground (for debugging)
plexium daemon --poll-interval 300 --max-concurrent 2
```

### Daemon Configuration

```yaml
daemon:
  enabled: true
  pollInterval: 300       # seconds between ticks
  maxConcurrent: 2        # max parallel worktrees
  runner: claude           # claude | codex | gemini | noop
  runnerModel: ""          # optional model override
  tracker: github          # github | none
  watches:
    staleness:
      enabled: true
      action: create-issue  # log-only | create-issue | auto-sync
      threshold: "7d"
    lint:
      enabled: true
      action: log-only
    ingest:
      enabled: true
      action: auto-ingest
      watchDir: ".wiki/raw/"
    debt:
      enabled: true
      action: create-issue
      maxDebt: 10
```

**Actions:** `log-only` records the finding. `create-issue` creates a GitHub issue. `auto-sync`, `auto-fix`, and `auto-ingest` create an isolated git worktree and run the configured runner to resolve the issue automatically.

**Runner:** The daemon shells out to the configured CLI tool (e.g., `claude --print`) in the worktree to perform wiki updates. Set `runner: noop` for dry-run mode.

**Tracker:** Set `tracker: github` and ensure `GITHUB_TOKEN` is set to enable automatic GitHub issue creation.

---

## LLM-Augmented Lint

When the assistive agent is configured, `plexium lint --full` runs semantic analysis in addition to structural checks:

```bash
plexium lint --full
```

This detects:
- **Contradictions** between linked wiki pages
- **Missing concepts** mentioned in 3+ pages without their own page
- **Missing cross-references** between related pages that should link
- **Semantic staleness** where page content appears outdated in meaning

Without the assistive agent, `--full` falls back to deterministic checks only.

---

## Experimental Features

The following features have functional scaffolding but limited real-world testing. See [Status](status.md) for details.

### Orchestrate [Experimental]

```bash
plexium orchestrate --issue PROJ-123
```

Creates an isolated git worktree for a single issue, runs retriever and documenter agent roles, then cleans up. Uses the configured `daemon.runner` from config (defaults to `noop` if not set). Set a real runner (e.g., `claude`) to do actual work.

### Linear Tracker [Stub]

The `tracker: linear` option is defined but not yet implemented. Use `tracker: github` for GitHub issue creation.
