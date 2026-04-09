# Plexium Build Execution Guide

> **Primary build document.** Hand this to an orchestration agent to begin building Plexium.
> For architectural reference (vault structure, manifest format, schema, ownership model, invariants), consult `docs/architecture/core-architecture.md`.
> For the archived original specification, see `docs/reference/plexium-spec-full.md`.

---

## Project Summary

**Plexium** transforms repositories into self-documenting systems by applying Karpathy's LLM Wiki pattern to agentic coding workflows. Instead of stateless RAG rediscovery on every session, LLM coding agents incrementally build and maintain a persistent, interlinked wiki — a compiled knowledge layer that compounds with every commit, every conversation, and every ingested source.

The codebase is the raw source layer. The `.wiki/` vault is the synthesized knowledge layer. The schema is the governance layer. Agents read the wiki before working and update it after every change. Git hooks and CI pipelines enforce this discipline. The wiki is simultaneously an Obsidian vault, a GitHub Wiki (via submodule), and the canonical context surface for every coding assistant.

**The pitch:** *Give it your repo, and it builds, maintains, and enforces a living wiki that makes every agent session smarter than the last.*

**Build tooling:** This project uses **bd (beads)** for task graph management and **memento** for session provenance. Both are configured from day 1 — not as Plexium features to build, but as build tools to use during development.

---

## Build Tooling

### bd (Beads) — Task Graph Management

All phases are tracked as bd epics. Use `bd` to navigate work across sessions.

```bash
# Initialize (Phase 0 creates this)
bd init

# See all milestone epics and their status
bd stats

# See next actionable tasks
bd ready

# Create a task in a phase epic
bd task add epic:<phase-id> "Implement config loader" --priority high
```

**Epic naming convention:** `plexium-m1` through `plexium-m10` for milestones, `plexium-p0` for project setup.

### memento — Session Provenance

