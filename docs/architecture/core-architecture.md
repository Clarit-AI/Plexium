# Plexium Core Architecture

> **Reference Document** — Shared architectural context referenced by all phase documents.
> The canonical specification is `docs/reference/plexium-spec-full.md`.
> This document extracts the invariant architecture: layers, vault structure, schema, ownership model, manifest format, config schema, invariants, and failure modes.
> Agents consult specific sections as needed; do not attempt to read cover-to-cover.

---

## Table of Contents

1. [Core Architecture](#1-core-architecture)
2. [Vault Structure](#2-vault-structure)
3. [The Universal Schema](#3-the-universal-schema)
4. [Page Ownership Model](#4-page-ownership-model)
5. [Page Generation Rules](#5-page-generation-rules)
6. [State Manifest & Mapping](#6-state-manifest--mapping)
7. [Configuration](#7-configuration)
8. [Invariants & Failure Modes](#8-invariants--failure-modes)
9. [Scaling Considerations](#9-scaling-considerations)
10. [Security & Trust](#10-security--trust)

---

## 1. Core Architecture

Three layers, cleanly separated:

```
┌──────────────────────────────────────────────────────────────┐
│                  Source Layer (immutable)                     │
│  src/**  •  docs/**  •  ADRs  •  README  •  raw/transcripts  │
│                                                              │
│  The LLM reads from this layer but NEVER modifies it.        │
│  This is the ground truth.                                   │
└────────────────────────────┬─────────────────────────────────┘
                             │
                ┌────────────▼──────────────┐
                │     State Manifest        │
                │  .plexium/manifest.json   │
                │  (bidirectional mapping,  │
                │   hashes, ownership,       │
                │   deterministic tracking)  │
                └────────────┬──────────────┘
                             │
┌────────────────────────────▼─────────────────────────────────┐
│                 Wiki Layer (.wiki/)                           │
│  _schema.md  •  _index.md  •  _log.md                        │
│  architecture/  •  modules/  •  decisions/  •  concepts/      │
│  Home.md  •  _Sidebar.md  •  _Footer.md                      │
│                                                              │
│  Ownership: managed | human-authored | co-maintained        │
│  Obsidian vault  •  GitHub Wiki submodule                    │
│                                                              │
│  The LLM owns this layer. It creates pages, updates them,    │
│  maintains cross-references, and keeps everything consistent.│
│  Humans read it, review it, and promote it.                   │
└────────────────────────────┬─────────────────────────────────┘
                             │
┌────────────────────────────▼─────────────────────────────────┐
│                    Control Layer                             │
│  _schema.md (constitution — how agents maintain the wiki)   │
│  Agent adapters: CLAUDE.md, AGENTS.md, .cursor/rules/        │
│  .plexium/config.yml  •  .plexium/plugins/                   │
│  lefthook.yml (git hooks)                                    │
│  .github/workflows/plexium-*.yml (CI/CD)                    │
│  .plexium/reports/ (structured output)                       │
└────────────────────────────┬─────────────────────────────────┘
                             │
                ┌────────────▼──────────────┐
                │   Enforcement Layers     │
                │  1. Schema (soft)        │
                │  2. Git hooks (medium)  │
                │  3. CI/CD (hard)        │
                │  4. Memento gate        │
                └──────────────────────────┘
```

```
                             │
                ┌────────────▼──────────────┐
                │   Execution Plane         │
                │   (optional, Phase 3)    │
                │   WORKFLOW.md  •  Daemon  │
                │   Workspace Mgr  •  Runner│
                │   Tracker Adapter         │
                └──────────────────────────┘
```

**Key principles:**
- Source layer is immutable. Wiki operations never touch source code.
- Wiki layer is LLM-maintained. Agents own it; humans review it.
- State manifest bridges source and wiki layers with deterministic tracking.
- Control layer governs behavior and enforces discipline.
- Optional execution plane (Phase 3) adds autonomous orchestration.

---

## 2. Vault Structure

The wiki lives at **`.wiki/`** at the repository root. Mirrors `.github/`, `.vscode/`, `.husky/` — a dotfolder signaling tooling infrastructure.

`.wiki/` is simultaneously:
- An **Obsidian vault** (with `.obsidian/` config inside)
- A **git submodule** pointing to `https://github.com/{org}/{repo}.wiki.git`, powering the GitHub Wiki tab
- The **canonical knowledge layer** for all coding agents

```
.wiki/                              # Obsidian vault root + GitHub Wiki submodule
├── .obsidian/                      # Vault config (graph view, Dataview, Marp)
├── _schema.md                      # The constitution — agent behavioral directives
├── _index.md                       # Master catalog: every page with summary + metadata
├── _log.md                         # Append-only chronological ledger (parseable)
├── Home.md                         # GitHub Wiki landing page
├── _Sidebar.md                     # GitHub Wiki navigation sidebar
├── _Footer.md                      # GitHub Wiki footer (optional)
├── architecture/
│   ├── overview.md                 # System-level architecture, data flow, deployment
│   ├── data-model.md
│   └── infrastructure.md
├── modules/                        # One page per major component/service/package
│   ├── auth.md
│   ├── api-gateway.md
│   └── ...
├── decisions/                      # Architecture Decision Records (ADRs)
│   ├── 001-chose-postgres.md
│   ├── 002-event-sourcing.md
│   └── ...
├── patterns/                       # Recurring patterns, conventions, anti-patterns
│   ├── error-handling.md
│   └── testing-strategy.md
├── concepts/                       # Domain concepts, glossary, business logic
├── onboarding.md                   # Generated onboarding guide
├── contradictions.md               # Explicitly flagged inconsistencies
├── open-questions.md               # Unresolved design questions, knowledge gaps
├── raw/                            # Immutable source documents
│   ├── meeting-notes/
│   ├── ticket-exports/
│   ├── memento-transcripts/        # Auto-ingested from memento session notes
│   └── assets/                     # Downloaded images, diagrams
└── reports/                        # Generated reports (bootstrap, sync, lint)

.plexium/                          # Plexium internal config (in repo root)
├── config.yml                      # Project settings, integrations, strictness
├── manifest.json                   # Bidirectional source↔wiki mapping + state
├── plugins/                        # Agent adapter plugins (extensible)
│   ├── claude.sh
│   ├── codex.sh
│   ├── gemini.sh
│   └── cursor.sh
├── hooks/                          # Validation scripts for git hooks + CI
├── templates/                      # Page templates for modules, ADRs, concepts
├── prompts/                        # Reusable prompt fragments for operations
├── migrations/                     # Schema migration scripts
├── WORKFLOW.md                     # Orchestration/execution contract (optional, Phase 3)
├── workspaces/                     # Per-issue git worktrees (daemon mode)
└── reports/                        # Machine-readable report output (JSON)
```

---

## 3. The Universal Schema

`_schema.md` is the constitution. It transforms a generic LLM into a disciplined wiki maintainer. Written once, referenced by every agent-specific instruction file via adapter plugins.

```markdown
# PLEXIUM SCHEMA v1 — MANDATORY AGENT DIRECTIVES

You are the custodian of the `.wiki/` vault in this repository. Your memory
does not persist between sessions, but this vault does. It is the compiled,
persistent knowledge of this entire codebase.

## MANDATORY WORKFLOW — EVERY TASK

### 1. READ (before any code change)
- Read `.wiki/_index.md` to orient yourself.
- Fetch relevant module, architecture, and decision pages for your work area.
- If a retrieval tool is available (PageIndex MCP, `plexium retrieve`),
  use it instead of scanning files manually.
- Check `.wiki/_log.md` (last 10 entries) for recent context.
- Check page `ownership` frontmatter before modifying any wiki page.

### 2. EXECUTE
- Perform the coding task requested by the user.

### 3. DOCUMENT (FORBIDDEN to end your task without this step)
- Update every `.wiki/modules/*.md` page affected by your changes.
- If you made an architectural decision, create or update a `.wiki/decisions/*.md` ADR.
- If you discovered a contradiction, add it to `.wiki/contradictions.md`.
- Add an entry to `.wiki/_log.md` (see LOG FORMAT below).
- Update `.wiki/_index.md` if you created or removed pages.
- Update cross-references ([[wiki-links]]) on pages whose relationships changed.
- NEVER modify pages with `ownership: human-authored` unless explicitly instructed.
- For `ownership: co-maintained` pages, append only — do not rewrite existing sections
  unless the user specifically requests it.

### 4. VALIDATE
- Confirm wiki updates are consistent with the code you actually wrote.
- Mark uncertain claims with `<!-- CONFIDENCE: low — needs human review -->`.
- Verify all [[wiki-links]] you created resolve to existing pages.
- Verify `source-files` frontmatter references existing paths.

## TRIVIAL CHANGE EXCEPTION
For changes affecting only a single file with no architectural impact
(typo fixes, version bumps, formatting): a brief `_log.md` entry suffices.
Full wiki update not required.

## LOG FORMAT
Each entry in `_log.md` must use this parseable format:

  ## [YYYY-MM-DD] {task|ingest|lint|query|convert} | Brief description
  - Changed: modules/auth.md, architecture/overview.md
  - Decision: decisions/015-jwt-rotation.md (new)
  - Contradictions: None found
  - Files touched: src/auth/middleware.ts, src/auth/jwt.ts

## PAGE GENERATION RULES

### Slug rules
- Page names must be filesystem-safe (no spaces — use hyphens).
- Duplicate titles must be deduplicated predictably (append qualifier).
- Heading-derived slugs must remain stable across regenerations.

### Navigation rules
- Every generated page must be reachable from _index.md directly or indirectly.
- _Sidebar.md must expose top-level sections and key pages.
- Navigation ordering must be deterministic (alphabetical within sections).

### Content rules
- Preserve factual meaning from source docs and code.
- NEVER invent implementation details not present in sources.
- Summarize when needed but do not silently discard major sections.
- Prefer cross-links ([[wiki-links]]) over duplicated paragraphs.
- Every page must begin with YAML frontmatter (see FRONTMATTER SPEC).

### Cross-reference rules
- When mentioning a concept, module, or decision that has its own page, use [[wiki-links]].
- Never remove existing cross-references without logging the removal in _log.md.
- When creating a new page, add inbound links from at least 2 related existing pages.

## FRONTMATTER SPEC
Every wiki page must begin with:
```yaml
---
title: <Human-readable title>
ownership: managed              # managed | human-authored | co-maintained
last-updated: YYYY-MM-DD
updated-by: <agent-name>
related-modules: [<list>]
source-files: [<glob patterns>]
confidence: high                # high | medium | low
review-status: unreviewed       # unreviewed | human-verified | stale
tags: [<list>]
---
```

## LINT PROTOCOL
When asked to lint, check for:
- Pages not updated in >30 days that reference frequently-changed code
- Orphan pages (no inbound [[links]])
- Concepts mentioned in 3+ pages without their own page
- Contradictions between module pages and architecture overview
- `source-files` in frontmatter referencing paths that no longer exist
- Missing cross-references between related modules
- Pages with `confidence: low` that need investigation
- Managed pages whose source file hashes differ from the state manifest

## INGEST PROTOCOL
When a new raw source is added (meeting note, ticket export, memento transcript):
1. Read it fully
2. Discuss key takeaways with the user (unless batch mode)
3. Write a summary page or update existing pages
4. Update _index.md, _log.md, _Sidebar.md
5. Cross-reference with existing module/decision pages
6. Flag contradictions with existing wiki content
7. Update the state manifest with new source mappings
```

**Schema injection per agent:**

| Agent | Instruction File | Method |
|-------|-----------------|--------|
| Claude Code | `CLAUDE.md` | Includes full schema excerpt + path references |
| OpenAI Codex | `AGENTS.md` | Same content, Codex format |
| Gemini CLI | `.gemini/config.md` | Same content |
| Cursor | `.cursor/rules/plexium.mdc` | Same content, MDC format |
| OpenCode | `opencode.json` agents config | Registers schema as system context |
| Continue.dev | `.continue/rules/plexium.md` | Same content |
| Future agents | `plexium plugin add <name>` | Plugin writes new instruction file |
| Symphony | `WORKFLOW.md` (injected section) | Plexium directives appended to orchestration contract |

---

## 4. Page Ownership Model

Every wiki page has an explicit owner. This is the single most critical trust mechanism.

| Ownership | Meaning | Agent Behavior |
|-----------|---------|----------------|
| `managed` | Created and controlled by Plexium automation. Safe to rewrite. | Agents may freely update, rewrite, or regenerate. |
| `human-authored` | Written by a human. Agents must not modify. | Agents must NEVER overwrite. They may suggest changes via `_log.md` or PR comments. |
| `co-maintained` | Collaboratively maintained. Agents may append, humans structure. | Agents append new sections or update data. Never rewrite existing prose without explicit instruction. |

**Enforcement:**
- Ownership declared in page frontmatter (`ownership: managed`)
- Ownership tracked in `.plexium/manifest.json` (machine-readable, deterministic)
- Deterministic lint layer rejects commits where an agent modified a `human-authored` page
- Schema explicitly instructs agents to check ownership before writing
- Optional: generated marker comment at top of managed pages:
  ```markdown
  <!-- PLEXIUM:MANAGED — This page is maintained by Plexium. Manual edits may be overwritten. -->
  ```

**Default behavior:**
- Pages created by `plexium init`, `plexium convert`, or agents following schema default to `managed`
- Pages created manually by humans (detected by absence from manifest) default to `human-authored`
- Users can promote any page to `co-maintained` when they want to add human context while keeping agent updates flowing

### Page Lifecycle State Machine

Beyond ownership, every wiki page has a lifecycle state with explicit transition rules:

```
stub → generated → unreviewed → human-verified → stale → regenerated → unreviewed
```

| State | Meaning | Transition To | Triggered By |
|-------|---------|---------------|-------------|
| `stub` | Placeholder created during convert/init | `generated` | Agent or `plexium sync` fills content |
| `generated` | Content created by automation | `unreviewed` | Automatic on creation |
| `unreviewed` | Content exists but not human-reviewed | `human-verified` | Human PR review approval |
| `human-verified` | Human has confirmed accuracy | `stale` | Source file hashes drift from manifest |
| `stale` | Source has changed since last wiki update | `regenerated` | `plexium sync`, daemon, or agent |
| `regenerated` | Updated after staleness detected | `unreviewed` | Automatic (re-enters review cycle) |

**Transition rules:**
- Only automation (agents, `plexium convert`, `plexium sync`) can move `stub → generated`
- Only humans can move `unreviewed → human-verified` (via PR review)
- `human-verified → stale` is automatic (deterministic hash comparison)
- The daemon (Phase 3) watches for pages entering `stale` and dispatches agents to regenerate

Lifecycle state is tracked in `review-status` frontmatter and in `manifest.json`.

---

## 5. Page Generation Rules

Concrete, testable rules that keep the wiki structurally sound.

### Slug Rules
- Page names must be filesystem-safe: lowercase, hyphens instead of spaces, no special characters
- Duplicate titles must be deduplicated predictably: append parent directory or qualifier (`auth` vs `auth-middleware`)
- Heading-derived slugs must remain stable — once a slug is assigned, it does not change unless the page is intentionally renamed (tracked in manifest)

### Title Rules
- Prefer human-readable titles over raw filenames
- Avoid raw filename leakage when a better title exists in the content
- Append qualifiers only when necessary to avoid collisions

### Content Rules
- Preserve factual meaning from source docs and code
- Never invent implementation details not present in sources
- Summarize when needed but never silently discard major sections
- Prefer cross-links (`[[wiki-links]]`) over duplicated paragraphs
- Each page should be self-contained enough to be useful without reading all linked pages

### Navigation Rules
- Every generated page must be reachable from `Home.md` / `_index.md` directly or via one intermediate link
- `_Sidebar.md` must expose all top-level sections and high-traffic pages
- Navigation ordering must be deterministic (alphabetical within sections, chronological for decisions/log)
- `_Footer.md` includes last-updated timestamp and link back to `Home.md`

### Page Taxonomy

| Source | Wiki Output | Section |
|--------|------------|---------|
| `README.md` | `Home.md` | Root |
| `src/{module}/` | `modules/{module}.md` | Modules |
| `docs/*.md` | Pages by content type | Architecture / Guides / Concepts |
| `docs/{folder}/` | Section index page + child pages | Varies |
| ADR files | `decisions/NNN-title.md` | Decisions |
| Named domain concepts | `concepts/{concept}.md` | Concepts |
| Recurring patterns | `patterns/{pattern}.md` | Patterns |
| Raw ingested sources | `raw/{category}/{source}` + summary in wiki | Raw (immutable) |

---

## 6. State Manifest & Mapping

`.plexium/manifest.json` tracks the bidirectional relationship between source files and wiki pages. Machine-readable companion to the human-readable `_index.md`.

```json
{
  "version": 1,
  "lastProcessedCommit": "abc123f",
  "lastPublishTimestamp": "2026-04-05T14:30:00Z",
  "pages": [
    {
      "wikiPath": "modules/auth.md",
      "title": "Auth Module",
      "ownership": "managed",
      "section": "modules",
      "sourceFiles": [
        {
          "path": "src/auth/**",
          "hash": "sha256:e3b0c44...",
          "lastProcessedCommit": "abc123f"
        },
        {
          "path": "docs/auth-flow.md",
          "hash": "sha256:d7a8fbb...",
          "lastProcessedCommit": "abc123f"
        }
      ],
      "generatedFrom": ["src/auth/", "docs/auth-flow.md"],
      "lastUpdated": "2026-04-05",
      "updatedBy": "claude-code",
      "inboundLinks": ["architecture/overview.md", "modules/api-gateway.md"],
      "outboundLinks": ["decisions/003-jwt-strategy.md", "concepts/rbac.md"]
    }
  ],
  "unmanagedPages": [
    {
      "wikiPath": "architecture/legacy-notes.md",
      "firstSeen": "2026-03-15",
      "ownership": "human-authored"
    }
  ]
}
```

**The manifest enables:**
- Forward mapping: "Which wiki pages were generated from `src/auth/`?"
- Reverse mapping: "Which source files feed into `modules/auth.md`?"
- Staleness detection: "Has `src/auth/middleware.ts` changed since its wiki page was last updated?" (hash comparison)
- Ownership enforcement: "Is this page managed or human-authored?"
- Idempotency: "Was this commit already processed?" (lastProcessedCommit)
- Link integrity: "Do all inbound/outbound links resolve?"

**Update rules:**
- The manifest is updated by the deterministic pipeline, not by LLM agents directly
- Agents update wiki pages; the `plexium` CLI updates the manifest from the resulting state
- CI validates manifest consistency on every PR

---

## 7. Configuration

`.plexium/config.yml` — project-level configuration:

```yaml
version: 1

repo:
  defaultBranch: main
  wikiEnabled: true

sources:
  include:
    - "README.md"
    - "docs/**/*.md"
    - "adr/**/*.md"
    - "src/**"
  exclude:
    - "**/node_modules/**"
    - "**/.next/**"
    - "**/dist/**"
    - "**/vendor/**"

agents:
  adapters:
    - claude
    - codex
    - gemini
    - cursor
  strictness: moderate          # strict | moderate | advisory

wiki:
  root: .wiki/
  home: Home.md
  sidebar: _Sidebar.md
  footer: _Footer.md
  log: _log.md
  index: _index.md
  schema: _schema.md

taxonomy:
  sections:
    - Architecture
    - Modules
    - Decisions
    - Patterns
    - Concepts
    - Guides
  autoClassify: true            # Use directory structure for initial classification

publish:
  preserveUnmanagedPages: true
  managedMarkerComment: true    # Insert <!-- PLEXIUM:MANAGED --> in managed pages

sync:
  mode: incremental             # incremental | full
  rewriteHomeOnSync: true
  rewriteSidebarOnSync: true
  idempotent: true              # Skip processing if commit already handled

enforcement:
  preCommitHook: true
  ciCheck: true
  mementoGate: false            # Enable when memento is configured
  strictness: moderate          # strict | moderate | advisory

integrations:
  memento: false
  beads: false
  pageindex: false
  obsidian: true

reports:
  emitBootstrapReport: true
  emitSyncReport: true
  emitLintReport: true
  format: both                  # json | markdown | both
  outputDir: .plexium/reports/

githubWiki:
  enabled: true
  submodule: true
  publish:
    - "architecture/**"
    - "modules/**"
    - "decisions/**"
    - "patterns/**"
    - "concepts/**"
    - "onboarding.md"
    - "Home.md"
    - "_Sidebar.md"
    - "_Footer.md"
  exclude:
    - "raw/**"
    - "reports/**"
    - ".obsidian/**"

sensitivity:
  rules: .plexium/sensitivity-rules.yml  # Optional
  neverPublish:
    - "raw/internal/**"
    - "**/*[CONFIDENTIAL]*"
```

**Assistive agent config** (Phase 10 scope):
Defined in the same file under `assistiveAgent` key. Provider cascade, task routing, rate limits, privacy mode, retry policy, and daemon settings are all Phase 10 content. See `docs/phases/phase-10-orchestration.md`.

---

## 8. Invariants & Failure Modes

### Invariants (Never Violated)

| Invariant | Description |
|-----------|-------------|
| **Never modify source code** | Wiki operations never touch files outside `.wiki/` and `.plexium/` |
| **Never delete unmanaged pages** | Pages with `ownership: human-authored` are never overwritten or removed by automation |
| **Never publish partial output** | If any step fails mid-update, roll back all wiki changes in the commit. No half-updated wikis. |
| **Never silently proceed on invalid config** | If `config.yml` or `_schema.md` is malformed, fail fast with actionable remediation |
| **Never commit references to nonexistent files** | `source-files` frontmatter and `[[wiki-links]]` are validated before commit |
| **Never remove cross-references silently** | Deletions of existing `[[links]]` must be logged in `_log.md` |

### Common Failure Cases & Handling

| Failure | Handling |
|---------|----------|
| Wiki not enabled on GitHub repo | `plexium doctor` detects and provides instructions to enable |
| Auth/token invalid or insufficient | Fail fast with specific remediation (which permissions are missing) |
| Duplicate page slug collision | Deterministic deduplication (append qualifier), log resolution |
| Malformed markdown source | Skip file, log warning in report, continue processing |
| Source file removed but mapping exists | Flag as stale in lint report, mark page for review |
| Sidebar links point to missing pages | Deterministic validation catches before publish |
| LLM generates hallucinated content | `confidence` frontmatter + `review-status` tracking + human review |
| Agent modifies `human-authored` page | Pre-commit hook rejects (deterministic check on frontmatter) |
| Manifest drift from actual wiki state | `plexium doctor` detects and offers `plexium manifest rebuild` |
| Concurrent agents update same page | See [Scaling Considerations](#9-scaling-considerations) |

### Fallback Behavior
- Stop publish on structural validation errors
- Allow dry-run artifact generation for debugging: `plexium sync --dry-run` outputs to `.plexium/output/`
- Produce actionable remediation output in all error messages
- Never exit silently — always emit a report

### Retry Semantics
For transient failures (LLM API errors, GitHub push rate limits, PageIndex unavailability), Plexium applies exponential backoff:
- **Default policy:** 3 attempts, 5s initial delay, 2× backoff multiplier, 60s max delay
- **Configurable** via `retry` block in `.plexium/config.yml`
- **Scope:** applies to all external API calls (LLM providers, GitHub API, issue trackers)
- **Daemon mode:** failed work items are released back to the queue after max retries, with the failure logged to `_log.md`

---

## 9. Scaling Considerations

### Small Projects (~10K LOC, 1–3 developers)
- `_index.md` is sufficient for navigation
- PageIndex not needed
- Single-file `manifest.json` works fine
- Advisory strictness is often sufficient — small teams self-enforce

### Medium Projects (~100K LOC, 5–15 developers)
- PageIndex becomes valuable for retrieval
- Full CI enforcement recommended
- Memento integration provides audit trail for distributed teams
- Weekly scheduled lint runs

### Large Projects / Monorepos (~500K+ LOC, 20+ developers)
- `_index.md` becomes insufficient → PageIndex required
- **Module-level sub-indexes:** each `modules/` subdirectory gets its own `_index.md`
- **CODEOWNERS-style governance:** `.plexium/owners.yml` maps wiki sections to responsible teams
- **Monorepo scoping:** Lefthook + Plexium scope hooks to specific packages, each with their own wiki subdirectory
- **Merge conflict mitigation:**
  - `_log.md` is append-only — conflicts trivially resolvable (keep both entries)
  - Module pages are scoped — different developers working on different modules rarely cause wiki conflicts
  - When pages do conflict: wiki Markdown conflicts are simpler than code conflicts, and agents can resolve them
  - For high-contention pages (`architecture/overview.md`): convention of appending `## Recent Changes` sections rather than rewriting the body, with periodic consolidation

### Concurrency Under Parallel Orchestration

Under daemon mode with `maxConcurrent > 1`, shared navigation files (`_index.md`, `_Sidebar.md`, `Home.md`, `_log.md`) become merge-conflict hotspots. The solution:
- **Agents update only content pages** (module, decision, concept pages) and append to per-run log files (`.plexium/workspaces/<ID>/run-log.md`)
- **`plexium compile`** deterministically regenerates all shared navigation files from page state — no LLM calls, no conflicts
- **`_log.md`** entries from parallel runs are merged by timestamp (append-only, chronologically sorted)
- This compilation step runs automatically after each daemon run and by CI on merge

### GitHub Wiki Submodule Under Concurrent Pushes

Concurrent pushes to `{repo}.wiki.git` will fail under parallel operation. Solutions:
- **Serialize wiki publishes** — `plexium compile` + `plexium publish` is a single-writer operation via lock file
- **Batch publishing** — collect changes and publish once per CI cycle or daemon poll interval

### Schema Migration

When wiki conventions change (new frontmatter field, section rename, structural change):
- `plexium migrate` applies schema changes across all existing pages
- `_schema.md` includes `schema-version: 1` — version is bumped on breaking changes
- Migration scripts live in `.plexium/migrations/` — similar to database migrations
- Migrations are deterministic and reversible where possible

When `plexium migrate` bumps `schema-version` during active daemon operation:
- **Drain before migrate:** signal the daemon to stop claiming new work, wait for in-flight runs to complete (configurable timeout), apply migration, resume daemon
- **Version assertion:** each daemon run checks `schema-version` at start — abort and re-queue if it doesn't match the dispatched version
- **Non-breaking migrations** (adding optional frontmatter fields) don't require drain
- **Breaking migrations** (renaming sections, changing required fields) require drain + rebuild

---

## 10. Security & Trust

### Agent-Generated Content Risk

Agents can introduce hallucinated or subtly incorrect information. Mitigations:

| Mechanism | Purpose |
|-----------|---------|
| `review-status` frontmatter | Every agent-written page starts as `unreviewed`. Humans promote to `human-verified`. |
| `confidence` markers | Agents mark uncertain claims. Dataview surfaces these for review. |
| PR diff review | Wiki changes appear in PR diffs alongside code, subject to same review process. |
| Ownership model | `human-authored` pages cannot be modified by agents. |
| Deterministic validation | Structural correctness (links, paths, frontmatter) is verified without LLM. |

**Framing:** This is not worse than the status quo. Agents already write code that goes through review. Wiki pages should receive the same scrutiny — Plexium makes documentation *visible and reviewable* rather than absent.

### Sensitive Information
- `plexium init --private` creates `.plexium/sensitivity-rules.yml`
- The schema instructs agents: "Never include API keys, secrets, PII, or content marked as CONFIDENTIAL in wiki pages"
- `githubWiki.exclude` config prevents sensitive pages from publishing to the public wiki tab
- `.wiki/raw/` can be added to `.gitignore` for projects with sensitive source documents
- For open-source repos: the wiki can be a private submodule
- `plexium lint --security` scans for accidental secret exposure in wiki pages

### Privacy by Provider Tier

When using the assistive agent, data handling varies by provider:

| Provider | Data Handling | Suitable For |
|----------|--------------|-------------|
| **Local (Ollama)** | Never leaves the machine. Total privacy. | Proprietary codebases, regulated industries, air-gapped environments |
| **OpenRouter Free** | Provider-dependent. Some models log prompts for training. | Open-source projects, non-sensitive wiki content |
| **OpenRouter Paid** | Generally better data policies than free tier. | Most commercial projects |
| **Primary Agent** | Depends on provider (Anthropic, OpenAI, Google policies). | Whatever the team already trusts for code |

`privacyMode: strict` in config restricts routing to providers that don't log prompts for training. The `assistiveAgent.neverSend` patterns exclude sensitive content from assistive agent routing entirely.

### Token Cost & Latency

Mandating agents read `_index.md` + relevant pages adds tokens per task. Mitigations:
- **The index is small:** `_index.md` is one-liners, typically 2–5K tokens even for large projects
- **Targeted reads:** Agents fetch only relevant pages via PageIndex, not the entire wiki
- **The tradeoff is favorable:** Reading 3–4 wiki pages (5K tokens) is far cheaper than an agent spending 20+ tool calls grepping through source files to rediscover context
- **Trivial change exception:** The schema allows skipping full wiki updates for single-file, non-architectural changes

---

*This document extracts the invariant architecture from the full specification. For complete context including CLI commands, enforcement details, and integration specs, see `docs/reference/plexium-spec-full.md`.*
