# LLM Wiki V3 Alignment — Segmentation Enhancement Plan

> **Status:** Planning artifact. Drives a future coding session as backlog clears.
> **Companion to:** `docs/reference/llm-wiki-ref.md` (Karpathy's V1 reference).
> **Source concept:** LLM Wiki V3 "Segmentation" — https://gist.github.com/ahumanft/6c96385be6ca4af578cc9b20e0f79e66
>
> Part 1 archives the V3 concept and maps it against Plexium today. Part 2 is the prioritized,
> code-grounded enhancement backlog (six workstreams), written to become Linear tasks.

---

## Part 1 — Context & the V3 concept

### Why this exists

Plexium already implements Karpathy's **LLM Wiki V1** and several **V2** (rohitg00) mechanisms. The new
**LLM Wiki V3 "Segmentation"** concept formalizes a pattern Plexium has *already partly stepped on*: a
role-segmented library with a **librarian**. This document captures V3, maps it against the current
implementation, and lays out a prioritized backlog to meet and exceed it.

> **State correction:** the repo's `CLAUDE.md` still calls the project *greenfield*. It is not.
> `docs/phases/OVERVIEW.md`'s build log shows Phases 0–4, 6, 8, 9, 10 **complete** (Phases 5 and 7
> pending), with a compiled `plexium` binary, ~25 `internal/` packages, and 86 test files. These
> enhancements **extend existing Go packages**, not unbuilt phases. (Fixing the stale `CLAUDE.md`
> line is a tracked follow-up.)

### V3 in one page

Thesis: **"Everything is stateless. Design around it."** The context window is a temporary working
surface; tokens go cold as it fills. Don't build systems that hold everything — build systems that
**keep the right things warm at the right moment**. The mechanism is **segmentation**: split every
component (ingestion, schema, roles, retrieval, lint) into narrow, purpose-built units so the LLM
executing any one of them never drifts.

Three segmented library roles:

- **Ingestor** — classification only. Reads *just enough* (title + first lines), writes a 3–5 sentence
  summary, tags, categorizes. Does not synthesize or cross-reference. Optimizes for *findability*, not depth.
- **Librarian** — retrieval only. Searches by title/summary/tags, **tracks checkout/access frequency**
  (frequently-pulled docs become candidates for deeper indexing — "crystallization"), **routes** queries
  but does **not answer** them. Critically, it **runs in its own isolated context**: it searches, filters,
  and scopes there, then hands the requesting team clean pre-scoped material *alongside the original
  objective*, so the team never inherits the searcher's navigational **drift/bias**. Sometimes **two
  librarians run in parallel** — one for local knowledge, one for web.
- **Linter** — collection health. Deduplicates, flags stale entries, and **detects the same document
  filed under different titles**. Runs on its **own schedule**, not as part of every query.

Two cross-cutting ideas:

- **Cache rewarming** — after a number of turns or a token threshold, inject a brief recap (what was
  retrieved, why it was relevant, what the team is building toward) so long sessions don't lose the
  thread. This mirrors Claude Code's automatic compaction.
- **Explicit schema + implicit charters** — keep the schema *minimal and structural* (explicit trigger →
  defined action → defined output; the model executes rather than interprets). Carry judgment and culture
  in separate **charter files invoked on demand** at the right workflow seam (e.g. a librarian reads
  `how-to-find-context-that-isnt-obvious.md` *only after* the first retrieval pass). Implicit guidance
  loaded passively goes cold and drifts — usually right when it matters most.

V3 is **intentionally incomplete**. Its stated open problem is **how segments communicate and hand off
at their boundaries**.

### Where Plexium already stands (V3 ↔ current code)

| V3 concept | Plexium today | Status | Code |
|---|---|---|---|
| **Ingestor** | Phase 2 taxonomy classifier + Phase 4 convert pipeline; `--depth shallow/deep`; size limits; stub pages | Strong | `internal/convert/`, `internal/generate/`, `roles.RoleIngestor` |
| **Linter** | Phase 6 deterministic lint (links, orphans, staleness, manifest/sidebar/frontmatter) + `lint/llm.go`; slug-dedup at generation time | Partial — no same-content / near-dup detection | `internal/lint/*` |
| **Librarian** | PageIndex hierarchical search + MCP server + Retriever fallback chain; `roles.RoleRetriever` (write-empty); roles sequenced Retriever→Coder→Documenter→Linter in **one session** | Partial — no isolated handoff, no access tracking, not parallel | `internal/integrations/pageindex/*`, `roles.go`, `internal/daemon/upkeep.go` |
| **Cache rewarming / recap** | None (only token-cost notes in arch §10) | Gap | — |
| **Explicit schema + charters** | Monolithic prescriptive `_schema.md`, injected per-agent | Differs | arch §3, `internal/prompts`, `internal/plugins/` |
| **Provider tiering** | `ProviderCascade` (Ollama→OpenRouter→inherit), `TaskRouter`, `RateLimitTracker`, capability profiles | Strong (cascade, not parallel) | `internal/agent/*`, `internal/capabilityprofile/` |
| **Deterministic-first** | `compile` regenerates nav deterministically; `manifest.Save` sorts pages | A constraint on new work | `internal/compile/`, `internal/manifest/manifest.go` |

