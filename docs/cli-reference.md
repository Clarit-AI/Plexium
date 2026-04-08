# CLI Reference

> Plexium v0.1.0

Stability tiers are defined in [status.md](status.md). Commands marked **[Stub]** or **[Experimental]** have limited or no functionality.

---

## Global Flags

These flags apply to every command.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--config` | string | `.plexium/config.yml` | Path to config file |
| `--output-json` | boolean | `false` | Emit JSON output instead of human-readable text |

---

## Commands

### `plexium init` [Stable]

Scaffold a Plexium wiki in the current repository.

```bash
plexium init [flags]
```

Creates `.wiki/` (vault), `.plexium/` (state), `config.yml`, `manifest.json`, and `_schema.md`. Generates tech-stack-aware schema by detecting the project's languages.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--github-wiki` | boolean | `false` | Enable GitHub Wiki integration |
| `--obsidian` | boolean | `false` | Generate Obsidian vault configuration (`.obsidian/`) |
| `--strictness` | string | `moderate` | Strictness level: `strict`, `moderate`, or `advisory` |
| `--dry-run` | boolean | `false` | Preview without writing files |
| `--with-memento` | boolean | `false` | Initialize memento session tracking |
| `--with-beads` | boolean | `false` | Initialize beads task tracking |
| `--with-pageindex` | boolean | `false` | Initialize PageIndex retrieval |

**Examples:**

```bash
# Basic initialization
plexium init

# Full setup with integrations
plexium init --obsidian --with-memento --with-beads --with-pageindex --strictness strict

# Preview what would be created
plexium init --dry-run
```

Non-destructive on re-run: skips files that already exist.

---

### `plexium sync` [Stable]

Detect stale wiki pages and update the manifest after source changes.

```bash
plexium sync [flags]
```

Loads the manifest, compares stored source hashes against current file contents, updates hashes for stale pages, detects new source files not yet tracked, and recompiles navigation files.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | boolean | `false` | Preview stale pages without writing changes |

**Examples:**

```bash
# Run sync after editing source files
plexium sync

# Preview what sync would detect
plexium sync --dry-run
```

Idempotent: running sync twice after a source change produces 0 stale pages on the second run.

---

### `plexium convert` [Stable]

Bootstrap a wiki from an existing repository (brownfield ingestion).

```bash
plexium convert [flags]
```

Runs a five-phase pipeline: scour (source extraction), filter (include/exclude), ingest (page generation), link (cross-reference injection), and lint (gap analysis). Page quality depends on heuristic-based content extraction; review generated pages and edit as needed.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--depth` | string | `shallow` | Scour depth: `shallow` (file-level) or `deep` (AST-level) |
| `--dry-run` | boolean | `false` | Preview without writing to `.wiki/` |
| `--agent` | string | `""` | Run specified agent adapter after conversion |

**Examples:**

```bash
# Basic brownfield conversion
plexium convert

# Deep scan with dry-run preview
plexium convert --depth deep --dry-run

# Convert and run the Claude adapter
plexium convert --agent claude
```

---

### `plexium lint` [Stable]

Check wiki health using deterministic structural checks and optional LLM-augmented semantic analysis.

```bash
plexium lint [flags]
```

Six deterministic checks: links (broken `[[wiki-links]]`), orphans (no inbound links), staleness (source hash mismatch), manifest (schema validation), sidebar (structure), and frontmatter (required fields). The `--full` flag adds LLM-augmented checks (requires a configured provider).

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--deterministic` | boolean | `false` | Run deterministic checks only |
| `--full` | boolean | `false` | Run full lint including LLM-augmented semantic checks |
| `--ci` | boolean | `false` | CI mode: exit with non-zero code on lint errors or warnings |
| `--fail-on` | string | `error` | Exit non-zero on this severity: `error` or `warning` |

**Exit codes:**

| Code | Meaning |
|------|---------|
| 0 | Clean: no errors or warnings |
| 1 | Errors found |
| 2 | Warnings found (only in CI mode with `--fail-on warning`) |

**Examples:**

```bash
# Run deterministic checks
plexium lint --deterministic

# CI mode: fail on warnings
plexium lint --ci --fail-on warning

# Full lint with LLM checks
plexium lint --full
```

