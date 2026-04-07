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

Query the wiki from the command line:

```bash
plexium retrieve "authentication flow"
plexium retrieve "database schema" --format json
```

Retrieve searches the wiki using BM25-scored PageIndex. It matches against page titles, content, wiki-links, and section names. When PageIndex returns no results, it falls back to `_index.md` parsing and content grep.

### MCP Server Mode

For agents that support MCP (Model Context Protocol):

```bash
plexium pageindex serve
```

This starts a JSON-RPC 2.0 server in stdio mode. Agents connect via MCP to query the wiki index programmatically.

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

Plexium generates agent-specific instruction files via plugins:

```bash
# List available plugins
plexium plugin list

# Install the Claude adapter
plexium plugin add claude

# Install from a custom path
plexium plugin add my-adapter --path /path/to/plugin
```

Each plugin runs a `plugin.sh` script that generates an instruction file (e.g., `CLAUDE.md`, `.cursorrules`) tailored to the wiki's structure and schema. Four adapters are included: Claude, Codex, Cursor, and Gemini.

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

## Experimental Features

The following features have functional scaffolding but incomplete integration points. See [Status](status.md) for details.

### Daemon [Experimental]

```bash
plexium daemon --poll-interval 300 --max-concurrent 2
```

The daemon polls for staleness, lint violations, ingest opportunities, and documentation debt. It creates isolated git worktrees for concurrent work. The issue tracker integration (LinearTracker) is not yet implemented. The runner dispatches to external CLI tools (Claude, Codex, Gemini) but these integrations are untested.

### Orchestrate [Experimental]

```bash
plexium orchestrate --issue PROJ-123
```

Creates an isolated git worktree for a single issue, runs retriever and documenter agent roles, then cleans up. The default runner is noop (no actual agent execution). Requires a configured provider and runner to do real work.

### Assistive Agent [Partial]

The agent lifecycle commands (`start`, `stop`) are stubs. The monitoring commands work:

```bash
plexium agent status      # Provider health and cost tracking
plexium agent test        # Test provider connectivity
plexium agent spend       # Daily spend per provider
plexium agent benchmark   # Latency benchmarking
```