**Net:** Plexium already exceeds V3 on the ingestor, provider tiering, and deterministic discipline. The
real gaps are the four prioritized below plus the headline librarian-isolation change.

### Decisions locked

1. **Deliverable** = prioritized enhancement plan mapped onto existing packages (not a new phase doc).
2. **Librarian** = go all the way to a **true isolated-context librarian** (separate dispatch returning
   clean scoped material; the team never sees the search). This is the headline architectural change.
3. **Schema explicit/implicit refactor** = **document the pattern and trigger points only**; defer the
   restructure.
4. Prioritize **all four** gaps: access-pattern tracking, same-content dedup lint, session recap /
   cache rewarming, and parallel local + web librarians.

### Two pre-existing packages worth noting

- **`internal/capabilityprofile/profile.go`** — a small enum (`ConstrainedLocal` / `Balanced` /
  `FrontierLargeContext`) consumed by `config.Validate()`. **Reuse it as-is** to select the librarian's
  provider tier; no change required.
- **`internal/regen/regen.go`** — a standalone page regenerator (`BuildRegenPrompt` → `Cascade.Complete`
  → write file) that deliberately does **not** import `internal/daemon`. **This is the architectural
  template for the isolated librarian:** a self-contained, cascade-driven, single-purpose unit that
  returns a typed result.

---

## Part 2 — Prioritized enhancement workstreams

Each workstream is written to become one or more Linear tasks. Effort and dependencies are noted.

> **Load-bearing invariant across all workstreams:** all *mutable/volatile* state (access counts, recap
> counters) lives in a **separate `.plexium/*-state.json` file** modeled on `RateLimitTracker`'s
> `agent-state.json` — **never** in `manifest.json`. `manifest.Save` sorts pages for byte-deterministic
> output, and `compile` / `staleness` must stay deterministic.

### WS1 — Access-pattern tracking *(foundation; build first)*

**Goal:** track per-page access/checkout frequency and last-accessed time, so the librarian can rank by
usage ("crystallization") and the linter can flag never-accessed pages.