---

### `plexium compile` [Stable]

Regenerate shared navigation files from the current manifest state.

```bash
plexium compile [flags]
```

Reads `manifest.json`, groups pages by section, and generates `_index.md` (master page list) and `_Sidebar.md` (collapsible navigation). Deterministic: same manifest state always produces identical output.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | boolean | `false` | Preview without writing files |

**Examples:**

```bash
plexium compile
plexium compile --dry-run
```

---

### `plexium publish` [Stable]

Push wiki files to the GitHub Wiki remote.

```bash
plexium publish [flags]
```

Collects files from `.wiki/`, applies publish/exclude filters and sensitivity rules from config, clones the wiki repository, replaces content, and pushes. Respects `publish.preserveUnmanagedPages` and `sensitivity.neverPublish` config settings.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | boolean | `false` | Preview without pushing |

**Examples:**

```bash
plexium publish --dry-run
plexium publish
```

---

### `plexium gh-wiki-sync` [Stable]

Sync wiki to GitHub Wiki with manifest-aware filtering.

```bash
plexium gh-wiki-sync [flags]
```

Selective sync using `githubWiki.publish` and `githubWiki.exclude` patterns from config. Produces a report of synced and skipped files with reasons.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | boolean | `false` | Preview sync without writing |
| `--push` | boolean | `false` | Push changes to GitHub Wiki |

**Examples:**

```bash
# Preview what would sync
plexium gh-wiki-sync --dry-run

# Sync and push
plexium gh-wiki-sync --push
```

---

### `plexium retrieve` [Stable]

Query the wiki for information using PageIndex search with fallback.

```bash
plexium retrieve "<query>" [flags]
```

Searches the wiki using the built-in PageIndex engine with BM25-scored matching across titles, section headings, summaries, content, and wiki-links. Falls back to `_index.md` parsing and content grep when PageIndex returns no results. This command works immediately after `plexium init` with no additional setup.

