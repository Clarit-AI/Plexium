# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project: Plexium

Self-documenting repository system. Applies Karpathy's LLM Wiki pattern to agentic coding workflows — transforms repos into systems where LLM agents incrementally build and maintain a persistent, interlinked wiki that compounds with every commit.

**Current state:** Greenfield. Specs finalized and decomposed into executable phase documents. Build begins at Phase 0 (Project Setup).

---

## Build Guide

**Primary build guide:** `docs/phases/OVERVIEW.md`
- Dependency graph, phase status tracker, prerequisite decisions
- Start here to understand what to build next

**Architecture reference:** `docs/architecture/core-architecture.md`
- Layers, vault structure, schema, ownership model, manifest format, config schema, invariants
- Consult specific sections as needed; not cover-to-cover reading

**Archived specification:** `docs/reference/plexium-spec-full.md`
- The original monolithic spec (1963 lines)
- Reference only; implementation follows the phase docs

**Phase documents:** `docs/phases/phase-{N}-{name}.md`
- Phase 0: Project Setup
- Phase 1-10: Milestones 1-10 (one doc per milestone)

---

## Build Tooling

This project uses **bd (beads)** and **memento** as build tools from day 1.

### bd (Beads) — Task Management

Track all milestone work as bd epics:

```bash
bd stats           # See all milestone epics and status
bd ready           # See next actionable tasks
bd dolt push       # Push beads data to remote (REQUIRED before git push)
```

**Important:** Run `bd dolt push` BEFORE `git push` to sync issue data to remote.

**Epics:** `plexium-p0` (Phase 0) through `plexium-m10` (Milestone 10)

### memento — Session Provenance

Use `git memento commit <session-id>` instead of `git commit` to capture session context on commits.

**When to use:**
- End of each phase completion
- Branched off functions or components
- Troubleshooting sessions
- Any significant implementation milestone

```bash
git memento commit <session-id> -m "commit message"
```

---

## Spec Documents

| File | Purpose |
|------|---------|
| `docs/phases/OVERVIEW.md` | **Primary build guide.** Orchestration spine with dependency graph, status tracker, prerequisite decisions. |
| `docs/architecture/core-architecture.md` | **Architecture reference.** Extracted invariant context: layers, vault, schema, ownership, manifest, config, invariants. |
| `docs/phases/phase-0-project-setup.md` | Project bootstrap. Toolchain choice, bd init, memento init, CI skeleton. |
| `docs/phases/phase-1-cli-foundation.md` | Milestone 1. CLI skeleton, config loader, scanner, normalizer, templates. |
| `docs/phases/phase-2-page-generation.md` | Milestone 2. Taxonomy, generators, nav files. |
| `docs/phases/phase-3-state-publishing.md` | Milestone 3. Manifest, publish, init, dry-run. |
| `docs/phases/phase-4-convert.md` | Milestone 4. Brownfield ingestion pipeline. |
| `docs/phases/phase-5-agent-adapters.md` | Milestone 5. Plugin arch, agent adapters. |
| `docs/phases/phase-6-lint.md` | Milestone 6. Deterministic lint pipeline. |
| `docs/phases/phase-7-reporting-obsidian.md` | Milestone 7. Reports, obsidian config, gh-wiki-sync. |
| `docs/phases/phase-8-enforcement.md` | Milestone 8. Hooks, CI workflows, WIKI-DEBT, migrate. |
| `docs/phases/phase-9-tool-integrations.md` | Milestone 9. Memento, beads, PageIndex, retrieval. |
| `docs/phases/phase-10-orchestration.md` | Milestone 10. Assistive agent, daemon, compile, workspaces. |
| `docs/reference/plexium-spec-full.md` | **Archived.** Original 1963-line spec. Reference only. |
| `docs/reference/symphony-integration-ref.md` | Symphony integration analysis. |
| `docs/reference/local-agent-ref.md` | On-device assistive agent analysis. |
| `docs/reference/cloud-fallback-ref.md` | Cloud provider cascade analysis. |
| `docs/reference/llm-wiki-ref.md` | Karpathy's LLM Wiki pattern reference. |
| `docs/reference/openai-symphony-ref.md` | OpenAI Symphony spec reference. |

The `docs/reference/` files are **voluntary reading** — consult them for deep context but follow the phase docs for execution.

---

## Three-Layer Architecture

