<p align="center">
  <img src="assets/logo-banner.png" alt="Plexium" width="600" />
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache_2.0-blue.svg" alt="License: Apache-2.0" /></a>
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go-1.25+-00ADD8.svg" alt="Go 1.25+" /></a>
  <a href="docs/status.md"><img src="https://img.shields.io/badge/Tests-540%2B_passing-brightgreen.svg" alt="Tests: 540+" /></a>
</p>

<p align="center">
  Plexium gives your repository a persistent, agent-maintained wiki that compounds with every commit.
</p>

---

## The Problem

LLM coding agents have no memory. Every session starts cold: the agent scans the repo, rebuilds its understanding through RAG, writes code, and then discards everything it learned. The next session repeats the same discovery from scratch.

This means:
- **No compounding knowledge.** Insights from session 47 are invisible to session 48.
- **Redundant context building.** Agents re-parse the same files and re-derive the same architectural understanding on every invocation.
- **No shared understanding.** Multiple agents working on the same repo have no common knowledge surface. Each builds its own ephemeral mental model.
- **Documentation debt accumulates silently.** Code changes outpace documentation because nothing enforces the connection.

RAG retrieval helps agents find relevant code, but it does not accumulate understanding. It is a search mechanism, not a knowledge layer.

---

## How Plexium Works

Plexium adds a synthesized knowledge layer to your repository. Source files are the ground truth (immutable). The `.wiki/` vault is the knowledge surface (agent-maintained). A state manifest (`.plexium/manifest.json`) tracks bidirectional mappings between source files and wiki pages, with content hashes for staleness detection and ownership metadata to prevent conflicts.

Agents read the wiki before working on a task, gaining accumulated project context. After making changes, agents update the relevant wiki pages. Git hooks validate that wiki updates accompany source changes. CI pipelines enforce this across the team.

