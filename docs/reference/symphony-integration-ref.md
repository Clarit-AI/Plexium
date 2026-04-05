> **Reference Document** — This is voluntary reading for context and rationale.
> The canonical specification is `plexium-spec.md` at project root.
> Agents may consult this but should NOT treat it as authoritative where it conflicts with the PRP.

# Assessment: Plexium PRP × OpenAI Symphony Integration Viability

---

## Executive Verdict

**Strongly viable.** Plexium and Symphony are complementary systems operating at different layers of the same autonomous development stack. Plexium is the **persistent knowledge/memory layer** — the compiled brain of the repository. Symphony is the **runtime orchestration/execution layer** — the autonomous nervous system that turns work items into completed PRs without human supervision. Neither replaces the other; together they create a closed loop that neither achieves alone.

The most important insight here isn't any specific Symphony feature — it's the validation that **in-repo policy files governing autonomous agent behavior** (`_schema.md` / `WORKFLOW.md`) is the emerging industry standard for agentic software engineering. Symphony and Plexium are the execution-layer and knowledge-layer instantiations of the same pattern. OpenAI calls it "harness engineering" — designing infrastructure, constraints, and feedback loops that make AI agents reliably productive ([github.com](https://github.com/openai/symphony/blob/main/SPEC.md)). Plexium already *is* harness engineering for knowledge. Symphony adds harness engineering for work execution.

Your PRP is already Symphony-ready in spirit but not Symphony-native operationally. It has all the parts Symphony wants agents to rely on — repo-owned policy, deterministic state, enforceable process, structured reports, and a durable knowledge surface. What it lacks is the **proactive, autonomous execution loop**: issue ingestion, run scheduling, workspace isolation, retries, reconciliation, and live observability. Symphony provides exactly that.

The combined system becomes:

```
Issue tracker → Symphony-style orchestrator → isolated worktree run 
    → Plexium retrieval/update → PR/handoff → deterministic wiki compile/publish
    → lint findings feed back as new issues → loop
```

---

## PRP Quality Assessment

Before addressing Symphony, the PRP itself is remarkably well-constructed. Clean layering (source → wiki → control → enforcement), a rigorous ownership model, deterministic-first architectural discipline, phased delivery with shippable milestones, comprehensive failure mode handling, and honest open design questions. This is a build-ready spec.

The primary structural gap: **the PRP relies entirely on reactive triggers.** Git hooks fire on commits, CI fires on PRs, `plexium sync` is run manually, and agents are expected to self-enforce via schema directives. There is no proactive autonomous loop. Documentation debt accumulates passively. Stale pages are detected but not automatically fixed. This is precisely the gap Symphony fills.

---

## Strategic Fit: How Symphony Maps to Plexium

| Concern | Plexium Handles | Symphony Handles |
|---------|-----------------|-----------------|
| **Knowledge persistence** | `.wiki/` vault, `_schema.md`, `manifest.json` | — (explicitly lacks persistent memory across runs) |
| **Agent behavioral policy** | `_schema.md` + agent adapters | `WORKFLOW.md` (YAML frontmatter + prompt body) |
| **Work dispatch** | — (reactive only) | Polls issue tracker, claims eligible work |
| **Execution isolation** | — (agents run in developer's checkout) | Per-issue workspaces with lifecycle hooks |
| **Concurrency control** | — (serial by assumption) | Bounded concurrent agent runs |
| **Retry/recovery** | — (not specified) | Exponential backoff, restart reconciliation |
| **Observability** | Batch reports (JSON + Markdown) | Structured logs, live run telemetry |
| **Enforcement** | Schema injection, git hooks, CI gates | — (defers to agent tooling and host OS) |
| **Handoff states** | `review-status` frontmatter | Workflow-defined states like `Human Review` |

The mutual benefit is clear: Symphony currently lacks any persistent knowledge synthesis across agent runs — every Codex session starts from scratch. Plexium solves exactly that. Meanwhile, Plexium currently has no autonomous execution engine — work only happens when a human triggers it. Symphony solves exactly that ([rywalker.com](http://rywalker.com/research/symphony)).

---

## Three Integration Tiers

### Tier 1: Low-Coupling Symbiosis — Do This Now

**Plexium as Symphony's knowledge layer. No architectural changes to either system.**

Any team running Symphony injects Plexium's schema directives into their `WORKFLOW.md`:

```markdown
# WORKFLOW.md

## Before Starting Any Issue
1. Read `.wiki/_index.md` to orient yourself on the codebase.
2. Read relevant `.wiki/modules/*.md` pages for your work area.
3. Check `.wiki/_log.md` (last 10 entries) for recent context.
4. Use `plexium retrieve "<topic>"` if PageIndex is configured.

## After Completing Any Issue  
1. Update every `.wiki/modules/*.md` page affected by your changes.
2. If you made an architectural decision, create/update a `.wiki/decisions/*.md` ADR.
3. Add an entry to `.wiki/_log.md`.
4. Update `.wiki/_index.md` if you created or removed pages.
5. Mark uncertain claims with `<!-- CONFIDENCE: low -->`.
```

**Why this is high-value at near-zero cost:**

Every Symphony-spawned agent session currently re-discovers codebase architecture from scratch. With Plexium's wiki in place, every agent run reads compiled, cross-referenced knowledge instead of raw source files — and contributes back on completion. The wiki compounds with every issue resolved. The brownfield enrichment pattern from PRP §19 happens automatically as a side effect of normal issue resolution.

**Implementation:** Add a `symphony` adapter plugin to `.plexium/plugins/` (Milestone 5 task). The plugin generates Plexium directives formatted for `WORKFLOW.md` instead of `CLAUDE.md`.

### Tier 2: Wiki-Aware Orchestration — Design Now, Build in Phase 3

**A Symphony-style daemon loop applied to wiki maintenance operations.**

Your PRP describes several operations that are currently reactive but should be proactive:

| Current Trigger | Symphony-Style Trigger |
|---|---|
| `plexium sync` (manual CLI) | Daemon polls for commits with unmapped wiki changes |
| `plexium lint` (manual or weekly cron) | Daemon continuously monitors for staleness, drift |
| WIKI-DEBT accumulates silently | Daemon auto-creates remediation tasks or auto-fixes |
| Ingest is user-initiated | Daemon watches `.wiki/raw/` for new source documents |
| Contradictions flagged but unresolved | Daemon spawns dedicated agent to propose resolutions |

This is the **"self-healing documentation"** loop: `plexium lint` finds stale pages, contradictions, and missing concept pages → those findings become tracker issues → the orchestrator picks them up as autonomous maintenance work → agents fix them → the wiki improves → fewer findings next cycle. This creates a genuine feedback loop where documentation debt resolves itself asynchronously.

**Implementation:** A `plexium daemon` command (see [Recommended PRP Amendments](#recommended-prp-amendments) below).

### Tier 3: Full Bidirectional Integration — Premature

**Deep coupling where Plexium becomes a first-class module inside Symphony's orchestration loop.**

This would mean Symphony creates issues for stale wiki pages, dispatches wiki-update agents, validates wiki completeness as a PR gate, and uses Plexium's lint report to determine issue eligibility.

**Why this is premature:**
- Symphony is explicitly "a low-key engineering preview for testing in trusted environments" ([github.com](https://github.com/openai/symphony)). Building on pre-v1 infrastructure is risky.
- Symphony is currently Linear-specific and Codex-focused. Plexium is deliberately agent-neutral and tracker-agnostic. Tight coupling would sacrifice this universality.
- The Elixir runtime question: Symphony's reference implementation is Elixir; Plexium is planned as Go/Rust. Deep integration means bridging runtimes or rewriting components.
- Scope identity crisis: Plexium's pitch is "give it your repo, it builds a wiki." Symphony's is "give it your issue tracker, it builds PRs." Merging them dilutes both messages.

**However**, Symphony's state machine for issue lifecycle (eligible → claimed → running → success/failed → released) contains a design lesson worth extracting. See the page state machine recommendation below.

---

## Specific Elements to Adopt from Symphony

### 1. The `WORKFLOW.md` Contract (High Priority)

**Position on the `_schema.md` vs. `WORKFLOW.md` question:** Keep them as **two separate files** with distinct responsibilities.

- **`_schema.md`** = the knowledge constitution. How agents must read, update, and maintain the wiki. Injected into agent instruction files (`CLAUDE.md`, `AGENTS.md`, `.cursorrules`).
- **`WORKFLOW.md`** = the orchestration/execution contract. How autonomous runs are dispatched, what triggers them, runtime settings, timeout/retry policy, handoff states. Read by the orchestrator daemon.

This separation is cleaner than overloading `_schema.md` with scheduling and runtime concerns, and cleaner than trying to merge fundamentally different policy types into one file. The two files reference each other: `WORKFLOW.md` says "before starting any issue, follow the READ protocol in `_schema.md`." The knowledge policy and the execution policy evolve at different rates and for different reasons.

Symphony's specific innovation worth adopting: `WORKFLOW.md` uses YAML frontmatter for machine-readable config combined with a Markdown prompt body for agent instructions, and supports dynamic reload when the file changes ([github.com](https://github.com/openai/symphony/blob/main/SPEC.md)). Add this to Plexium's `WORKFLOW.md`:

```yaml
---
version: 1
tracker:
  kind: linear              # or: github-issues, none
  project: "PROJ"
polling:
  interval: 300s
  eligible_states: ["Backlog", "Todo"]
workspace:
  strategy: worktree         # git worktree per issue
  base_path: .plexium/workspaces/
agent:
  runner: codex              # or: claude, gemini, any
  timeout: 1800s
  max_concurrent: 3
retry:
  max_attempts: 3
  initial_delay: 5s
  backoff_multiplier: 2
  max_delay: 60s
handoff:
  success_state: "Human Review"
  failure_state: "Needs Triage"
---

# Autonomous Execution Policy

## Before Starting Any Issue
1. Read `.wiki/_index.md` for orientation.
2. Read relevant module/architecture/decision pages.
...
```

### 2. Per-Issue Isolated Workspaces

Symphony creates deterministic per-issue workspaces that persist across runs, with lifecycle hooks (`after_create`, `before_run`, `after_run`, `before_remove`) ([github.com](https://github.com/openai/symphony/blob/main/SPEC.md)). For Plexium, this addresses a real gap:

- **Heavy operations** (`plexium convert` on a 100K-line brownfield codebase) should not block or pollute the developer's working tree.
- **Parallel autonomous runs** need isolation to avoid stepping on each other.
- **Lifecycle hooks** map directly to Plexium operations: `before_run` → `plexium retrieve`, `after_run` → `plexium sync` + `plexium lint`.

Implementation: use **git worktrees** under `.plexium/workspaces/<ISSUE-ID>/`. Each worktree gets a fresh checkout. The agent works in the worktree, updates `.wiki/` pages, and the results are merged back to the main branch via PR.

The orchestrator should explicitly **sequence roles in order**: Retriever first (via PageIndex, to compile context) → Coder (implements the change) → Documenter (updates wiki pages) → Linter (validates integrity). This gives concrete shape to the abstract swarm pattern described in PRP §16 and prevents context overflow by keeping each role focused.

### 3. Exponential Backoff and Retry Policy

Your PRP's failure modes section (§18) handles failures well but **does not specify retry semantics.** Symphony's exponential backoff pattern should be adopted for:

- LLM API failures during `plexium convert` or `plexium sync`
- GitHub Wiki push failures (rate limits, transient auth issues)
- PageIndex server unavailability
- Issue tracker API failures (when configured)

Add to `.plexium/config.yml`:

```yaml
retry:
  maxAttempts: 3
  initialDelay: 5s
  backoffMultiplier: 2
  maxDelay: 60s
```

### 4. Handoff States

Symphony explicitly supports workflow-defined handoff states such as `Human Review` rather than binary done/not-done ([github.com](https://github.com/openai/symphony/blob/main/SPEC.md)). This pairs directly with Plexium's `review-status` frontmatter. Instead of "agent finishes and publishes," model it as: "agent finishes, updates wiki, opens PR, transitions to `Human Review`, and stops." This is a safer autonomy boundary than full auto-merge, especially for `co-maintained` and high-impact architecture pages.

### 5. Structured Observability for Live Operations

Your PRP has strong batch reporting (§13) but **no live operations surface.** For the daemon mode, adopt Symphony-style structured JSON logging:

```json
{
  "level": "info",
  "component": "orchestrator",
  "event": "run_completed",
  "issue": "PROJ-142",
  "page": "modules/auth.md",
  "duration_ms": 12400,
  "tokens_used": 8200,
  "wiki_pages_updated": 3,
  "retry_count": 0
}
```

This is materially different from Plexium's current batch reports and necessary for teams managing autonomous wiki maintenance at scale. The daemon should expose at minimum: running sessions, queued work, retry counts, token totals, and last-error per workspace.

### 6. Restart Recovery Without Persistent Database

Symphony recovers from restarts by reconciling against the issue tracker state — no database required ([github.com](https://github.com/openai/symphony/blob/main/SPEC.md)). Plexium already has this capability via `manifest.json` + `lastProcessedCommit`. The daemon should use the manifest as its reconciliation state: on restart, compare `lastProcessedCommit` against `git log`, identify unprocessed commits, and resume from there. No additional persistence layer needed.

### 7. Formalized Page State Machine

Symphony's issue lifecycle state machine (eligible → claimed → running → success/failed → released) is elegant. Plexium's wiki pages would benefit from a similar formalization. Currently the lifecycle is implicit in frontmatter fields (`review-status: unreviewed | human-verified | stale`), but it's not expressed as a state machine with explicit transition rules.

Formalize it:

```
stub → generated → unreviewed → human-verified → stale → regenerated
                                                    ↓
                                              needs-review (when source changes detected)
```

**Transition rules:**
- `stub → generated`: only by `plexium convert` or agent following schema
- `generated → unreviewed`: automatic on creation
- `unreviewed → human-verified`: only by human (PR review approval)
- `human-verified → stale`: automatic when source file hashes drift from manifest
- `stale → regenerated`: only by `plexium sync` or orchestrated agent run
- `regenerated → unreviewed`: automatic (re-enters review cycle)

This enables the daemon model: it can watch for pages entering `stale` state and automatically dispatch agents to regenerate them, creating the self-healing loop.

---

## What NOT to Adopt from Symphony

| Symphony Element | Why Not for Plexium |
|---|---|
| Linear-only issue tracker integration | Plexium must remain tracker-agnostic. If tracker support is added, abstract behind an adapter interface (Linear, GitHub Issues, Jira, none). |
| Per-issue workspace directories for wiki-only ops | Wiki operations don't need filesystem isolation from each other — they all target `.wiki/`. Workspaces are for coding+wiki combined runs. |
| Agent spawning via JSON-RPC stdio (Codex app-server) | Plexium delegates to whatever agent the developer is already using. The runner interface must be abstract. |
| Elixir runtime dependency | Stick with Go/Rust for single-binary cross-compiled distribution. Reimplement key orchestration patterns in the CLI's language. |
| High-trust deployment assumption | Symphony says it's for "trusted environments." Plexium's enforcement model (ownership, hooks, CI gates) is more mature — preserve it. |
| "Ticket writes performed by the agent" | Wiki writes blend deterministic pipeline + LLM augmentation. The manifest update is always deterministic. |

---

## Critical Architectural Change: Shared Files Under Concurrency

This is the most important structural issue that the PRP must address before Symphony-style parallelism is viable.

**The problem:** Under parallel Symphony-style runs, `_index.md`, `_Sidebar.md`, `_log.md`, and `Home.md` become merge-conflict hotspots. If three concurrent agents each update `_index.md`, you get a three-way merge conflict on every run.

**The solution:** Under autonomous/daemon mode, **make shared navigation files compiled deterministic outputs rather than direct agent write targets.**

- Agents update **module/decision/concept pages** and append to **per-run log files** (e.g., `.plexium/workspaces/<ISSUE>/run-log.md`).
- The **deterministic pipeline** (`plexium compile`) regenerates `_index.md`, `_Sidebar.md`, `Home.md`, and consolidates per-run logs into `_log.md` — either after each run completes or at merge time.
- `_log.md` entries from parallel runs are merged by timestamp (append-only, chronologically sorted — conflicts are trivially resolved).
- This compilation step is fast, deterministic, and reproducible — no LLM calls required.

Add a new command:

```bash
plexium compile    # Regenerate _index.md, _Sidebar.md, Home.md, _log.md from page state
```

This command is called automatically by the daemon after each run and by CI on merge. In non-daemon mode (standard developer workflow), the existing behavior where agents directly update navigation files remains fine — the concurrency problem only exists under parallel orchestration.

---

## A Novel Concept: Proof of Wiki-Integrity

Every autonomous PR should include a structured demonstration of how the `.wiki/` vault was mutated. This makes Plexium the **validation mechanism** that determines whether a PR is truly "Done":

```markdown
## Wiki Integrity Report
- **Pages updated:** modules/auth.md, architecture/overview.md
- **Pages created:** decisions/016-session-tokens.md
- **Contradictions resolved:** 2 (auth.md vs. overview.md session handling)
- **New cross-references:** 4 added, 0 removed
- **Files mapped:** src/auth/session.ts → modules/auth.md
- **Confidence:** high (all claims verified against source code)
- **Wiki debt:** 0 outstanding items
```

This report is generated by `plexium ci check` and posted as a PR comment. It transforms wiki maintenance from an invisible obligation into visible, reviewable proof of work — using the same philosophy Symphony applies to code changes ([heyuan110.com](https://www.heyuan110.com/posts/ai/2026-03-05-openai-symphony-autonomous-coding/)).

---

## Recommended PRP Amendments

### Amendment 1: Add Execution Plane to Core Architecture (§2)

Add a fifth layer to the architecture diagram:

```
┌──────────────────────────────────────────────────────────────┐
│                  Execution Plane (optional)                   │
│  WORKFLOW.md  •  Tracker Adapter  •  Orchestrator            │
│  Workspace Manager  •  Runner Adapter  •  Run Telemetry      │
│                                                              │
│  Opt-in daemon mode. Polls for work, dispatches agents,      │
│  manages concurrency/retries, and feeds results back.        │
│  The wiki is both the context input and the validation gate. │
└──────────────────────────────────────────────────────────────┘
```

### Amendment 2: Add `symphony` Agent Adapter (§4)

Add to the agent adapter table:

| Agent | Instruction File | Method |
|---|---|---|
| Symphony | `WORKFLOW.md` (injected section) | Plexium directives appended to orchestration contract |

### Amendment 3: Add `WORKFLOW.md` to Vault Structure (§3)

```
.plexium/
├── ...existing files...
├── WORKFLOW.md              # Orchestration/execution contract (optional)
├── workspaces/              # Per-issue git worktrees (daemon mode)
```

### Amendment 4: Add Retry Policy to Config (§8)

```yaml
retry:
  maxAttempts: 3
  initialDelay: 5s
  backoffMultiplier: 2
  maxDelay: 60s

daemon:
  enabled: false
  pollInterval: 300
  maxConcurrent: 2
  watches:
    staleness:
      enabled: true
      threshold: 7d
      action: auto-sync          # auto-sync | create-issue | log-only
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
  issueTracker:
    type: none                   # none | linear | github-issues
    project: ""
    labelPrefix: "wiki-"
```

### Amendment 5: Add New CLI Commands (§9)

| Command | Description |
|---------|-------------|
| `plexium daemon [options]` | Start the autonomous orchestration loop (opt-in) |
| `plexium compile` | Regenerate shared navigation files from page state |
| `plexium orchestrate --issue <ID>` | Run a single orchestrated wiki-update for one issue |

### Amendment 6: Add Operation 6 to Workflows (§10)

> **Operation 6: Autonomous Maintenance Daemon (The Symphony Loop)**
>
> **Goal:** Fix documentation drift asynchronously without human supervision.
>
> **Trigger:** `plexium daemon` (long-running process).
>
> **Process:**
> 1. Poll for triggers: new commits with unmapped changes, stale pages past threshold, WIKI-DEBT entries, new files in `.wiki/raw/`, lint findings above severity threshold.
> 2. Claim eligible work items (bounded by `maxConcurrent`).
> 3. Create isolated git worktree for each work item.
> 4. Sequence agent roles: Retriever (context via PageIndex) → Coder (if code changes needed) → Documenter (wiki updates) → Linter (validation).
> 5. On success: open PR with Wiki Integrity Report, transition to `Human Review` handoff state.
> 6. On failure: retry with exponential backoff, then release work item and log to `_log.md`.
> 7. After each run: `plexium compile` regenerates shared navigation files.

### Amendment 7: Formalize Page State Machine (§5)

Extend the ownership model with explicit lifecycle states and transition rules as described above.

### Amendment 8: Add Symphony Integration to Tool Integrations (§16)

> **OpenAI Symphony — Autonomous Execution Engine**
>
> While Plexium is the repository's persistent brain, [Symphony](https://github.com/openai/symphony) provides the autonomous execution nervous system.
>
> **Integration levels:**
> - **Tier 1 (recommended):** Inject Plexium's `_schema.md` directives into Symphony's `WORKFLOW.md`. Every Symphony-dispatched agent reads the wiki before working and updates it after. Zero architectural changes.
> - **Tier 2 (Phase 3+):** `plexium daemon` implements Symphony-style polling, workspace isolation, and retry/reconciliation using Plexium's own `manifest.json` as the state layer.
> - **Tier 3 (future):** Full bidirectional integration where Plexium lint findings auto-generate tracker issues and Symphony dispatches wiki-update agents.
>
> **Ecosystem note:** Community forks like Stokowski (Claude-compatible) suggest the orchestration ecosystem is diversifying beyond Codex. Plexium's runner interface should remain abstract.

### Amendment 9: Update Phase 3 Scope (§22)

Add to Phase 3:
```
- WORKFLOW.md generation and loader
- `plexium daemon` command (opt-in continuous mode)
- `plexium compile` command (shared file generation)
- Per-issue git worktree workspace management
- Staleness/debt/ingest watchers with configurable thresholds
- Bounded concurrency for parallel operations
- Retry policy with exponential backoff
- Structured JSON logging for daemon mode
- Wiki Integrity Report generation for autonomous PRs
- Tracker adapter interface (Linear first, GitHub Issues second, none as default)
- Runner adapter interface (agent-neutral)
```

### Amendment 10: Add Open Design Question #11

```
11. **Daemon vs. CI-only:** Should Plexium support a long-running daemon mode 
    (Symphony-style continuous polling) for proactive wiki maintenance, or is 
    CI + git hooks sufficient? If daemon, should it be bundled in the CLI binary 
    or a separate deployable service? How does the daemon handle in-flight runs 
    during schema migrations (`plexium migrate`)?
```

---

## Addressing Scaling and Operational Concerns

### Token Costs Under Parallel Orchestration

The PRP's current token cost analysis (§21) assumes serial sessions. Under Symphony-style parallel orchestration with `maxConcurrent: 3`, three agents simultaneously reading `_index.md` + relevant pages triples the token consumption per unit time. Mitigations:

- **PageIndex becomes essential, not optional.** Targeted retrieval via PageIndex costs far fewer tokens than each agent reading the full index.
- **Cache wiki reads across concurrent runs** when the wiki hasn't changed between dispatches.
- **Budget configuration:** Add `daemon.tokenBudgetPerRun` and `daemon.tokenBudgetPerHour` to config, with the daemon pausing dispatch when limits are hit.
- **Prioritize deterministic operations.** Many daemon tasks (staleness detection, link validation, manifest reconciliation) require zero LLM tokens.

### GitHub Wiki Submodule Under Concurrent Pushes

The git submodule architecture assumes serial pushes to `{repo}.wiki.git`. Under parallel autonomous operation, concurrent pushes will fail. Solutions:

- **Serialize wiki publishes.** The `plexium compile` + `plexium publish` steps should be a single-writer operation, either via a lock file or by only publishing on merge to main (not per-worktree).
- **Batch publishing.** Instead of publishing after each autonomous run, collect changes and publish once per CI cycle or daemon poll interval.
- **Consider the sync-and-push model** as the default for daemon mode, with the submodule approach reserved for non-daemon workflows. This partially addresses Open Design Question #6.

### Testing the Orchestration Layer

The daemon/orchestrator needs its own test strategy:

- **Mock issue tracker adapter** for integration tests (returns synthetic issues without live Linear/GitHub API).
- **Mock runner adapter** that simulates agent completion with predefined wiki page outputs.
- **Deterministic replay:** Record orchestrator decisions (claim, dispatch, retry, release) as structured logs that can be replayed and asserted against.
- **CI without live dependencies:** The orchestrator's core state machine (polling, claiming, concurrency bounds, reconciliation) should be testable with in-memory fakes. No external services required.
- Add acceptance criteria: "Orchestrator state machine can be tested with mock tracker and mock runner, achieving 100% state transition coverage without live API calls."

### Licensing Considerations

Symphony is Apache 2.0 licensed ([github.com](https://github.com/openai/symphony)). If Plexium reimplements Symphony's patterns in Go/Rust (rather than importing Elixir code), this is a clean-room reimplementation of publicly specified behavior — the SPEC.md is a language-agnostic specification explicitly designed to be reimplemented. No licensing conflict arises from implementing a published specification. The Plexium CLI should include an attribution notice in `NOTICE` or `THIRD_PARTY_NOTICES` referencing Symphony's spec as design inspiration, per Apache 2.0 norms.

### Developer Experience Impact

Adding orchestration complexity risks diluting the "give it your repo, it builds a wiki" pitch. The key discipline:

- **The core pitch doesn't change.** `plexium init` + `plexium convert` works exactly as specced. No daemon, no tracker, no orchestration. Just "give it your repo."
- **Orchestration is a progressive disclosure.** Teams that want more autonomy opt into `plexium daemon` after they've proven the core workflow. It's Phase 3 for a reason.
- **Marketing framing:** "Plexium builds your wiki. With daemon mode, it *maintains* it forever."
- **The CLI remains the atomic building block.** Every daemon operation composes existing CLI commands (`sync`, `lint`, `compile`, `publish`). The daemon is a scheduler, not a new system.

### Schema Migrations During Active Orchestration

When `plexium migrate` bumps `schema-version` in `_schema.md`, in-flight daemon runs may be operating under the old schema. Handle this the same way database migrations handle active connections:

- **Drain before migrate.** `plexium migrate` first signals the daemon to stop claiming new work, waits for in-flight runs to complete (with a configurable timeout), applies the migration, then resumes the daemon.
- **Version assertion.** Each daemon run checks `schema-version` at start. If it doesn't match the version the run was dispatched under, abort and re-queue.
- **Non-breaking migrations** (adding optional frontmatter fields) don't require drain.
- **Breaking migrations** (renaming sections, changing required fields) require drain + rebuild.

---

## Bottom Line

| Integration Tier | Viability | Timing | Effort |
|---|---|---|---|
| **Tier 1:** Symphony agents read/write Plexium wiki via `WORKFLOW.md` | ✅ High | Now (Milestone 5) | Trivial — one adapter plugin |
| **Tier 2:** `plexium daemon` with Symphony-style polling, workspaces, retries | ✅ High | Phase 3 (design now) | Moderate — new command + orchestrator |
| **Tier 3:** Full bidirectional Symphony ↔ Plexium integration | ⚠️ Premature | Phase 4+ (if ever) | High — tight coupling risk |

**Plexium remains the persistent brain. Symphony becomes the optional execution nervous system.** The lightest-touch integration (Tier 1) delivers the most value per unit of effort. The daemon mode (Tier 2) is architecturally sound but must remain opt-in, designed as a composition of existing CLI commands, and gated behind the concurrency fixes (compiled shared files, serialized publishing) outlined above.

The PRP needs six targeted amendments (Execution Plane, `WORKFLOW.md`, retry policy, daemon config, `plexium compile`, and the page state machine) — not a redesign. The existing architecture absorbs Symphony's patterns cleanly because both systems share the same fundamental conviction: **the repository is the system of record, and in-repo policy files are how you govern autonomous agents.**