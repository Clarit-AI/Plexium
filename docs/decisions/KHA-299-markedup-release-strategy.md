# KHA-299: MarkedUp Release/Update Strategy

**Status:** Decided
**Date:** 2026-05-26
**Supersedes:** KHA-289 (MarkedUp-specific portion; general plugin lifecycle → KHA-297)

## Decision

### Near-term: Pinned pseudo-version + compatibility gate

Keep the current pinned-dependency model. Before any future `markedup` bump, run
an integration compatibility test suite that exercises the contract Plexium
depends on. The compat tests are the concrete next action (see Follow-up Issues).

- Pin to specific commits, not `@main`
- Record the pinned commit hash in a `go.mod` comment
- Compat tests must pass against the current pin before attempting a bump
- Bump procedure is bundled with the compat test issue

### Medium-term: Tagged releases → automated bump PRs

Gate this transition on MarkedUp adopting:
- Semver tags (`v0.1.0`, `v0.2.0`, ...)
- A CHANGELOG or release notes file documenting breaking changes
- A documented public API surface (which packages/types are stable vs internal)

Once those exist, Plexium tracks `markedup@v0` (Go module semver), Dependabot
can open automated bump PRs, and the compat tests built in near-term serve as
the automated gate.

### Not pursued: Sidecar / package-manager install

The integration is ~3K lines with a clean plugin boundary. Moving MarkedUp
outside `go.mod` would add a process boundary, deployment coordination, and
architectural complexity disproportionate to the current risk. Revisit only if
MarkedUp grows into a heavy dependency (embedded ML model, GPU requirement,
conflicting transitive deps).

## Evidence

- MarkedUp has zero git tags, zero GitHub releases — purely commit-based
- Plexium imports 6 MarkedUp packages across 6 Go files (~15 exported symbols)
- The KHA-298 bump (`12e3cc5` → `0c5745b`) required 9 files changed for one
  behavioral change: issue #109 splitting `Relationships` into `SemanticRelationships`
- The break was caught by an existing test, but a bump at a different point in
  time could have resulted in silent data loss

## KHA-289 Disposition

KHA-289 ("Makedup/Plugin Update System Decisions") overlaps both this decision
and KHA-297 (general plugin lifecycle/update framework). Mark it **superseded** by:
- KHA-299 for MarkedUp-specific dependency strategy
- KHA-297 for general plugin lifecycle/update framework

## Follow-up Issues

| Issue | Where | Priority |
|-------|-------|----------|
| Add MarkedUp integration compatibility tests before future module bumps | Plexium | High |
| Adopt semver tags and CHANGELOG | MarkedUp upstream | Medium |
| Update KHA-299 with this decision → mark Done | Linear | — |
| Mark KHA-289 superseded by KHA-299 + KHA-297 | Linear | — |

### Compat test scope

- Tier 1 fixture: wikilink-derived `Relationships`
- Tier 2 fixture: NER-derived `SemanticRelationships`
- Config parse case-normalization check (Viper lowercase contract)
- Retrieval tool registration sanity
- Must fail clearly when MarkedUp changes semantics, before any Plexium code changes