- **New package `internal/access/access.go`**, modeled on `internal/agent/ratelimit.go`:
  - State file `.plexium/access-state.json` (helper `DefaultPath(repoRoot)` mirroring `manifest.DefaultPath`).
  - Types: `Tracker{stateFile; mu sync.Mutex}`, `State{Version; Records map[string]*PageAccess}`,
    `PageAccess{Retrievals, Checkouts int; LastAccessed, FirstAccessed string}` keyed by `WikiPath`.
  - Methods: `NewTracker`, `Load`, `Save` (**atomic temp-file + rename**, copying `saveStatusSnapshot` in
    `internal/daemon/status.go` rather than `ratelimit.go`'s direct write), `RecordRetrieval([]string)`,
    `RecordCheckout([]string)`, `Top(n)`, `Get(wikiPath)`.
- **Write sites (in-process only):** the librarian dispatcher (WS2) records retrievals for every hit it
  surfaces and checkouts for the subset it hands off; `internal/daemon/upkeep.go` `buildContextPages`
  (~line 634–661) wraps its `retriever.Retrieve` call to record. **Do not** mutate state inside
  `pageindex.Retriever.Retrieve` — it is a pure read used by tests and the MCP server. MCP-server
  recording (`server.go` `callSearch`) is config-gated so the read-only `plexium retrieve` CLI can opt out.
- **Surfacing:** keep `compile` output a pure function of the manifest — **do not** let access counts
  affect `_index.md` / `_Sidebar.md` ordering. Surface counts via a new deterministic
  `plexium access report` (`.plexium/reports/access.md`) and feed `Top(n)` into librarian ranking.
- **Config:**
  ```yaml
  librarian:
    accessTracking: { enabled: true, stateFile: .plexium/access-state.json }
  ```
- **Tests:** `internal/access/access_test.go`, table-driven, mirroring `ratelimit_test.go`: counts;
  `Top` ordering with ties broken by `WikiPath`; concurrent-goroutine atomic save; missing-file → zero
  state. Plus a determinism guard: `compile.Compile` output byte-identical with and without
  `access-state.json` present.
- **Effort:** Low–Medium. **Dependencies:** none. Confirm `.plexium/access-state.json` is gitignored (as
  `agent-state.json` already is).

### WS2 — True isolated-context librarian *(headline)*

**Goal:** retrieval runs as a separate invocation in its **own context**, scopes there, and returns clean
pre-scoped material plus the original objective. The requesting team never inherits the searcher's drift.

- **New package `internal/librarian/librarian.go`** (sibling-in-spirit to `internal/regen/`; **must not
  import `internal/daemon`** — the daemon imports it).
- **Formalize the role** in `internal/integrations/roles/roles.go`: add `RoleLibrarian Role = "librarian"`
  to the const block and `AllRoles()`; add a `Registry` entry with `CanRead: [".wiki/**", ".plexium/**"]`
  and `CanWrite: [".plexium/access-state.json"]` — it reads everything but writes only telemetry, never
  wiki content (encoding "retrieval only, routes-not-answers" structurally).
- **Hand-off contract:**
  ```go
  type ScopedResult struct { Objective, Source, Rationale string; Pages []ScopedPage }
  type ScopedPage   struct { Path, Title, Summary, WhyRelevant string; Relevance float64 }
  ```
  Built from `pageindex.PageHit`; deterministic order `(-Relevance, Path)`. `Objective` is echoed back
  verbatim — the team always gets the objective alongside the scope.
- **Two dispatch modes (config `librarian.mode`):**
  1. **`in-process`** (deterministic default / stepping stone): a pure-Go run of
     `pageindex.Retriever.Retrieve` → `ScopedResult`. "Isolation" here means the team only ever sees the
     `ScopedResult`, never the search transcript. Zero token cost, fully deterministic.
  2. **`separate-process`** (the true-V3 target, decision #2): dispatch a dedicated
     `RunnerAdapter.Run(ctx, "librarian", librarianPrompt, nil, workdir)` — a fresh `claude`/`codex`/
     `gemini` **process with its own context window** (`exec.CommandContext`, `runner.go:115`). The prompt
     instructs: "search for material relevant to OBJECTIVE; return ONLY a JSON `ScopedResult` (page paths
     + one-line why-relevant each + the restated objective); do **not** answer the objective." The daemon
     parses the JSON (reusing the `providerExecutionPlan` JSON parse in `runProviderPrimary`) and passes
     **only** the parsed `ScopedResult` downstream — the team process physically cannot inherit the search
     context.
- **Integration** in `internal/daemon/upkeep.go`: `buildContextPages` (~line 634) is wrapped by
  `librarian.Scope(ctx, objective, …)` returning a `ScopedResult`; the prompt builders
  `buildRunnerJobPrompt` / `buildProviderJobPrompt` take the `ScopedResult` (embedding `Objective` and
  per-page `WhyRelevant`) instead of a bare `[]string`. The librarian records WS1 telemetry.
- **Config:**
  ```yaml
  librarian:
    enabled: true
    mode: in-process        # in-process | separate-process
    runner: claude          # used when mode=separate-process
    maxPages: 5             # matches the current buildContextPages cap
  ```
  Add a `Librarian LibrarianConfig` field to `config.Config` (`internal/config/config.go:15`) plus a
  `config.Validate()` stanza defaulting `mode: in-process` and validating `runner` against the known
  runner set (mirror the `daemon.executionMode` validation).
- **Tests:** `internal/librarian/librarian_test.go`: in-process mode against a temp wiki (reuse pageindex
  fixtures) — objective echoed verbatim, results capped at `maxPages`, deterministic order, no search
  artifacts. Separate-process mode via a fake `RunnerAdapter` (like `NoOpRunner`) returning canned JSON —
  assert parsing and that the downstream documenter prompt contains only scoped pages + objective.
- **Effort:** High. **Dependencies:** WS1 (telemetry; degrades to a no-op if disabled). Foundation for WS3 and WS4.

### WS3 — Session recap / cache rewarming *(builds on WS2)*

**Goal:** within a long job/session, inject a recap so scope doesn't go cold.

- **New `internal/daemon/recap.go`:** `SessionRecap{Objective, RetrievedPages, WhyRetrieved,
  BuildingToward string; TurnsSinceWarm, TokensSinceWarm int}` + `BuildRecapPrompt(SessionRecap) string`
  (a pure builder, mirroring `regen.BuildRegenPrompt`).
- **Trigger at existing phase boundaries** in `upkeep.go` (`jobPhaseRetrieving` / `Planning` /
  `Documenting` / `Validating` / `Applying`): on entering a phase, if `TurnsSinceWarm >= N` or
  `TokensSinceWarm >= T`, prepend the recap (via `buildPrompt`) and reset counters. Persist counters on
  `DaemonJobSnapshot` (`internal/daemon/status.go:60`, `omitempty`) via the existing `persistJobPhase`.
- **Caveat:** CLI runners report `RunResult.TokensUsed = -1` (`runner.go:75`), so token-threshold
  rewarming only works in provider-primary mode; for coding-agent (CLI) mode, use the turn/phase-count
  trigger.
- **Config:**
  ```yaml
  daemon:
    rewarming: { enabled: true, everyNTurns: 6, tokenThreshold: 40000 }
  ```
- **Tests:** `internal/daemon/recap_test.go` (`BuildRecapPrompt` contents) plus a daemon test
  (`NoOpRunner`) asserting the recap appears once the counter crosses N, is absent before, and that
  counters reset.
- **Effort:** Medium. **Dependencies:** WS2 for `WhyRetrieved` / `RetrievedPages` provenance (degrades
  gracefully — the recap still emits the objective and context pages without it).

### WS4 — Parallel local + web librarians *(builds on WS2)*

**Goal:** run two librarians concurrently (local PageIndex knowledge + web), then merge the scoped results.

- **In `internal/librarian/`:** a `Source` interface — `Search(ctx, objective) (*ScopedResult, error)` +
  `Name()` — with a **local** source (wrapping WS2 retrieval) and a **web** source (an isolated
  `RunnerAdapter` call; the CLI runners have web access — or a future `plugins.RetrievalPlugin`). Web
  defaults **off**.
- **Concurrency model (Go):** `RunParallel(ctx, objective, sources, timeout)` — `context.WithTimeout`, one
  goroutine per source, a buffered results channel, a `sync.WaitGroup`, then a `mergeScoped` step that
  imposes a **total order `(sourceName, -relevance, path)` and de-dups**, so goroutine completion order
  never leaks into output (keeping daemon jobs reproducible). A slow web source degrades to local-only via
  the timeout (matching the existing PageIndex → index-scan → grep fallback ethos). Both run *inside* one
  job, so they don't consume worktree budget.
- **Integration:** `buildContextPages` builds `[]Source{local, web}` when `librarian.web.enabled`,
  otherwise local only; the rest of `executeJob` is unchanged.
- **Config:**
  ```yaml
  librarian:
    parallel: { enabled: false, timeout: 20s }
    web:      { enabled: false, runner: claude, maxResults: 5 }
  ```
- **Tests:** `internal/librarian/parallel_test.go` with fake sources: one slow-past-timeout → only the fast
  result merged; both succeed → deterministic merged order across repeated runs; an erroring source →
  others still return.
- **Effort:** Medium–High. **Dependencies:** WS2 (`Source` / `ScopedResult` / isolated dispatch).

### WS5 — Same-content dedup lint *(linter gap; independent)*

**Goal:** detect the same document filed under different titles/paths.

- **New `internal/lint/dedup.go`** (following the pattern of `staleness.go` / `orphans.go`):
  `NewDuplicateDetector(wikiRoot, mgr).Detect()`. Deterministic detection via **content hashing of a
  normalized body** — strip frontmatter, lowercase, collapse whitespace, drop wiki-link decorations, then
  `manifest.ComputeHashString` (`hash.go:26`). Excluding the title/frontmatter is what catches "same
  content, different title." Exact hash collisions → a group; **near-duplicates** via a cheap deterministic
  shingle / token-set **Jaccard** above a configurable threshold. Groups sorted by smallest `WikiPath`,
  members by `WikiPath` (honoring the §8 "deterministic deduplication" invariant). Skip `raw/` and
  `_`-prefixed files (as `LLMAnalyzer.loadAllPages` does).
- **Wire into `internal/lint/lint.go`:** add `DuplicateReport{Group []string; Reason, Severity string}`
  and a `Duplicates []DuplicateReport` field (`omitempty`) to `DeterministicReport`; add a step in
  `RunDeterministic` after `checkFrontmatter`; count duplicate groups as warnings in `summarize`.
- **Optional LLM semantic dedup** in `lint --full`: a `detectSemanticDuplicates` pass in
  `LLMAnalyzer.Analyze` (`llm.go`), a new `prompts.PromptDuplicates` asset, advisory-only (never
  auto-deletes — preserving "never delete unmanaged pages" and the human-review model).
- **"Own schedule" (V3):** duplicates flow through the existing lint watch; give the watch independent
  cadence by honoring `WatchEntry.Interval` in `detectLintJob` (currently parsed but unused) — skip
  queuing if the last lint tick was within the interval (persisted in the status snapshot).
- **Config:**
  ```yaml
  daemon:
    watches:
      lint: { enabled: true, interval: 24h, dedup: { enabled: true, nearDupThreshold: 0.85, semantic: false } }
  ```
- **Tests:** `internal/lint/dedup_test.go` (mirroring `orphans_test.go`): identical body / different titles
  → one exact group; near-dup above/below threshold; `raw/` and `_`-prefixed excluded; run-twice
  determinism. LLM pass via a fake `LLMClient` (as in `llm_test.go`).
- **Effort:** Medium. **Dependencies:** none (deterministic pass first; LLM pass later).

### WS6 — Explicit/implicit schema pattern *(document only — deferred, decision #3)*

**No code this round.** Add a design section to `docs/architecture/core-architecture.md` (a new §3.x
under "The Universal Schema") and reference it from the generated `_schema.md` template
(`internal/prompts` assets). Document:

- **Minimal explicit structural schema** (stays always-on): the frontmatter spec, ownership states,
  section taxonomy, and link rules.
- **Implicit charters loaded on demand** (e.g. an ADR-authoring charter, an ingest charter, a lint
  charter) with **explicit triggers keyed to existing `jobPhase*` / `jobType*` moments** in `upkeep.go`:
  the ingest charter triggers on `jobTypeRawIngest` at `jobPhaseRetrieving`; the ADR charter on a
  decisions-section write at `jobPhasePlanning`; the lint charter on `jobTypeLint`. The librarian (WS2) is
  the natural charter loader — it can attach the relevant charter to the `ScopedResult` when the objective
  matches a trigger.
- Roll-out via the existing schema-migration mechanism (arch §"Schema Migration": drain-before-migrate,
  version assertion).

**Effort:** Low (docs).

---

## Cross-cutting determinism / philosophy tensions

1. **Access tracking vs `compile` determinism** → isolate in `access-state.json`; add a byte-identical
   `compile` test with and without it present; never feed nav ordering.
2. **Librarian "judgment" vs "no LLM for structural work"** → keep scoring deterministic (`pageindex` is
   pure math); use the LLM only in `separate-process` mode and only for rationale/why-relevant — never to
   author wiki content or mutate the manifest. `mode: in-process` is the deterministic default.
3. **Concurrent writes under `maxConcurrent > 1`** → atomic temp-file + rename plus a `sync.Mutex` for
   `access-state.json` and recap counters; add no new `manifest.json` writers.
4. **Dedup vs the "deterministic deduplication" invariant** → the hash/Jaccard pass honors it; the LLM
   semantic pass is advisory-only.
5. **Parallel-librarian non-determinism** → `mergeScoped` imposes a total order before hand-off.

## Recommended build order

**WS1 (access) → WS2 (isolated librarian) → WS3 (recap) + WS4 (parallel) in parallel → WS5 (dedup,
independent) → WS6 (docs, anytime).**

## Critical files (for the execution session)

- `internal/daemon/upkeep.go` — `buildContextPages`, `runCodingPrimary` / `runProviderPrimary`,
  `buildRunnerJobPrompt` / `buildProviderJobPrompt`: the retrieval + role-sequencing seam where WS2/WS3/WS4 integrate.
- `internal/integrations/pageindex/retrieve.go` — `Retriever.Retrieve` / `PageHit`: the local librarian core.
- `internal/agent/ratelimit.go` — `RateLimitTracker` / `agent-state.json`: the template for `access-state.json`.
- `internal/regen/regen.go` — the architectural template for the self-contained librarian package.
- `internal/lint/lint.go` (with `staleness.go`, `llm.go`) — where the dedup checks slot in.
- `internal/config/config.go` — `Config` / `Validate`: the new `librarian` block plus rewarming/dedup keys.
- `internal/integrations/roles/roles.go` — add `RoleLibrarian`.

## Verification (for the execution session)

- Per-package table-driven tests following the patterns above.
- A determinism-guard test for `compile` (WS1) and for dedup ordering (WS5).
- End-to-end: `plexium retrieve "<query>"` increments access state; dedup lint flags a fixture page
  duplicated under two titles; a daemon tick with the `separate-process` librarian yields a coder prompt
  containing only the `ScopedResult` (no search transcript).

## Follow-ups

- Fix the stale **"greenfield"** line in `CLAUDE.md` (the project is actually Phases 0–10 mostly built).
- Open the six workstreams as Linear tasks in the recommended build order.
