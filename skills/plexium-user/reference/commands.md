# CLI Command Reference

Full command reference for agents working in Plexium repositories.

## Core Commands

| Command | Purpose | When to use |
|---------|---------|-------------|
| `plexium retrieve "<query>"` | Search wiki | Before starting any task — gather context |
| `plexium lint --deterministic` | Structural health check | Before committing — verify links, orphans, staleness |
| `plexium lint --full` | Structural + semantic analysis | When assistive agent is configured — finds contradictions, missing cross-refs |
| `plexium compile` | Regenerate `_index.md`, `_Sidebar.md` | After adding or removing wiki pages |
| `plexium sync` | Update manifest, detect stale pages | After source file changes |
| `plexium doctor` | Validate setup | When something seems wrong |

## Publishing

| Command | Purpose |
|---------|---------|
| `plexium publish` | Push wiki to GitHub Wiki |
| `plexium publish --dry-run` | Preview what would be published |
| `plexium gh-wiki-sync --push` | Selective sync with include/exclude patterns |

## Setup

| Command | Purpose |
|---------|---------|
| `plexium init` | Scaffold `.wiki/`, `.plexium/`, config |
| `plexium setup <agent>` | Canonical repo onboarding for Claude or Codex |
| `plexium verify <agent>` | Verify that Plexium is ready for Claude or Codex |
| `plexium convert` | Bootstrap wiki from existing source code |
| `plexium migrate` | Apply schema version migrations |
| `plexium agent setup` | Provider configuration via OAuth, env-var fallback, key file, or stdin |

## Secret Safety

- Never paste API keys or secrets into chat.
- Prefer terminal-native setup such as `export OPENROUTER_API_KEY=...` followed by `plexium agent setup`.
- In repos using memento, pasted secrets can end up in session notes. If that happens, rewind the session if possible and do not commit that session to memento.

## Agent Management

| Command | Purpose |
|---------|---------|
| `plexium agent start` | Launch daemon in background |
| `plexium agent stop` | Stop background daemon |
| `plexium agent status` | Show provider health and daemon state |
| `plexium agent test` | Test provider connectivity |
| `plexium agent spend` | Show daily cost breakdown |

## CI

| Command | Purpose |
|---------|---------|
| `plexium ci check --base SHA --head SHA` | Diff-aware wiki check for PRs |
| `plexium lint --deterministic --ci` | Exit non-zero on lint errors |

## Output Formats

Most commands accept `--output-json` for structured output. `plexium retrieve` accepts `--format json`.
