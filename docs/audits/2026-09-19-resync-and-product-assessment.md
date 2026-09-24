# Plexium and MarkedUp: resynchronization and product assessment

Audit date: September 19, 2026, America/Los_Angeles (September 20 UTC).

Plexium baseline: `ed2a09b0561854073c312928786ba846e9df75bc`, also GitHub `main`.

MarkedUp published baseline: `0c5745b5a98610e01f4d358fee089a90aeafd6a2`.

MarkedUp local test baseline: `4513879`, the unmerged keyring-test fix in PR #156.

Audit tracking: `Plexium-eal`.

## Executive assessment

Plexium has a substantial working implementation. Its Go CLI, conversion pipeline, manifest, deterministic navigation generation, linting, source-change detection, regeneration, agent integrations, and MarkedUp plugin are real. The full Plexium test suite passes at the audited commit, with 64.1% statement coverage. This is a foundation worth retaining. However, passing component tests currently overstates the reliability of the complete knowledge-maintenance experience. Live probes found stale retrieval, a default sync operation that clears source-hash staleness without changing documentation, and mismatched storage/retrieval contracts in the MarkedUp integration.

The project stopped between implementation and product validation. The main status document predates much of the implementation; both issue trackers lag behind shipped code; the next-generation plan is an open draft PR; two important May decision documents remain untracked locally. MarkedUp has a broader standalone product surface than its description as a Plexium plugin suggests, but neither repository currently has GitHub release artifacts or tags in the queried listings. The local MarkedUp branch contains an unmerged testing fix and pre-existing modifications, so it must not be conflated with published `main`.

The recommended direction is **a reliable, portable knowledge service for coding agents, initially proven locally and then across a small collection of repositories**. Preserve repository-owned artifacts and make shared indexing and hosting optional deployment choices. Do not start with a SaaS rewrite or a new multi-agent workbench. First demonstrate that maintained knowledge improves real tasks over current agents with their normal search and memory tools. Hosting should follow evidence of team coordination, shared access, and operational needs that customers will pay to remove.

## Scope and confidence

This combines the five phases of `project-resync` with a product and architecture assessment. It includes local code and documentation, git history and working-tree state, Linear, Beads, GitHub PRs and CI, tests, isolated CLI/MCP probes, and current primary-source product documentation.

Evidence labels used below:

- **Verified:** directly reproduced, or confirmed against live GitHub/Linear state.
- **Code inspection:** observed implementation behavior without exercising every production path.
- **Recommendation:** a proposed direction or acceptance criterion, not a completed feature or measured result.

No source implementation, user configuration, existing stash, or existing MarkedUp checkout was changed for this audit. Probes used disposable repositories. MarkedUp tests used an archive of its committed local HEAD. No live paid-model benchmark, destructive migration, end-to-end GitHub Wiki publication, or deployed multi-user test was performed. An exhaustive security audit and vulnerability scan are outside the evidence gathered here; `govulncheck` was not available. Current product documentation establishes available offerings, not market share or customer adoption rates.

The earlier bounded memory search did not recover useful Plexium-specific continuity. Native Linear and GitHub CLI provided the external context; the skill's legacy Core-Memory integration gateway was not available. This did not block the assessment.

## Where work actually stopped

### Plexium timeline

| Period | Recovered work | Interpretation |
|---|---|---|
| Early April | Core CLI, wiki generation, lint, validation, publishing, and initial hardening | The original engine was built, not merely planned. |
| April 7–13 | Provider setup, MCP/Memento onboarding, agent adapters, UX, and publishing fixes | Attention moved to usability and real installations. |
| April 15–16 | Content regeneration and sync CI flag; worktree cap | Drift repair and daemon safety were active pain points. |
| April 16–19 | Plugin interfaces, MarkedUp enrichment, retrieval, embeddings, and daemon wiring | Architecture expanded beyond a standalone wiki generator. |
| May 26 | PR #26 preserved semantic relationships after the MarkedUp dependency bump | Cross-project schema compatibility was already a concrete risk. |
| May 26 | Draft PR #27 proposed LLM Wiki V3 segmentation work | This is a proposal, not shipped functionality. |
| May 26–31 | Local release-strategy and PageIndex migration reports | Valuable decisions exist outside committed history. |