The wiki is browsable as an [Obsidian](https://obsidian.md) vault, publishable as a [GitHub Wiki](https://docs.github.com/en/communities/documenting-your-project-with-wikis), and queryable via [MCP](https://modelcontextprotocol.io) by any coding agent. A governance schema (`_schema.md`) instructs agents on the read-execute-document-validate loop, ownership rules, and page generation conventions.

---

## Proof

Plexium has completed all 11 build phases and passed a comprehensive validation suite.

| Metric | Value |
|--------|-------|
| Test functions | 540+ across 25 packages |
| Safety invariants proven | 7 (source immutability, dry-run isolation, ownership protection, init non-destructiveness, compile scope, manifest preservation) |
| Determinism guarantees | 6 (manifest sort stability, hash consistency, compile idempotency, lint stability, empty-manifest stability, JSON shape stability) |
| Cross-phase contracts | 10 verified (struct fields, ownership values, exit codes, config validation) |
| CLI commands | 22 commands and subcommands |
| Go packages | 26 |
| Blocking issues | 0 |

Full details: [Implementation Status](docs/status.md)

---

## Quick Start

```bash
# Install
go install github.com/Clarit-AI/Plexium/cmd/plexium@latest

# Initialize in your repo
cd /path/to/your/repo
plexium init

# Validate the setup
plexium doctor

# Run structural lint
plexium lint --deterministic

# Generate navigation
plexium compile
```

For a complete walkthrough, see the [Getting Started](docs/getting-started.md) guide.

---

## Details

### Architecture

```
Source Layer (immutable)
  src/**, docs/**, README, ADRs
        |
State Manifest (.plexium/manifest.json)
  Bidirectional source-to-wiki mapping, content hashes, ownership
        |
Wiki Layer (.wiki/)
  _schema.md, _index.md, modules/, decisions/, concepts/
        |
Enforcement
  _schema.md (soft) -> Git hooks (medium) -> CI/CD (hard)
```

- The source layer is never modified by wiki operations.
- The wiki layer is agent-maintained: agents own it, humans review it.
- Enforcement escalates from schema guidance through hooks to CI checks.

### Vault Structure

```
.wiki/
  Home.md                 # Landing page
  _schema.md              # Agent governance schema
  _index.md               # Auto-generated page index
  _Sidebar.md             # Auto-generated navigation
  _log.md                 # Change log
  architecture/           # System architecture pages
  modules/                # Module documentation
  decisions/              # Architecture Decision Records
  patterns/               # Design patterns
  concepts/               # Domain concepts
  guides/                 # How-to guides
  raw/                    # Unprocessed sources (meeting notes, transcripts)
```

### Retrieval

Plexium includes a built-in search engine queryable via CLI (`plexium retrieve "query"`) or MCP (`plexium pageindex serve`). The engine uses BM25-style scoring across page titles, sections, summaries, content, and wiki-links. CLI retrieval works immediately after `plexium init`. The MCP server exposes the same engine over JSON-RPC 2.0 stdio for agents that support the Model Context Protocol.

### Ownership Model

| Mode | Meaning |
|------|---------|
| `managed` | Agent-regenerated from source. Human edits are overwritten on sync. |
| `human-authored` | Locked from automated changes. Agents cannot overwrite. |
| `co-maintained` | Both agents and humans edit. Agents append, do not rewrite. |

### Key Commands

| Command | Purpose |
|---------|---------|
| `plexium init` | Scaffold wiki and config |
| `plexium sync` | Detect stale pages, update manifest |
| `plexium convert` | Bootstrap wiki from existing repo |
| `plexium lint` | Structural and semantic health checks |
| `plexium compile` | Regenerate navigation files |
| `plexium publish` | Push wiki to GitHub Wiki |
| `plexium doctor` | Validate setup and config |
| `plexium retrieve` | Query the wiki (built-in search engine) |

Full reference: [CLI Reference](docs/cli-reference.md)

### Assistive Agent

Plexium includes an assistive agent that automates wiki maintenance using a provider cascade. The cascade tries providers in cost order (cheapest first), falling through on failure:

- **Ollama** -- local LLM via llama.cpp, zero cost, handles low-complexity tasks (frontmatter updates, index regeneration, log entries)
- **OpenRouter / OpenAI-compatible** -- remote API with daily budget caps, handles medium-complexity tasks (module summaries, cross-reference suggestions)
- **Inherit** -- delegates to the primary coding agent for high-complexity tasks (architecture synthesis, contradiction detection)

A **task router** classifies each wiki maintenance task by complexity and routes it to the appropriate cascade tier. A **rate limiter** tracks daily spend per provider and applies adaptive batching delays as usage approaches the budget cap.

The **daemon** (`plexium daemon` or `plexium agent start`) runs a background poll loop that detects stale pages, lint issues, ingest candidates, and wiki-debt, then takes the configured action (log, create issue, or auto-fix in an isolated worktree).

```yaml
# .plexium/config.yml
assistiveAgent:
  enabled: true
  providers:
    - name: local-ollama
      enabled: true
      type: ollama
      endpoint: http://localhost:11434
      model: llama3
    - name: openrouter
      enabled: true
      type: openai-compatible
      endpoint: https://openrouter.ai/api
      model: meta-llama/llama-3.1-8b-instruct:free
      apiKeyEnv: OPENROUTER_API_KEY
  budget:
    dailyUSD: 1.00

daemon:
  enabled: true
  runner: claude
  tracker: github
  pollInterval: 300
  watches:
    staleness:
      enabled: true
      action: create-issue
      threshold: "7d"
```

| Command | Purpose |
|---------|---------|
| `plexium agent start` | Launch daemon in background |
| `plexium agent stop` | Stop background daemon |
| `plexium agent status` | Show provider health, daemon state, daily spend |
| `plexium agent test` | Test provider connectivity |
| `plexium agent spend` | Show daily cost breakdown |
| `plexium daemon` | Run daemon in foreground |

---

## Inspirations

Plexium synthesizes ideas from four projects:

**[LLM-Wiki](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f)** by Andrej Karpathy. The conceptual origin. Karpathy proposed that LLM coding agents should maintain a wiki inside the repository they work on, building a persistent knowledge layer that compounds over time instead of relying on stateless RAG. Plexium implements this idea as a complete system with enforcement, deterministic validation, and multi-agent coordination.

**[Symphony](https://github.com/openai/symphony/blob/main/SPEC.md)** by OpenAI. The multi-agent orchestration pattern. Symphony's approach to task decomposition, agent roles, and workspace isolation informed Plexium's daemon, orchestrate command, and role-based capability model (coder, retriever, documenter, ingestor).

**[Memento](https://github.com/mandel-macaque/memento)** by Manuel de la Pena. Git-native session provenance. Memento captures coding session context as git notes, creating an audit trail for every commit. Plexium integrates memento as both a build tool (session tracking during development) and a feature (transcript ingestion for decision extraction).

**[PageIndex](https://github.com/VectifyAI/PageIndex)** by VectifyAI. Hierarchical document retrieval. PageIndex's approach to structured document search informed Plexium's `retrieve` command and the PageIndex MCP server, giving agents a queryable index of the wiki with BM25-scored relevance ranking and fallback strategies.

---

## Ecosystem

Plexium is part of the [Clarit.AI](https://github.com/Clarit-AI) open-source ecosystem. Plexium solves agent amnesia at the repository knowledge layer: it gives agents a persistent, shared understanding of the codebase that compounds across sessions. [Engram](https://github.com/Clarit-AI/Engram) solves it at the model inference layer with persistent memory across conversations. [Synapse](https://github.com/Clarit-AI/Synapse) solves it at the hardware compute layer with hybrid NPU/CPU routing for edge devices.

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for build instructions, testing, and PR workflow.

---

## License and Acknowledgements

[Apache License 2.0](LICENSE)

Copyright 2026 Clarit.AI

### Upstream Projects

- [Andrej Karpathy's LLM-Wiki](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f) (conceptual origin)
- [OpenAI Symphony](https://github.com/openai/symphony) (orchestration patterns)
- [Memento](https://github.com/mandel-macaque/memento) (session provenance)
- [PageIndex](https://github.com/VectifyAI/PageIndex) (hierarchical retrieval)
- [cobra](https://github.com/spf13/cobra) (CLI framework)
- [viper](https://github.com/spf13/viper) (configuration)