```
Source Layer (immutable)     → src/**, docs/**, README, ADRs
         ↕
State Manifest               → .plexium/manifest.json (bidirectional mapping, hashes, ownership)
         ↕
Wiki Layer (.wiki/)           → _schema.md, _index.md, modules/, decisions/, concepts/
         ↕
Control Layer                → _schema.md, .plexium/config.yml, lefthook.yml, .github/workflows/
         ↕
Enforcement                  → Schema (soft) → Git hooks (medium) → CI/CD (hard) → Memento gate
         ↕
Execution Plane (opt-in)      → WORKFLOW.md, daemon, workspace mgr, runner/tracker adapters
```

- **Source layer** is never modified by wiki operations
- **Wiki layer** is LLM-maintained (agents own it, humans review it)
- **Control layer** governs agent behavior and enforces discipline
- Deterministic checks always run before LLM calls — no API costs for structural validation

---

## Build Order (11 Phases)

| Phase | Milestone | Focus |
|-------|-----------|-------|
| 0 | Project Setup | Repo init, toolchain, bd, memento, CI skeleton |
| 1 | CLI Foundation | Command routing, config loader, scanner, normalizer, templates |
| 2 | Page Generation | Taxonomy classifier, generators, nav files |
| 3 | State & Publishing | Manifest, hash computation, publish, init, dry-run |
| 4 | Convert (Brownfield) | Directory traversal, scour/ingest/link/lint phases |
| 5 | Agent Adapters | Plugin architecture, schema generation, 4 adapters |
| 6 | Deterministic Lint | Link/orphan/staleness detection, validators |
| 7 | Reporting & Obsidian | Bootstrap/sync/lint reports, obsidian config |
| 8 | Enforcement | Lefthook hooks, CI workflows, WIKI-DEBT, migrate |
| 9 | Tool Integrations | Memento, beads, PageIndex, retrieval agent |
| 10 | Orchestration | Assistive agent, daemon, compile, workspaces |

---

## CLI Command Reference

```
plexium init [--github-wiki] [--obsidian]  # Scaffold .wiki/, .plexium/, generate schema
plexium convert                             # Brownfield: bootstrap wiki from existing repo
plexium sync [--dry-run]                    # Incremental update after source changes
plexium lint [--deterministic|--full] [--ci]  # Health check (structural + optional semantic)
plexium publish                             # Push wiki to GitHub Wiki submodule
plexium retrieve "<query>"                  # Query wiki via PageIndex or fallback
plexium doctor                              # Validate config, auth, wiki integrity
plexium report --format markdown|json       # Emit structured report
plexium hook pre-commit|post-commit         # Git hook entry points
plexium ci check --base SHA --head SHA      # CI diff-aware wiki check
plexium migrate                             # Schema version migration
plexium gh-wiki-sync --push                 # GitHub Wiki submodule sync
plexium daemon [options]                    # Autonomous wiki maintenance loop
plexium compile                             # Regenerate shared navigation files from page state
plexium agent <subcommand>                  # Manage assistive agent (start/stop/status/test/spend)
plexium orchestrate --issue <ID>           # Run single orchestrated wiki-update for issue
```

---

## Key Configuration Files

- `.plexium/config.yml` — sources include/exclude globs, strictness level, publish/exclude rules, agent config, assistive agent providers
- `.plexium/manifest.json` — bidirectional source↔wiki mapping with hashes, ownership, timestamps
- `.wiki/_schema.md` — the constitution governing how agents maintain the wiki
- `.plexium/WORKFLOW.md` — orchestration/execution contract for daemon mode
- `lefthook.yml` — pre-commit/post-commit hooks for wiki enforcement

---

## Invariants

- Wiki operations never modify source code
- Pages with `ownership: human-authored` are never overwritten
- Partial failures roll back cleanly — no half-updated wikis
- All `[[wiki-links]]` validated before commit
- Sync is idempotent (same commit → identical output)

---

## Skills

Repo skills in `.claude/skills/` provide domain knowledge for development:

- **`lefthook/`** — Git hooks manager used for Plexium's enforcement layer (Phase 8)
- **`git-submodule/`** — GitHub Wiki integration via submodule
- **`plugin-dev/`** — 7 skills for building Claude Code plugins (relevant for agent adapters, Phase 5)
- **`mcp-server-dev/`** — 3 skills for MCP server development (relevant for PageIndex integration, Phase 9)
- **`claude-code-dev/`** — Working with Claude Code and developing plugins
- **`codex-cli-*`** — OpenAI Codex CLI configuration, hooks, plugins
- **`github-*`** — Multi-repo, release management, workflow automations
- **`hooks-automation/`** — Hook patterns and automation

---

## Codebase Exploration

Always use codebase_search for codebase exploration. Do not call it in parallel with other tools.
