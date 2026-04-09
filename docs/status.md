# Plexium Implementation Status

> Last updated: 2026-04-06 | Validated against commit `e642d22`

This document is the single source of truth for what works, what is incomplete, and what is planned. Every other Plexium doc references stability tiers defined here.

---

## Stability Tiers

| Tier | Meaning |
|------|---------|
| **Stable** | Tested in the validation suite, safe for daily use. Behavior is covered by safety invariants and determinism guarantees. |
| **Experimental** | Functional scaffolding exists. Core logic runs, but integration points are incomplete or use placeholder implementations. |
| **Stub** | The CLI command exists and accepts flags, but prints a placeholder message and performs no real work. |
| **Partial** | Some subcommands or modes work; others are stubs or incomplete. Check the notes column for specifics. |

---

## Command Status

| Command | Tier | Notes |
|---------|------|-------|
| `plexium init` | Stable | Scaffolds `.wiki/`, `.plexium/`, config, schema. Flags: `--github-wiki`, `--obsidian`, `--dry-run`, `--strictness`, `--with-memento`, `--with-beads`, `--with-pageindex` |
| `plexium sync` | Stable | Detects stale pages via hash comparison, updates manifest, recompiles navigation. Flags: `--dry-run` |
| `plexium convert` | Stable | Brownfield ingestion: scour, filter, ingest, link, lint. Page quality depends on heuristic-based content extraction. Flags: `--depth`, `--dry-run`, `--agent` |
| `plexium lint` | Stable | Six deterministic checks (links, orphans, staleness, manifest, sidebar, frontmatter) plus optional LLM-augmented checks. Flags: `--deterministic`, `--full`, `--ci`, `--fail-on` |
| `plexium compile` | Stable | Regenerates `_index.md` and `_Sidebar.md` from manifest. Deterministic, idempotent. Flags: `--dry-run` |
| `plexium publish` | Stable | Pushes wiki files to GitHub Wiki remote. Respects publish/exclude filters and sensitivity rules. Flags: `--dry-run` |
| `plexium gh-wiki-sync` | Stable | Selective sync to GitHub Wiki with manifest-aware filtering. Flags: `--dry-run`, `--push` |
| `plexium doctor` | Stable | Validates config, wiki structure, manifest integrity, and integration health. |
| `plexium migrate` | Stable | Applies schema migrations from `.plexium/migrations/`. Flags: `--dry-run`, `--version` |
| `plexium retrieve` | Stable | Queries wiki via PageIndex with fallback to index scan. Flags: `--format` |
| `plexium hook pre-commit` | Stable | Blocks commits when source files changed but wiki not updated. Respects strictness levels. |
| `plexium hook post-commit` | Stable | Tracks WIKI-DEBT when commits bypass the pre-commit hook via `--no-verify`. |
| `plexium ci check` | Stable | Diff-aware wiki validation for CI pipelines. Flags: `--base` (required), `--head` (required), `--output` |
| `plexium plugin add` | Stable | Installs a plugin from `.plexium/plugins/`. Validates manifest, copies files, runs setup. Flags: `--path` |
| `plexium plugin list` | Stable | Lists installed plugins with descriptions. |
| `plexium beads link` | Stable | Links a task ID to a wiki page via frontmatter. Bidirectional, idempotent. |
| `plexium beads unlink` | Stable | Removes a task-to-page link. |
| `plexium beads pages` | Stable | Lists wiki pages linked to a task ID. |
| `plexium beads tasks` | Stable | Lists task IDs linked to a wiki page. |
| `plexium beads scan` | Stable | Scans all wiki pages and builds the complete task-page mapping. |
| `plexium pageindex serve` | Stable | Starts a PageIndex MCP server (stdio mode) for agent-accessible wiki search. |
| `plexium agent status` | Stable | Shows provider cascade health, daily spend, request counts, and rate limit state. |
| `plexium agent test` | Stable | Tests provider connectivity via the cascade. Reports latency, tokens, and cost. Flags: `--provider` |
| `plexium agent spend` | Stable | Shows daily spend per provider against budget limits. |
| `plexium agent benchmark` | Stable | Benchmarks provider latency over 3 rounds. |
| `plexium bootstrap` | Stub | Prints placeholder. No page generation logic implemented. |
| `plexium agent start` | Experimental | Starts the background daemon, writes `.plexium/daemon.pid`, and uses the configured daemon runner/watches. |
| `plexium agent stop` | Experimental | Stops the background daemon referenced by `.plexium/daemon.pid`. |
| `plexium daemon` | Experimental | Poll loop and workspace management work. The LinearTracker returns `ErrNotImplemented` for all operations. Runner dispatches to external CLI tools (untested integration). Flags: `--poll-interval`, `--max-concurrent` |
| `plexium orchestrate` | Experimental | Creates isolated worktree and runs retriever/documenter roles. Default runner is noop. Flags: `--issue` (required) |