The same search engine is available over MCP via [`plexium pageindex serve`](#plexium-pageindex-serve-stable). See [User Guide: Wiki Retrieval](user-guide.md#wiki-retrieval) for details on both interfaces.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--format` | string | `markdown` | Output format: `json` or `markdown` |

**Examples:**

```bash
plexium retrieve "authentication flow"
plexium retrieve "database schema" --format json
```

---

### `plexium doctor` [Stable]

Validate Plexium configuration and setup.

```bash
plexium doctor
```

Runs health checks: config file exists and parses, required directories present, manifest loadable, wiki root accessible, integration config valid. Reports pass/fail/warning/skip for each check.

No flags.

---

### `plexium setup` [Stable]

Canonical repo onboarding for Claude Code or Codex.

```bash
plexium setup <agent> [flags]
```

Runs idempotent repo setup for the selected agent: initializes Plexium if needed, compiles navigation, installs the built-in adapter, ensures the PageIndex reference file exists, optionally runs the native MCP command, and then verifies readiness.

Supports `claude` and `codex`.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--write-config` | boolean | `false` | Run the native MCP configuration command instead of only printing it |

**Examples:**

```bash
plexium setup claude
plexium setup claude --write-config
plexium setup codex
plexium setup codex --write-config
```

---

### `plexium verify` [Stable]

Agent-specific readiness verification for Plexium repositories.

```bash
plexium verify <agent>
```

Checks the general Plexium install plus agent-facing readiness: compiled navigation files, generated instruction file, PageIndex reference, deterministic lint status, and whether the agent MCP config is already present or still pending.

Supports `claude` and `codex`.

**Examples:**

```bash
plexium verify claude
plexium verify codex
```

---

### `plexium migrate` [Stable]

Apply schema migrations to the wiki.

```bash
plexium migrate [flags]
```

Reads the current schema version from `_schema.md`, discovers migration scripts in `.plexium/migrations/`, and applies pending migrations in order.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | boolean | `false` | Preview migrations without applying |
| `--version` | int | `0` | Target schema version (0 = latest) |

**Examples:**

```bash
plexium migrate --dry-run
plexium migrate --version 3
```

---

### `plexium hook pre-commit` [Stable]

Pre-commit hook entry point. Checks whether source file changes are accompanied by wiki updates.

```bash
plexium hook pre-commit
```

Behavior depends on the `enforcement.strictness` config value:
- **strict**: blocks the commit if wiki is not updated
- **moderate**: warns but allows the commit
- **advisory**: logs a notice only

No flags.

---

### `plexium hook post-commit` [Stable]

Post-commit hook entry point. Tracks documentation debt when commits bypass the pre-commit hook.

```bash
plexium hook post-commit
```

Detects `--no-verify` bypasses and records WIKI-DEBT entries for later resolution.

No flags.

---

### `plexium ci check` [Stable]

Diff-aware wiki validation for CI pipelines.

```bash
plexium ci check --base <SHA> --head <SHA> [flags]
```

Computes the file diff between two commits, identifies changed source files, and checks whether the wiki was updated to reflect those changes.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--base` | string | (required) | Base commit SHA |
| `--head` | string | (required) | Head commit SHA |
| `--output` | string | `""` | Output file path for JSON results |

**Examples:**

```bash
plexium ci check --base ${{ github.event.before }} --head ${{ github.sha }}
plexium ci check --base abc1234 --head def5678 --output results.json
```

---

### `plexium plugin add` [Stable]

Install a Plexium plugin adapter.

```bash
plexium plugin add <name> [flags]
```

Installs a bundled Plexium adapter by name, or installs a custom adapter from `--path`. Plexium validates `manifest.json`, copies the adapter into `.plexium/plugins/<name>/`, and runs `plugin.sh` to generate the target instruction file. Most users should prefer [`plexium setup`](#plexium-setup-stable), which wraps this command into the full onboarding flow.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--path` | string | `""` | Install plugin from a local path instead of the default plugins directory |

**Examples:**

```bash
plexium plugin add claude
plexium plugin add custom-adapter --path /path/to/plugin
```

---

### `plexium plugin list` [Stable]

List available Plexium plugins.

```bash
plexium plugin list
```

Lists bundled adapters and marks which ones are already installed in `.plexium/plugins/`.

No flags.

---

### `plexium beads link` [Stable]

Link a task ID to a wiki page.

```bash
plexium beads link <task-id> <wiki-path>
```

Adds the task ID to the page's YAML frontmatter (`beads-ids` field). Idempotent: linking the same task twice has no effect.

---

### `plexium beads unlink` [Stable]

Remove a task-to-page link.

```bash
plexium beads unlink <task-id> <wiki-path>
```

---

### `plexium beads pages` [Stable]

List wiki pages linked to a task.

```bash
plexium beads pages <task-id>
```

Scans all wiki pages for the given task ID in frontmatter.

---

### `plexium beads tasks` [Stable]

List task IDs linked to a wiki page.

```bash
plexium beads tasks <wiki-path>
```

Reads the page's frontmatter `beads-ids` field.

---

### `plexium beads scan` [Stable]

Build the complete task-page mapping.

```bash
plexium beads scan
```

Scans all wiki pages and returns every task-to-page link found.

---

### `plexium pageindex serve` [Stable]

Start a PageIndex MCP server for agent-accessible wiki search.

```bash
plexium pageindex serve
```

Runs in stdio mode using JSON-RPC 2.0. Exposes the same search engine used by [`plexium retrieve`](#plexium-retrieve-stable) over the Model Context Protocol. Agents connect via MCP and gain access to three tools: `pageindex_search`, `pageindex_get_page`, and `pageindex_list_pages`.

Use [`plexium pageindex connect`](#plexium-pageindex-connect-stable) for agent-specific setup guidance.

No flags.

---

### `plexium pageindex connect` [Stable]

Show or apply the native MCP setup command for Claude Code or Codex.

```bash
plexium pageindex connect <agent> [flags]
```

Supports `claude` and `codex`.

For the canonical onboarding flow, prefer [`plexium setup`](#plexium-setup-stable). Use `pageindex connect` when you only want the native MCP command without the rest of the setup steps.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--write-config` | boolean | `false` | Run the native `claude mcp add ...` or `codex mcp add ...` command instead of only printing it |

**Examples:**

```bash
plexium pageindex connect claude
plexium pageindex connect claude --write-config
plexium pageindex connect codex
plexium pageindex connect codex --write-config
```

---

### `plexium agent status` [Stable]

Show provider cascade health and cost tracking.

```bash
plexium agent status
```

Displays each configured provider's health, daily request counts, spend, and rate limit state from `.plexium/agent-state.json`.

---

### `plexium agent test` [Stable]

Test provider connectivity.

```bash
plexium agent test [flags]
```

Sends "Respond with: OK" to each provider (or a specific one) and reports latency, tokens, and cost.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--provider` | string | `""` | Test a specific provider instead of the full cascade |

---

### `plexium agent spend` [Stable]

Show daily spend per provider.

```bash
plexium agent spend
```

Loads state from `.plexium/agent-state.json` and compares spend against the configured daily budget.

---

### `plexium agent benchmark` [Stable]

Benchmark provider latency.

```bash
plexium agent benchmark
```

Runs 3 rounds of "Respond with: OK" per provider and reports average latency.

---

### `plexium bootstrap` [Stub]

This command exists in the CLI but prints a placeholder message and performs no work.

```bash
plexium bootstrap
# Output: " plexium bootstrap"
```

---

### `plexium agent start` [Stub]

Prints a message. Does not start a background process.

```bash
plexium agent start
```

---

### `plexium agent stop` [Stub]

Prints a message. Does not stop anything.

```bash
plexium agent stop
```

---

### `plexium daemon` [Experimental]

Run an autonomous wiki maintenance loop.

```bash
plexium daemon [flags]
```

Polls for staleness, lint violations, ingest opportunities, and documentation debt. Creates isolated worktrees for concurrent work. The LinearTracker (issue creation) returns `ErrNotImplemented` for all operations. The runner dispatches to external CLI tools.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--poll-interval` | int | `300` | Poll interval in seconds |
| `--max-concurrent` | int | `2` | Maximum concurrent worktrees |

---

### `plexium orchestrate` [Experimental]

Run a single orchestrated wiki-update for an issue.

```bash
plexium orchestrate --issue <ID>
```

Creates an isolated git worktree, runs retriever and documenter agent roles, then cleans up. The default runner is noop (no actual agent execution).

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--issue` | string | (required) | Issue ID to orchestrate |

---

## Environment Variables

These override corresponding config file values.

| Variable | Config Key | Description |
|----------|------------|-------------|
| `PLEXIUM_WIKI_ROOT` | `wiki.root` | Wiki directory path |
| `PLEXIUM_WIKI_HOME` | `wiki.home` | Home page filename |
| `PLEXIUM_WIKI_SIDEBAR` | `wiki.sidebar` | Sidebar filename |
| `PLEXIUM_WIKI_FOOTER` | `wiki.footer` | Footer filename |
| `PLEXIUM_WIKI_LOG` | `wiki.log` | Change log filename |
| `PLEXIUM_WIKI_INDEX` | `wiki.index` | Index filename |
| `PLEXIUM_WIKI_SCHEMA` | `wiki.schema` | Schema filename |
| `PLEXIUM_REPO_DEFAULT_BRANCH` | `repo.defaultBranch` | Git default branch |
| `PLEXIUM_REPO_WIKI_ENABLED` | `repo.wikiEnabled` | Enable GitHub Wiki |
| `PLEXIUM_SOURCES_INCLUDE` | `sources.include` | Source file include globs |
| `PLEXIUM_SOURCES_EXCLUDE` | `sources.exclude` | Source file exclude globs |
| `PLEXIUM_AGENTS_STRICTNESS` | `agents.strictness` | Agent strictness level |
| `PLEXIUM_SYNC_MODE` | `sync.mode` | Sync mode (incremental or full) |
| `PLEXIUM_ENFORCEMENT_STRICTNESS` | `enforcement.strictness` | Hook/CI strictness level |
| `PLEXIUM_GITHUB_WIKI_ENABLED` | `githubWiki.enabled` | Enable GitHub Wiki publishing |