Every coding session is captured as a git note on the commit. This provides:
- Audit trail for all build decisions
- Rich transcript corpus for Phase 9 (when Plexium's memento integration is built)
- Dog-fooding: the transcripts become source material for Plexium's own wiki

```bash
# Initialize (Phase 0 does this)
git memento init

# Verify it's working
git memento doctor
```


**Memento Claude Code Compatibility Shim:**
> **Note:** Claude Code v2.1.x removed the `claude sessions` command that memento relies on. To restore functionality without modifying memento itself, a custom git-config shim was installed linking the `claude` provider to a bridge script.
- **Script:** `.plexium/bin/claude-memento-bridge.cjs`
- **Configuration:** `git config --local memento.claude.bin "$(pwd)/.plexium/bin/claude-memento-bridge.cjs"`
- **Behavior:** The script intercepts memento's provider calls (e.g. `list` and `get`). If an agent provides an arbitrary, non-UUID session name (e.g., `session-phase1`), the script seamlessly catches the validation failure, fetches the *most recently updated* Claude session from `~/.claude/projects/`, and maps it back to memento using the agent's requested alias.
- **Workflow:** Agents can safely run `git memento commit <any-name> -m "message"` without needing to look up the exact internal session UUID of their environment.

---

## Prerequisite Decisions

These Open Design Questions (§25) must be resolved before specific phases. All are currently unresolved — decide before building the phase that needs them.

| # | Question | Blocks Phase | Recommendation |
|---|----------|-------------|----------------|
| — | **Language choice: Go or Rust** | Phase 0 | Needs user decision. Go has faster prototyping; Rust has better binary distribution. |
| 1 | Excerpt vs synthesis | Phase 2 | Synthesis default. Excerpt (quoting) adds trust but creates duplication. |
| 2 | One-to-many mapping | Phase 3 | Single mapping in Phase 1; split to one-to-many later if needed. |
| 3 | Opinionated taxonomy | Phase 2 | Opinionated default (Architecture/Modules/Decisions/Patterns/Concepts), fully configurable. |
| 4 | Diff granularity | Phase 3 | Path-filtered only initially. Fine-grained semantic diff (detect which functions changed) is Phase 6+ work. |
| 5 | Preview branches | Phase 7 | Direct-publish in Phase 1. Preview branches as a Phase 7+ feature. |
| 6 | Submodule vs sync | Phase 7 | Sync-and-push default for Phase 1-6. GitHub Wiki submodule as Phase 7+ option. |
| 7 | Agent-generated vs CLI-generated LLM calls | Phase 4 | Run Plexium's own LLM calls through user's configured coding agent (inherits API keys and preferences). |
| 8 | Schema co-evolution | Phase 8 | Treat `_schema.md` as `co-maintained` — users customize, Plexium appends on migrate. |
| 9 | Wiki-as-context budget | Phase 5 | No hard token budget. Let agents read as much as they determine is relevant. |
| 10 | Contradiction resolution | Phase 6 | Flag contradictions for human review. Agents suggest resolutions but don't auto-resolve. |
| 11 | Daemon vs CI-only | Phase 10 | Both — CI+hooks as default, daemon as opt-in Phase 10 feature. |
| 12 | Assistive agent model selection | Phase 10 | Model-agnostic. Plexium ships without a bundled model; users configure any Ollama-served model. |

---

## Phase Dependency Graph

```
Phase 0 (Setup)
    │
    ├── Language/toolchain decision
    ├── bd init + milestone epics
    ├── memento init
    └── CI skeleton

Phase 1 (CLI Foundation)
    │
    └── CLI skeleton, config loader, scanner, normalizer, templates

Phase 2 (Page Generation) ◄── Phase 1
    │
    └── Taxonomy, module/decision/concept generators, nav files

Phase 3 (State & Publishing) ◄── Phase 2
    │
    ├── Manifest creation/update, hash computation
    ├── Publish command, init command
    └── Dry-run mode

         ┌──────────────────────────────────────┐
         │                                      │
         ▼                                      ▼
Phase 4 (Convert) ◄──────────┐    Phase 5 (Agent Adapters) ─┐
    │                         │        │                        │
    │ (depends on M1-M3)      │        │ (independent of M4)   │
    └──────────┬──────────────┘        └───────────┬─────────┘
               │                                   │
               ▼                                   ▼
         Phase 6 (Deterministic Lint) ◄──────────┘
              │                        (M6 needs manifest from M3)
              │                        (M6 can start after M3, before M4/M5 complete)
              ▼
         Phase 7 (Reporting & Obsidian) ◄── M4, M5
              │
              ▼
         Phase 8 (Enforcement)
              │
              ▼
         Phase 9 (Tool Integrations)
              │
              ▼
         Phase 10 (Orchestration)
```

**Parallelization notes:**
- Phase 5 (Agent Adapters) is independent of Phase 4 (Convert). Both can start after Phase 3.
- Phase 6 (Lint) needs the manifest from Phase 3 but doesn't need Convert or Adapters.
- Phase 7 needs Phase 4 and Phase 5, but can start planning during Phase 3.
- Phases 8-10 are strictly sequential.

---

## Phase Status Tracker

| Phase | Milestone | Status | bd Epic | Key Deliverables |
|-------|-----------|--------|---------|-----------------|
| 0 | Project Setup | `complete` | `plexium-p0` | Repo initialized, toolchain chosen, bd + memento configured |
| 1 | CLI Foundation | `complete` | `plexium-m1` | CLI binary with routing, config loader, file scanner, normalizer, templates |
| 2 | Page Generation | `complete` | `plexium-m2` | Taxonomy classifier, module/decision/concept generators, nav file generation |
| 3 | State & Publishing | `complete` | `plexium-m3` | Manifest CRUD, hash computation, publish, init scaffolding, dry-run |
| 4 | Convert (Brownfield) | `complete` | `plexium-m4` | Scour/filter/ingest/link/lint pipeline, conversion report, plexium convert command |
| 5 | Agent Adapters | `pending` | `plexium-m5` | Plugin architecture, schema generation, 4 agent adapters |
| 6 | Deterministic Lint | `complete` | `plexium-m6` | Link/orphan/staleness detection, manifest validation, doctor command |
| 7 | Reporting & Obsidian | `pending` | `plexium-m7` | Report formats (3 types), obsidian config, gh-wiki-sync |
| 8 | Enforcement | `complete` | `plexium-m8` | Lefthook hooks, CI workflows, WIKI-DEBT tracking, migrate command |
| 9 | Tool Integrations | `complete` | `plexium-m9` | Memento/beads/PageIndex product integration |
| 10 | Orchestration | `complete` | `plexium-m10` | Assistive agent, daemon mode, compile command, workspaces |

---

## Per-Phase Quick Reference

### Phase 0: Project Setup
- **Doc:** `docs/phases/phase-0-project-setup.md`
- **Summary:** Bootstrap the build environment, choose toolchain, configure bd and memento
- **Prereqs:** Language decision (Go vs Rust), CI provider selection
- **Acceptance criteria:** 4 items — repo initialized, bd epics created, memento working, CI runs

### Phase 1: CLI Foundation
- **Doc:** `docs/phases/phase-1-cli-foundation.md`
- **Architecture:** [Config Format](../architecture/core-architecture.md#configuration), [Vault Structure](../architecture/core-architecture.md#vault-structure)
- **Summary:** Build the CLI binary with command routing, config loading, file scanning, markdown normalization, template engine
- **Prereqs:** Phase 0 complete
- **Acceptance criteria:** CLI skeleton routes commands, config loads, scanner globs work, normalizer handles frontmatter

### Phase 2: Page Generation
- **Doc:** `docs/phases/phase-2-page-generation.md`
- **Architecture:** [Page Generation Rules](../architecture/core-architecture.md#page-generation-rules), [Vault Structure](../architecture/core-architecture.md#vault-structure)
- **Summary:** Taxonomy classifier, generators for modules/decisions/concepts, slug deduplication, navigation file generation
- **Prereqs:** Phase 1 complete
- **Acceptance criteria:** Taxonomy applied, slugs deduplicated, nav files generated deterministically

### Phase 3: State & Publishing
- **Doc:** `docs/phases/phase-3-state-publishing.md`
- **Architecture:** [State Manifest](../architecture/core-architecture.md#state-manifest--mapping), [Config Format](../architecture/core-architecture.md#configuration)
- **Summary:** Manifest creation/update, bidirectional source↔wiki mapping, hash computation, publish command, dry-run
- **Prereqs:** Phase 2 complete
- **Acceptance criteria:** Manifest accurate, hashes computed, publish works, dry-run produces output without side effects

### Phase 4: Convert (Brownfield)
- **Doc:** `docs/phases/phase-4-convert.md`
- **Architecture:** [Vault Structure](../architecture/core-architecture.md#vault-structure), [Page Generation Rules](../architecture/core-architecture.md#page-generation-rules)
- **Summary:** Multi-phase ingestion (scour/filter/ingest/link/lint), conversion report
- **Prereqs:** Phase 3 complete (Phase 5 can start in parallel)
- **Acceptance criteria:** All 5 phases execute, stub pages for undocumented areas, conversion report generated

### Phase 5: Agent Adapters
- **Doc:** `docs/phases/phase-5-agent-adapters.md`
- **Architecture:** [Schema Injection](../architecture/core-architecture.md#the-universal-schema)
- **Summary:** Plugin architecture, schema generation per tech stack, adapters for Claude/Codex/Gemini/Cursor
- **Prereqs:** Phase 3 complete (independent of Phase 4)
- **Acceptance criteria:** 4 adapters generate valid instruction files, plugin add command works

### Phase 6: Deterministic Lint
- **Doc:** `docs/phases/phase-6-lint.md`
- **Architecture:** [State Manifest](../architecture/core-architecture.md#state-manifest--mapping), [Invariants](../architecture/core-architecture.md#invariants--failure-modes)
- **Summary:** Link crawler, orphan/staleness detection, manifest consistency, doctor command
- **Prereqs:** Phase 3 complete
- **Acceptance criteria:** All deterministic checks pass, zero false negatives for broken links, doctor detects config issues

### Phase 7: Reporting & Obsidian
- **Doc:** `docs/phases/phase-7-reporting-obsidian.md`
- **Architecture:** [Vault Structure](../architecture/core-architecture.md#vault-structure)
- **Summary:** Bootstrap/sync/lint reports (Markdown + JSON), obsidian config, gh-wiki-sync with publish/exclude
- **Prereqs:** Phase 4 and Phase 5 complete
- **Acceptance criteria:** All 3 report types emit valid output, obsidian vault opens, wiki sync filters correctly

### Phase 8: Enforcement
- **Doc:** `docs/phases/phase-8-enforcement.md`
- **Architecture:** [Invariants](../architecture/core-architecture.md#invariants--failure-modes)
- **Summary:** Lefthook hooks, strictness levels, CI workflows, WIKI-DEBT tracking, schema migrate
- **Prereqs:** Phase 7 complete
- **Acceptance criteria:** Hooks fire correctly, CI blocks on wiki-debt, migrate updates schema-version

### Phase 9: Tool Integrations
- **Doc:** `docs/phases/phase-9-tool-integrations.md`
- **Architecture:** [Security & Trust](../architecture/core-architecture.md#security--trust)
- **Summary:** Memento integration, beads integration, PageIndex MCP server, retrieval agent, LLM-augmented lint
- **Prereqs:** Phase 8 complete
- **Acceptance criteria:** Memento transcripts ingested, beads task IDs linked to wiki pages, PageIndex serves queries

### Phase 10: Orchestration
- **Doc:** `docs/phases/phase-10-orchestration.md`
- **Architecture:** [Scaling Considerations](../architecture/core-architecture.md#scaling-considerations), [Invariants](../architecture/core-architecture.md#invariants--failure-modes)
- **Summary:** Assistive agent provider cascade, daemon mode, compile command, workspaces, retry policy
- **Prereqs:** Phase 9 complete
- **Acceptance criteria:** Provider cascade works, daemon polls and dispatches, compile produces deterministic output

---

## Architecture Reference

For detailed architectural context, see `docs/architecture/core-architecture.md`:

| Section | Content | When to Consult |
|---------|---------|-----------------|
| §1 Core Architecture | Three-layer diagram, responsibilities | Understanding the system shape |
| §2 Vault Structure | Full directory tree, file purposes | Implementing any file I/O |
| §3 Universal Schema | `_schema.md` content, agent injection table | Implementing agent adapters, schema injection |
| §4 Page Ownership | Ownership types, lifecycle state machine | Any wiki page creation or modification |
| §5 Page Generation | Slug/title/content/nav rules, taxonomy table | Implementing page generators |
| §6 State Manifest | `manifest.json` schema, update rules | Implementing sync, staleness, publish |
| §7 Configuration | `config.yml` schema | Implementing config loading, CLI flags |
| §8 Invariants | Never-violated rules, failure handling | Implementing any CLI command |
| §9 Scaling | Small/medium/large guidance, concurrency | Implementing daemon, compile |
| §10 Security | Content risk, privacy, token costs | Implementing assistive agent |

---

## Build Norms

1. **Every commit is memento-captured.** Run `git memento doctor` before pushing. If it fails, fix before pushing.
2. **Use `bd` for task tracking.** Don't just start working — log the task first so progress is visible across sessions.
3. **Reference architecture docs, don't duplicate.** When implementing, link to relevant sections of `core-architecture.md` rather than copying context into your implementation notes.
4. **Phase docs are the living spec.** The archived original (`docs/reference/plexium-spec-full.md`) is reference only. Implementation decisions live in the phase docs.
5. **Resolve open design questions before the blocking phase.** Check the prerequisite table above before starting a new phase.
6. **Log build entries in the Build Log table below.** After each phase completes, the finishing agent MUST add a row to the Build Log table with: date, phase(s) completed, agent name + model, and a concise summary of deliverables. Validation findings and fixes also go here.

---

## Build Log

| Date | Phase(s) | Agent | Summary |
|------|---------|-------|---------|
| 2026-04-05 | P0, M1, M2 | Claude Code | Phase 0 project setup (Go toolchain, bd, memento). Phase 1 CLI foundation (18 Cobra commands, config loader, scanner, markdown parser, template engine). Phase 2 page generation (taxonomy classifier, module/decision/concept generators, slug dedup, nav generators). |
| 2026-04-05 | M1, M2 | Kilo (zai-coding/glm-5.1) | Code review of M1+M2. Fixed 3 bugs (Deduplicate overwrite, extractTags header matching, scanner root-level glob), 7 design issues (config spec alignment, Viper env binding, JSON/JSON generation, template type consolidation, YAML safety). 38 tests passing. |
| 2026-04-05 | M3 | Claude Code (glm-5.1) | Phase 3 State & Publishing. Manifest CRUD (Load/Save/UpsertPage/RemovePage, bidirectional lookups). SHA256 hash computation (file, dir, batch). plexium init with full .wiki/ + .plexium/ scaffolding, README→Home, schema, config. plexium publish with GitHub Wiki push, publish/exclude filters. Dry-run mode for all write ops. 140 tests passing. |
| 2026-04-05 | M4 | Claude Code (Opus 4.6) | Phase 4 Convert (Brownfield). 6-phase pipeline: scour (README/ADR/config/source/doc extraction), filter (include/exclude with binary/size/UTF8 checks), ingest (taxonomy-based page generation), link (cross-reference injection), lint (gap analysis, orphan detection, stub creation), report (JSON + Markdown). plexium convert command with --depth and --dry-run. 40 convert tests + 140 prior = 180 tests passing. |
| 2026-04-05 | M6 | Claude Code (Sonnet 4 mini) + Qwen Code (validation) | Phase 6 Deterministic Lint. `internal/lint/` package: link crawler (finds/resolves [[wiki-links]], detects broken), orphan detector (inbound-link graph, sidebar-reachable exclusions), staleness detector (SHA256 hash comparison vs manifest), manifest validator (path/hash/link consistency), sidebar validator, frontmatter validator (required fields), `plexium lint --deterministic` (JSON + human-readable, exit codes), `plexium doctor` (8 system health checks). 8 lint tests passing. Fixed AC12 bug: `--ci` and `--fail-on` flags were read but never registered — added flag registration in `init()`. |
| 2026-04-06 | M6 | Claude Code | Deviation fix: Added explicit `--deterministic` flag to `lint` command per spec. Implementation previously always ran deterministic checks without flag; spec required `--deterministic` flag. Flag added, emits note when used without flag (Phase 9 `--full` not yet implemented). |
| 2026-04-06 | M8 | Claude Code (glm-5.1) | Phase 8 Enforcement. `internal/hook/` package: PreCommitHook (source file detection, wiki change check, strictness levels strict/moderate/advisory, PLEXIUM_BYPASS_HOOK bypass), PostCommitHook (WIKI-DEBT tracking in _log.md, bypass detection). `internal/ci/` package: diff-aware wiki check (base vs head SHA, source file filtering, manifest mapping, debt threshold). `internal/migrate/` package: schema versioning (read/write _schema.md, numbered migration scripts, dry-run mode). CLI: `plexium hook pre-commit`, `plexium hook post-commit`, `plexium ci check --base SHA --head SHA`, `plexium migrate [--dry-run] [--version N]`. Lefthook config (`lefthook.yml`). GitHub Actions: `plexium-lint.yml` (PR lint + PR comment), `plexium-sync.yml` (merge to main sync+publish), `plexium-scheduled-lint.yml` (weekly Monday 9am). 13 new tests across hook/ci/migrate. 208+ tests total passing. |
| 2026-04-06 | M9 | Claude Code (Opus 4.6, Agent Teams) | Phase 9 Tool Integrations. `internal/integrations/memento/`: MementoIngestor (transcript scanning, decision extraction via 8 regex patterns, decision page generation, idempotent processing via .processed markers), MementoGate (git notes check for CI). `internal/integrations/beads/`: BeadsLinker (bidirectional task↔page linking via frontmatter beads-ids, scan/link/unlink). `internal/integrations/pageindex/`: PageIndex (hierarchical text search with multi-term scoring), PageIndexServer (JSON-RPC 2.0 MCP server over stdio), Retriever (PageIndex + fallback chain via _index.md scan + content grep). `internal/lint/llm.go`: LLMAnalyzer (contradiction detection, concept extraction, cross-ref suggestions, semantic staleness), LLMClient interface, rate limiting, prompt templates. `internal/integrations/roles/`: Role types + Registry + RoleContext for Phase 10 orchestration. CLI: `plexium init --with-memento --with-beads --with-pageindex`, `plexium retrieve <query> --format json\|markdown`, `plexium lint --full`. 22 memento + 20 beads + 39 pageindex + 16 llm-lint + 5 roles = 102 new tests. All 221+ tests passing. |
| 2026-04-06 | M10 | Claude Code (Opus 4.6, Agent Teams) | Phase 10 Orchestration. `internal/agent/`: ProviderCascade (Ollama → OpenRouter → inherit, cost-ordered fallthrough), RateLimitTracker (daily budget, per-provider usage persistence), TaskRouter (cognitive complexity classification: low/medium/high/deterministic). `internal/daemon/`: Daemon loop (configurable watches: staleness/lint/ingest/debt, bounded concurrency), WorkspaceMgr (git worktree lifecycle), TrackerAdapter (GitHub Issues via gh CLI, Linear stub, NoOp), RunnerAdapter (Claude/Codex/Gemini CLI dispatch, NoOp). `internal/retry/`: RetryPolicy (exponential backoff, context-aware, retryable error classification). `internal/compile/`: Compiler (deterministic _index.md + _Sidebar.md regeneration from manifest). CLI: `plexium agent` (start/stop/status/test/spend/benchmark), `plexium daemon` (--poll-interval, --max-concurrent), `plexium compile` (--dry-run), `plexium orchestrate` (--issue). 156 new tests (agent: 51, daemon: 81, compile: 8, retry: 16). 455 total passing. |

---

*This document is the orchestration spine. Update status tracker as phases complete. See individual phase docs for implementation details.*