GitHub `main` still matches the local Plexium source baseline. The open PR is [#27, LLM Wiki V3 alignment enhancement plan](https://github.com/Clarit-AI/Plexium/pull/27), marked draft, with one documentation file. The last 20 merged PRs cover onboarding through MarkedUp integration; there are no open GitHub issues in the queried Plexium listing.

Local branches include earlier OAuth, documentation, regeneration, and hardening work. Several are not ancestors of HEAD, but squash merging means that fact alone does not establish unfinished work. Two Plexium stashes remain: one includes worktree/gitignore changes and instructions, the other a binary change. Preserve and compare them before any cleanup; neither was applied or deleted.

### MarkedUp timeline and boundaries

MarkedUp's current published `main` ends April 19. Its late development concentrated on extraction reliability, NuExtract/Triplex parsing, fallback execution, semantic relationship separation, configuration, keyring behavior, and a setup wizard. Twenty recent merges were examined, including #124, #127, #143, #146, #147, #149, #150, and #154.

Two PRs remain open:

- [#156: disable native keyring access in tests](https://github.com/Clarit-AI/markedup/pull/156), matching local committed HEAD.
- [#155: TUI ASCII art asset](https://github.com/Clarit-AI/markedup/pull/155).

Fourteen open GitHub issues include the Tier-1-before-fallback write crash window (#145), endpoint capability fallback (#144), parser edge cases (#139), fenced-code wikilinks (#125), provenance dedup (#123), real-model validation (#136), wizard behavior, and release signing (#138). Twelve local stashes and pre-existing modified markdown/test fixtures remain. These were inventoried, not cleaned up.

### Trackers versus reality

| Source at audit start | Observed state | Assessment |
|---|---|---|
| Plexium Linear project | 18 issues: 17 open, 1 done; no assignee returned on these project issues | Not a trustworthy implementation inventory. |
| MarkedUp Linear project | 12 issues: 6 done, 2 in progress, 2 todo, 2 backlog | Better populated, but still behind implementation in places. |
| Plexium Beads | 16 open, 0 closed; mostly original milestones and CLI tasks | Historical planning state, not proof all those features remain unbuilt. |
| Plexium project status doc | Last validated April 6 | Predates regeneration and the entire MarkedUp integration. |

Specific reconciliation candidates:

- **KHA-274, worktree spawning:** cap and cleanup safeguards exist in `internal/daemon/workspace.go` and later daemon work. The original report mentions 308 worktrees/41 GB; verify recovery behavior before closing, but do not reimplement the already-shipped cap.
- **KHA-299, release strategy:** a local May 26 decision chooses a pinned dependency and compatibility tests. Linear still says backlog. Preserve the decision and distinguish the decision from its uncompleted test/release follow-ups.
- **KHA-289:** the local decision proposes superseding it with KHA-299 and KHA-297. No automatic tracker closure was performed.
- **KHA-259:** marked done for the plugin foundation. This is compatible with **KHA-297** remaining open for distribution/lifecycle; compiled-in plugins do not equal an external marketplace.
- **KHA-254:** May 31 PageIndex assessment exists locally. Implementation remains future work.
- **KHA-266:** safe transcript ingestion remains a material gap, corroborated by code inspection.
- **MarkedUp KHA-278/KHA-279:** reasoning tool and config code exist despite in-progress/backlog states. Reconcile against acceptance criteria and published code.
- **KHA-298:** the May compatibility fix demonstrates some integration work; it does not establish that the intended llm-dev-council field test completed.

MarkedUp's returned assigned issues include KHA-278 (in progress), and completed KHA-283, KHA-284, KHA-281, KHA-276, KHA-275, and KHA-277. Project-scoped results were used; this is not a workspace-wide count of the user's assignments.

The existing [CS-22 distribution strategy](https://linear.app/khaentertainment/issue/CS-22/distribution-strategy-lead-with-accessible-products-plexium-markedup) argues for accessible Plexium/MarkedUp entry products that introduce people to Clarit. That supports retaining a useful local/open-source path while testing a commercial service hypothesis. It is historical strategy, not proof of present demand.

## Architecture and implementation assessment

### Plexium

The product is a Go 1.25.1 module using Cobra/Viper, filesystem state, Markdown/YAML, git subprocesses, and an optional model-provider cascade. Major packages are `internal/{wiki,scanner,convert,manifest,compile,lint,sync,regen,publish,hook,ci,agent,daemon,plugins}` and `internal/integrations/{pageindex,memento,beads,roles}`. The CLI composes these in `cmd/plexium`.

The durable model is a `.wiki/` content tree plus `.plexium/manifest.json`. The manifest relates page paths to source hashes and now includes graph-enrichment fields. MarkedUp is pinned to an April pseudo-version. Retrieval has two paths: the built-in PageIndex-named implementation and an optional MarkedUp plugin. Most Plexium engine packages are Go `internal` packages; a separately versioned public engine API does not yet exist.

| Capability | Assessment |
|---|---|
| Scaffold, conversion, navigation, manifest CRUD | Implemented, covered by tests; preserve. |
| Source-change detection | Implemented; freshness semantics need correction. |
| Deterministic lint | Useful structural checks; does not prove factual accuracy. |
| Regeneration | Implemented; nonempty model output is written directly, so publication-quality review is a separate need. |
| CLI retrieval | Working built-in lexical retrieval; bypasses plugin retrieval registry. |
| MCP retrieval | Working basic tools; freshness and protocol deficiencies reproduced. |
| MarkedUp enrichment/retrieval | Significant implementation, but default storage/identity contracts do not line up. |
| Agent setup and provider configuration | Substantial implementation; requires current-client compatibility checks before claiming supported versions. |
| Background maintenance | More implemented than the old status page suggests; still warrants operational validation. |
| Linear daemon tracker | Still explicitly unimplemented. |
| Transcript memory loop | Components exist; complete safe capture-to-knowledge flow is unproven and incompletely connected. |
| Hosted service | No production tenant/auth/job/storage/service layer identified in the inspected architecture. |

### MarkedUp

MarkedUp is a Go library and standalone CLI/TUI/MCP product for Markdown knowledge graphs. Public packages cover schema, Markdown parsing, index/search/traversal, embeddings, reranking, enrichment, temporal scoring, and caching. It already uses an MCP SDK; Plexium maintains a separate handwritten protocol server. The standalone product also has a setup wizard, endpoint configuration and probes, keyring integration, and fallback workflows.

That makes it a reusable technical asset, but also creates duplicated product responsibilities: two configuration experiences, two retrieval surfaces, two provider integrations, and two MCP implementations. A durable division would be:

- **MarkedUp:** document identity, metadata, graph/index primitives, deterministic extraction, retrieval adapters, and serialization.
- **Plexium:** repository/workspace lifecycle, source provenance, change detection, review and validation, knowledge publication, and the user-facing coding-agent workflow.

Keep separate libraries where useful. Avoid building two competing hosted products or independently expanding both frontends until their audiences are validated.

### Dependencies and release hygiene

A read-only `go list -m -u -json all` found newer versions for five of Plexium's eight direct dependencies: glob 0.2.3 → 1.0.0, go-toml/v2 2.1.0 → 2.4.3, Cobra 1.8.1 → 1.10.2, Viper 1.18.2 → 1.21.0, and testify 1.9.0 → 1.12.1. These are upgrade candidates, not evidence of vulnerabilities. Major-version behavior and Go toolchain compatibility require review; do not bulk-update as part of a product reset.

The May release decision is sensible: retain the pinned MarkedUp module, add compatibility gates, then adopt tagged releases and automated update PRs. A sidecar would add deployment complexity without resolving schema correctness. No runtime extraction-model upgrade is needed just to stabilize this boundary.

## Verification results

| Check | Result and qualification |
|---|---|
| Plexium `go test ./... -count=1 -timeout 180s -coverprofile=…` | **Pass**, 64.1% aggregate statement coverage. Initial sandbox run could not bind test sockets; rerun with that restriction removed passed. |
| Plexium `go vet ./...` | **Pass**. |
| Plexium binary build | **Pass**, output placed in temporary directory. |
| Test inventory | 859 `func Test` declarations found under CLI, internal packages, and validation. This is not a count of distinct end-to-end scenarios or executed subtests. |
| MarkedUp tests at `4513879` | All tested packages pass across the suite run and corrected config rerun. Global `MARKEDUP_DISABLE_KEYRING=1` initially disabled fake-ring tests too; config passes with the override removed and its verified fake backend in use. Do not report the first run as a product regression or claim published main was tested. |
| MarkedUp vet/build | **Pass** in an isolated committed-source archive. |
| Live CLI/MCP probes | Found behavioral gaps below despite unit-suite success. |
| GitHub build CI on Plexium main | Most recent main build/test run succeeded, May 26. |
| GitHub scheduled lint | Eight most recent retrieved runs failed; August 3 log confirms unsupported `--output`. |
| Native model/service integrations | No paid API or local-model quality evaluation performed. |
| Vulnerability scan | Not performed; scanner unavailable. |

Representative Plexium coverage: CLI 33.4%, CI checks 33.9%, lint 42.0%, daemon 60.6%, regeneration 73.5%, MarkedUp adapter 78.7%, built-in retrieval 84.6%, convert 85.3%, compile 92.3%. Coverage identifies where to examine contracts; higher percentages do not establish that contracts are correct.

## Gap analysis

No production outage or active external disclosure was established. The following are high-priority correctness and readiness findings. Hosted deployment would increase their impact.

### F1 — Sync can erase evidence of stale knowledge

**Verified, high priority.** `internal/sync/sync.go` updates source hashes during ordinary sync even without regeneration. In the probe, a tracked source changed while its page retained the old explanation. First sync reported one stale page, one hash updated, and zero pages regenerated. The second reported zero stale pages; the page was unchanged.

This may match the original command's bookkeeping intent, but it conflicts with using those hashes as evidence that knowledge remains aligned with code. Introduce separate observed-source and verified-knowledge revisions. A successful scan must not imply a successful review. Preserve stale state until actual validation/regeneration is accepted. Existing semantic lint can be an additional signal, not a substitute for that distinction.

Evidence: `internal/sync/sync.go`, `internal/manifest/crud.go`; [probe](evidence/sync-probe.json). Follow-up: `Plexium-a7v`.

### F2 — Retrieval is stale within an MCP session and shallow by default

**Verified, high priority.** Start the server, query a page titled Amberwidget, edit it to Cobaltwidget, then query again: Cobaltwidget is absent and Amberwidget still matches. A new CLI process sees Cobaltwidget immediately. `Server.Start` loads the built-in index once; no refresh occurs in the inspected loop.

The same probe put BodyOnlyNeedle beneath the summary paragraph. Built-in MCP search returned no match. Search scores title, section, first-paragraph summary, and links; it is neither full-body BM25 nor the upstream PageIndex tree engine, despite older status wording. The CLI has additional fallback behavior, so this specific observed miss is the built-in MCP search path, not every possible retrieval route.

Use a shared query contract for CLI/MCP, explicit index revision, and defined refresh behavior. Evaluate full-text, semantic, and graph retrieval as interchangeable strategies rather than selecting a strategy by its name.

Evidence: `internal/integrations/pageindex/{index,server,retrieve}.go`, `cmd/plexium/main.go`; [probe](evidence/cli-mcp-probe.json). Follow-up: `Plexium-5o0`.

### F3 — MarkedUp enrichment and retrieval disagree on their source of truth

**Verified, high priority.** The plugin defaults to storing enrichment in the manifest without rewriting wiki frontmatter. Retrieval then loads wiki files using `WithAutoEnrich(false)` and does not consume that manifest metadata. Files with no MarkedUp ID can overwrite one another under the empty-string index key.

In a fresh init/convert probe with the plugin enabled, the manifest recorded `Home.md` as a document. The graph result instead contained one empty-ID scaffold page plus a deliberately ID-bearing raw fixture; Home was absent from that graph listing. This exposes an integration contract gap, not a claim that MarkedUp's standalone graph works incorrectly for valid identified documents.

Decide the canonical enriched representation. Either construct the index from manifest-enriched documents or publish stable enriched frontmatter as an explicit operation. Reject or deterministically assign missing IDs, namespace IDs across repositories, and expose parse/identity warnings. Make default-mode integration fixtures cover the entire convert → enrich → serve → retrieve chain.

Evidence: `internal/plugins/markedup/{config,enricher,retrieval}.go`, MarkedUp `index/{load,index}.go`; [probe](evidence/plugin-probe.json). Follow-up: `Plexium-5o0`.

### F4 — Retrieval providers apply different raw-content boundaries

**Verified, high priority.** Built-in retrieval skips `raw/`. The MarkedUp plugin's filesystem loader only skips `.knowledge/` in the inspected walk. A synthetic `raw/audit-private.md` fixture was absent from `pageindex_search` but returned its text through `markedup_search`.

No real secret was used or observed being disclosed. The reproduced behavior matters because transcripts and source material are explicitly stored under raw paths. Define one exclusion/sensitivity policy before enabling plugins, automatic transcript capture, or shared hosting. Apply it during ingestion and retrieval; do not depend on one provider's filename convention.

Evidence: the same [plugin probe](evidence/plugin-probe.json). Follow-up: `Plexium-5o0`; related existing Linear KHA-266.

### F5 — The transcript-learning loop is not connected as advertised

**Code inspection, high priority.** The dedicated `MementoIngestor.IngestNewTranscripts` is called by tests only. It expects `.wiki/raw/memento-transcripts/`; the daemon's generic raw-ingest discovery reads the raw directory's immediate entries and skips subdirectories. A generic ingestion path exists, but that does not establish the expected nested transcript pipeline.

The ingestor extracts phrase matches such as “we decided to,” writes decision pages, and marks transcripts processed. It does not implement the sanitization/review gate requested in KHA-266. Generated page names can collide, and direct writes do not implement a conflict-aware decision lifecycle.

Stronger models are especially relevant here: extract structured decision candidates with evidence and uncertainty, then validate/deduplicate/review them before publication. Prefer useful decisions and failed-attempt rationale over wholesale transcript replication. Preserve raw-session access separately.

Evidence: `internal/integrations/memento/ingest.go`, `internal/daemon/upkeep.go:146`; follow-up `Plexium-jk2` and Linear KHA-266.

### F6 — Workflow and protocol tests miss real integration failures

**Verified, high priority.** [Scheduled run 30804688871](https://github.com/Clarit-AI/Plexium/actions/runs/30804688871) fails on `plexium lint --deterministic --ci --output …`. The CLI supports `--output-json`, not `--output`. The workflow also pins Go 1.22 while the module requires 1.25.1; the examined failure is the flag, not an inferred toolchain failure.

The handwritten MCP server returns a method-not-found response with `id: null` to `notifications/initialized`. Notifications must not receive responses. Pinning an old protocol version is not by itself proof of breakage; the reproduced notification behavior is a specific contract violation. Adopt a maintained SDK or add protocol conformance tests, version negotiation, lifecycle handling, and a real client handshake. [MCP message specification](https://modelcontextprotocol.io/specification/2025-06-18/basic).

Evidence: `.github/workflows/plexium-scheduled-lint.yml`, `internal/integrations/pageindex/server.go`; [probe](evidence/cli-mcp-probe.json). Follow-up: `Plexium-1g3`.

### F7 — Enforcement is weaker than “verified knowledge” implies

**Code inspection, medium/high priority.** The precommit hook accepts any wiki change alongside source changes; it does not require updating the affected page. CI's mapping checks record untracked changes, but the final pass decision can accept any wiki change or acceptable recorded debt. This is workflow encouragement, not evidence-level freshness verification.

Keep structural determinism, but describe it accurately. Define accepted source revisions and per-page review status; verify the relevant dependencies, not just the existence of a wiki diff. Model-generated confidence is not a calibrated correctness guarantee.

Evidence: `internal/hook/precommit.go:71`, `internal/ci/check.go:115`.

### F8 — Product-state and release evidence are stale

**Verified, medium priority.** The local Plexium manifest has 52 pages, all with the same April 7 last-updated value, 147 source references, one missing source, and an empty last-processed commit. There are 61 local wiki Markdown files and none tracked in the main repository. A separate publishing model can explain the last fact; it is not independently a defect. The snapshot is nevertheless unsuitable as sole evidence for current implementation.

The status document calls some behavior stable that now differs, omits major added capabilities, and describes retrieval inaccurately. The untracked May reports and open V3 draft must be reconciled into a single current assessment. Do not mass-close old tickets or remove stashes based solely on this report.

Follow-up: `Plexium-rbd`.

### Additional design risks

- MarkedUp `search` can mutate source Markdown through default auto-enrichment. This documented behavior was reproduced on a disposable plain file. A future shared read API must be explicitly side-effect-free; migrations/enrichment should be separate jobs.
- Plexium manifest locking is per Manager instance and process, and Save writes the JSON file directly. A hosted/multi-worker design needs transactional persistence or serialized updates and crash-safe writes, not only an in-process mutex.
- Fresh initialization emits sidebar links that the current sidebar validator reports as unresolved. Keep fresh-init → compile → lint as a real CLI acceptance test, not separate tests with incompatible fixtures.
- Plexium still stores provider credentials in a permission-restricted local JSON file; KHA-270's keychain request remains relevant. A hosted service requires a different credential model. No credential contents were inspected.
- Generated wiki prose can drift even when hashes and links are valid. Claims, citations, uncertainty, and review status need explicit treatment.

## What has changed in the surrounding market

The broad category is now crowded. The defensible question is which useful outcome Plexium delivers beyond tools developers already use.

| Current offering | Documented capability | Implication for Plexium |
|---|---|---|
| [Xirp + Portal](https://portal.spotify.com/blog/introducing-xirp) | Multi-agent sessions connected to organizational context, shared transcripts, and reusable institutional knowledge | Direct overlap with the memory loop; competing as another full agent workbench would expand scope substantially. |
| [Augment Context Engine MCP](https://docs.augmentcode.com/context-services/mcp/overview) | Local working-directory context and hosted indexing of selected default branches, including cross-repo use | Multi-repo MCP context is already an available product category. Hosting or MCP alone is not differentiation. |
| [GitHub Copilot indexing](https://docs.github.com/en/copilot/concepts/context/repository-indexing) | Automatic semantic repository indexing for chat and coding agents | Basic repository search must compete with built-in functionality. |
| [DeepWiki](https://docs.devin.ai/work-with-devin/deepwiki) | Generated repository documentation, diagrams, source links, and Q&A | A generated wiki alone is a weak new-product proposition. |
| [Claude Code memory](https://code.claude.com/docs/en/memory) | Persistent instruction files and agent-maintained memory | “Agents can remember things” is no longer sufficient positioning. Cross-tool sharing and trust must be demonstrated. |
| [Graphiti](https://github.com/getzep/graphiti) | Temporal context graphs, provenance, incremental updates, and hybrid retrieval | Graph structure and temporal metadata are useful mechanisms but are not unique product claims. |

These are vendor/open-source descriptions checked for this report, not independent performance comparisons. Spotify's scale claims and any vendor quality claims should not be treated as results for this project.

The promising gap to test is **portable, reviewable project knowledge that carries its evidence and revision context across repositories and coding tools**. Example: explain why a dependency was pinned, identify the decision and test that justified it, show whether that decision still applies to the current revision, and identify which other project is affected by changing it.

That is more specific than “better RAG,” and is testable against native search plus handwritten ADRs.

## What stronger models should change

Better models are an opportunity to simplify and improve quality, not evidence that the current architecture will now work automatically. No current-model bake-off was run during this audit.

**Reassess model-specific scaffolding.** MarkedUp's NuExtract parser repair work, multiple specialist extraction formats, and layered fallback orchestration addressed real limitations. Benchmark one capable model using structured output against those paths before expanding them. Retain local/offline profiles where cost or privacy warrants them; do not require a small-model stack for the default experience.

**Spend inference on synthesis and validation.** Stronger models can propose decision records, reconcile conflicting accounts, classify which knowledge a change invalidates, and decide what evidence to retrieve next. Keep deterministic code responsible for identities, revisions, policy boundaries, persistence, and reproducible checks.

**Stop precomputing everything by default.** Cheap structural indexes plus targeted, on-demand analysis may beat elaborate eager summarization. Anthropic describes just-in-time retrieval and concise context as useful even with improving models; this is a design input, not proof of which backend wins here. [Context engineering guidance](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents).

**Measure marginal value over strong agents.** The baseline is an agent with normal filesystem search, current instructions, and its available native memory. A successful wiki-generation demo is not the benchmark. Measure actual task success, correctness of cited rationale, stale-answer rate, time, tokens, and maintenance burden.

## Deployment and product options

Multi-repository support and hosted delivery are independent decisions. A local workspace can index several repositories; a hosted service can preserve repository-owned knowledge. There is no need to replace the engine merely to serve a UI.

| Option | User value | Main cost/risk | Recommendation |
|---|---|---|---|
| A. Repair and ship repository CLI/plugin | Easiest adoption; existing workflow; private/local use | Narrow value unless outcome quality beats native agent tools | Necessary foundation and useful entry product. |
| B. Local or self-hosted multi-repo workspace | Shared decisions, dependency contracts, and retrieval across a project family | Repo/branch identity, cross-repo links, freshness, concurrency | **Best next product experiment.** |
| C. Hosted companion over repository artifacts | Always-on refresh, shared access, review UI, onboarding, remote MCP | Auth, permissions, installation, jobs, storage, operations, billing | Build after B establishes demand; initially a narrow pilot. |
| D. Hosted-only knowledge platform | Central experience and service monetization | Migration, trust, procurement, storage dependencies, crowded competition | No evidence yet to justify replacing the local model. |
| E. Backstage/Portal knowledge integration | Reach teams with an existing catalog | Platform integration and distribution dependency | Useful adapter experiment if users request it; not a prerequisite. |
| F. General multi-agent development environment | Session/worktree management | Large new product surface and direct competition with agent vendors/Xirp | Defer. Delegate coding execution to existing harnesses. |

The commercial hypothesis is that teams pay for **shared freshness, access control, and reduced coordination effort**, not merely for a hosted copy of Markdown. The local product should prove useful independently. Validate willingness to pay with actual pilot teams before investing in an enterprise platform.

## Suggested target architecture

Preserve the useful layers while making their contracts explicit:

1. **Evidence sources:** selected repository revisions, PRs, accepted decisions, optional sanitized session material. Record authority and visibility, not just text.
2. **Knowledge records:** stable ID, repository identity, source path/range and revision, claim/decision text, dependencies, validation state, supersedes links, and visibility. Distinguish observed, inferred, proposed, accepted, and superseded content.
3. **MarkedUp indexing:** derived lexical/vector/graph indexes over those records. Rebuildable and replaceable; not the only copy of knowledge.
4. **Plexium maintenance:** detect changes, propose updates, validate structure and evidence, apply review policy, then publish a new knowledge revision.
5. **Shared access:** one query contract exposed through CLI, local MCP, and optionally authenticated service endpoints; return provenance and freshness with results.
6. **Optional UI:** browse explanations, inspect evidence, review proposed updates, and see stale/missing coverage. A dashboard should support those decisions rather than add another place to manage agent sessions.

For the first multi-repo version, use a workspace manifest listing explicitly selected repositories. Namespace identities by repository plus stable document ID. Distinguish default-branch snapshots from local worktree changes. Do not combine incompatible branches into a single unstated truth. Support cross-repo evidence links and invalidation before adding automated cross-repo edits.

For hosted delivery, retain Markdown/JSON export and consider git-backed accepted records with transactional service metadata and derived indexes. A database is reasonable for jobs, authorization, revision pointers, and concurrent writes; it need not replace portable knowledge artifacts. Decide the canonical accepted record and reconciliation rules before introducing bidirectional editing.

Hosting adds work that is absent from the current engine: tenant/user/repo authorization, OAuth/GitHub App lifecycle, remote MCP authentication, webhook deduplication, job retries, rate limits, audit trails, deletion/retention, backups, and spend controls. These are concrete service requirements, not reasons to abandon hosting. Permission checks must occur before retrieval exposes any document, snippet, embedding-derived result, or graph relationship. [MCP HTTP authorization specification](https://modelcontextprotocol.io/specification/2025-06-18/basic/authorization).

## Reassess the feature backlog

| Disposition | Features | Reason |
|---|---|---|
| Keep and harden | Source mappings, ownership modes, reproducible navigation, portable files, source-linked retrieval | Reusable foundation for any deployment. |
| Redesign now | Freshness state, stable IDs, canonical enrichment, query/refresh behavior, transcript ingestion | These determine whether the knowledge can be trusted. |
| Simplify | Provider UX, specialist-model defaults, duplicate CLI/MCP retrieval paths, duplicate configuration | Reduce maintenance and first-run friction. |
| Evaluate before implementing | PageIndex tree search, graph reasoning, embeddings/reranking, isolated librarian | Choose based on measured recall/task lift and cost. |
| Defer | Marketplace, broad plugin lifecycle, TUI expansion, status footer, ASCII art, parallel web librarian | Add surface area without first establishing the core outcome. |
| Reconsider strongly | Automatic removal of original documents (KHA-260) | Provenance should survive migration; an archive/export workflow is safer and more useful than deleting evidence. |
| Reuse from existing tools | General coding-agent execution, session UI, worktree orchestration where possible | Plexium should not need to become a full agent IDE. |

Reinterpret draft PR #27 as a hypothesis inventory. A small context-selection API may be valuable, but access-frequency ranking, isolated librarian processes, recap injection, and parallel web search should not precede reliable retrieval and evaluations. Frequently retrieved content is not necessarily correct or relevant. Preserve the draft, revise its order, and avoid mechanically implementing six old workstreams.

## Evaluation plan and decision gates

The following numbers are proposed pilot acceptance criteria, not achieved results or industry benchmarks. Adjust after observing task variance.

### Gate 1: reliable single-repository workflow

Use temporary fixtures plus real representative repositories. Exercise fresh init/convert, source edit, stale detection, update proposal, acceptance, persistent MCP retrieval, restart, and failed-job recovery. Require no loss of reviewed facts, no false “current” state after an unvalidated source change, consistent exclusions, and stable IDs. Test a prohibited synthetic canary to verify raw/hidden content does not leak through alternate providers.

Exit: all known F1–F6 regression cases pass; documented fresh-install path works; retrieval reports its source revision; failed writes/retries preserve previous accepted knowledge.

### Gate 2: prove agent outcome improvement

Build approximately 30–50 tasks across Plexium, MarkedUp, and one larger actively maintained repository. Include architecture questions, a known debugging task, an old decision that was superseded, a misleading/stale document, and a cross-repo API change. Keep expected evidence and answers versioned. Avoid evaluating only the generated wiki's wording.

Compare the same agent/model settings across:

- Native code search + normal project instructions/memory.
- Existing maintained docs/ADRs + the same agent.
- Plexium basic retrieval.
- Plexium + MarkedUp graph/semantic retrieval.
- Optional targeted model-assisted context selection.

Use several runs where model variance matters. Assess outcomes blindly where feasible and record both accuracy and cost. Track task completion, answer correctness, citation support, stale/superseded-fact errors, retrieval recall, latency, tokens, first-use setup time, and human review burden. Include maintenance cost per meaningful source change. Separate ingestion-model quality from answering-model quality.

Suggested pass bar: at least a 15% relative task-success improvement **or** a 25% reduction in time/tokens at equivalent correctness on the selected task set, with no increase in unsupported assertions. Require zero forbidden-canary disclosures in the tested cases. Report uncertainty; a small pilot cannot establish universal superiority.

### Gate 3: earn multi-repository scope

Run a thin workspace prototype across Plexium and MarkedUp, then add a third repository with genuine dependencies. The key example is the semantic-relationship schema change: identify the upstream decision, downstream assumptions, affected integration, and validation needed before a bump.

Exit: users repeatedly answer questions that neither isolated repository answers well; repo/revision identity is explicit; default branches and worktrees cannot silently contaminate one another; the service updates knowledge after a change without manual re-setup.

### Gate 4: earn hosted delivery

Recruit a small set of design partners, for example three to five teams, with multiple repositories and more than one coding tool or contributor. Determine whether the valuable job is onboarding, dependency-change impact, decision recovery, or maintaining shared knowledge. Observe repeat use over several weeks and test willingness to pay for operational convenience and collaboration.

Exit: shared access/always-on maintenance is a recurring blocker for the local version, users retain the workflow without prompting, permissions/export work in realistic scenarios, and estimated ingestion/query/support costs permit a viable offer. Only then choose a hosted companion, customer-hosted service, or hosted-first package.

If Gates 2–3 fail, narrow Plexium to a dependable documentation-maintenance tool or package MarkedUp as a library. Do not use hosting to mask an unproven core benefit.

## Recommended sequence

**Immediate:** preserve recovery artifacts, reconcile this report with the existing decision notes, repair F1–F6, and update the status document after verification. Review PR #156 separately from published MarkedUp main; add compatibility fixtures before changing the pin. Do not spend the first restart sprint on model menus or a new frontend.

**Next:** build the evaluation harness and run a small current-model comparison. Use the results to choose the simplest effective extraction and retrieval path. Consolidate the two products' responsibilities and define one supported setup path.

**Then:** prototype multi-repo context locally with explicit source revisions and cross-repo decisions. Add a minimal evidence/review UI only if observing the workflow shows it is useful.

**Later, conditionally:** offer an authenticated hosted companion and design-partner pilot. Decide pricing and distribution from observed use and operating cost. Keep export/local operation as an intentional product choice, not an accidental limitation.

## Follow-up tracking

The audit created these local Beads items, leaving existing Linear/GitHub issue states unchanged:

| ID | Scope |
|---|---|
| Plexium-a7v | Freshness semantics and stale-state regression. |
| Plexium-5o0 | Retrieval refresh, canonical graph data, identity, and exclusion contract. |
| Plexium-1g3 | Scheduled lint and MCP protocol checks. |
| Plexium-jk2 | Safe transcript-to-knowledge workflow; cross-reference KHA-266. |
| Plexium-rbd | Recovery artifacts, tracker reconciliation, and release compatibility gates. |

These record verified or concrete follow-up work. The product options and pilot gates remain recommendations for discussion, not a committed feature roadmap.

## Documentation inventory and quality

The complete path inventory is in [documentation-inventory.md](documentation-inventory.md). Its presence is not a claim every file was read line by line; detailed review focused on architecture, current behavior, status, decisions, and critical execution paths.

| Documentation group | Assessment |
|---|---|
| Root README and user/how-it-works guides | Useful intent; claims about memory/retrieval need qualification against this audit. |
| `docs/status.md` and historical validation | Materially stale; preserve as dated evidence and replace current-status claims. |
| `docs/architecture`, `docs/phases`, `docs/reference` | Original design and implementation history; distinguish intention from shipped behavior. |
| `docs/decisions/KHA-299-markedup-release-strategy.md` | Useful May decision, untracked at audit start. |
| `docs/plans/pageindex-plugin-migration-roadmap.md` | Detailed May assessment, untracked; accurately distinguishes built-in retrieval from upstream PageIndex. |
| PR #27 V3 plan | Unmerged future work; reorder using outcome evaluations. |
| Local `.wiki/` | Historical generated snapshot; not adequate as sole current context. |
| MarkedUp docs | Broad coverage of schema/API/CLI/MCP; local files have pre-existing generated frontmatter changes. Current committed source used for test baseline. |

Harness-specific instruction copies were not imported as project authority. Optional `.agent/` notes were absent. Existing credentials and complete user configuration files were not printed.

## Questions for the next discussion

The important decision is the first user and recurring job. A solo developer returning to a repository, a small team coordinating changes across services, and an enterprise knowledge-platform buyer require different products. The recovered distribution strategy favors an accessible developer entry point; this report uses that as a provisional starting point while evaluating all three.

Choose one concrete success story to pilot: “recover why this was built,” “safely change an upstream dependency,” or “keep shared project knowledge current across tools.” Then decide which records deserve durable storage, what may leave the machine, and who approves shared knowledge. Those answers should determine deployment and UI, rather than selecting SaaS first and working backward.