---

## Integration Status

| Integration | Tier | What Works | What Does Not |
|-------------|------|------------|---------------|
| **Beads** (task linking) | Stable | Bidirectional task-to-page linking via frontmatter. Link, unlink, scan, query all functional. | - |
| **PageIndex** (wiki search) | Stable | In-memory BM25 index with title/content/link/section scoring. MCP server for agent access. Fallback to index scan when PageIndex unavailable. | - |
| **Memento** (session provenance) | Stable | Transcript ingestion from `.wiki/raw/memento-transcripts/`. Decision extraction via pattern matching. CI gate verifies git-notes on HEAD. | - |
| **Obsidian** | Partial | Plexium generates `.obsidian/` config and dataview templates when `--obsidian` flag is used. | No end-to-end Obsidian workflow testing. Users should verify vault behavior independently. |
| **Roles** (agent capabilities) | Partial | Four role definitions (coder, retriever, documenter, ingestor) with read/write capability maps. | Roles are data definitions only. No active enforcement or assignment. Used by orchestrate (experimental). |
| **Linear** (issue tracking) | Stub | LinearTracker interface defined in daemon. | All methods return `ErrNotImplemented`. No Linear API calls. |

---

## Known Limitations

These are documented findings from the [post-build audit](validation/AUDIT-REPORT.md):

1. **`_schema.md` lint false positives.** The generated schema contains `[[wiki-links]]` as documentation examples of syntax. The link crawler correctly identifies these as broken links because no `wiki-links.md` page exists. This produces lint errors on a freshly initialized repo. Workaround: ignore these specific findings, or exclude `_schema.md` from link checks.

2. **Freshly scaffolded pages trigger frontmatter lint.** Scaffolded pages have minimal frontmatter (title, ownership, last-updated) but the schema prescribes additional fields (updated-by, related-modules, source-files, confidence, review-status, tags). This is expected behavior: agents fill in full frontmatter as they work on pages.

3. **UnmanagedPages protection gap.** Human-authored page protection in `UpsertPage` only checks the `Pages` list. A page tracked only in `UnmanagedPages` can be added to `Pages` as managed. This is low risk because unmanaged pages are files found in `.wiki/` that are not tracked in the manifest.

---

## Validation Summary

The validation suite covers 540+ test functions across 25 packages.

### Safety Invariants (7 proven)

| ID | Invariant | Proven By |
|----|-----------|-----------|
| S1 | Source files unchanged after init, compile, and lint | 3 dedicated tests |
| S2 | Dry-run produces no live file writes | 2 tests (init, compile) |
| S3 | Human-authored pages cannot be overwritten by managed pages | 2 tests |
| S4 | Init is non-destructive on re-run | 1 test |
| S5 | Compile only writes `_index.md` and `_Sidebar.md` | 1 test |
| S6 | Manifest upsert/remove preserves unrelated entries | 2 tests |

### Determinism Guarantees (6 proven)

| ID | Guarantee | Method |
|----|-----------|--------|
| D1 | Manifest pages sorted by WikiPath on every save | Verified across 5 runs |
| D2 | Hash of same content produces same result | Verified across 10 runs |
| D3 | Compile output identical across consecutive runs | Verified across 5 runs |
| D4 | Lint results stable across consecutive runs | Verified across 3 runs |
| D5 | Empty manifest compile produces stable minimal output | Single verification |
| D6 | Manifest JSON shape is stable | Schema contract test |

### Cross-Phase Contracts (10 verified)

- PageEntry struct: 10 fields locked
- SourceFile struct: 3 fields locked
- UnmanagedEntry struct: 3 fields locked
- Manifest top-level: 5 fields locked
- Config struct: 16 top-level fields locked
- Wiki config: 7 fields verified
- Ownership values: `managed`, `human-authored`, `co-maintained`
- Lint report JSON shape: stable
- Lint exit codes: 0 (clean), 1 (errors), 2 (warnings)
- Config validation catches missing required fields
