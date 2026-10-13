# KHA-574 — MarkedUp Integration Contract Gate

## Status

Accepted (2026-09-20, base `main` at `eef0327`).

## Context

Plexium depends on `github.com/Clarit-AI/markedup` as a library (not a
sidecar process) and pins it by Go-module pseudo-version
`v0.0.0-20260419063450-0c5745b5a986`. The dependency is consumed at six
import sites across two Plexium packages:

- `internal/plugins/markedup/{enricher,retrieval,llm_adapter}.go`
- `internal/plugins/bootstrap/bootstrap.go`

MarkedUp publishes no semver tags; every Plexium bump so far has been a
pseudo-version change. The first bump (the one that landed `ed2a09b
fix(markedup): preserve semantic relationships after module bump (#26)`)
silently dropped `manifest.PageEntry.SemanticRelationships` because
`manifest.GraphMetadataSemanticEqual` didn't compare them. The bug shipped
because no contract test pinned the behavior — the only thing that caught it
was a manual CodeRabbit review pass.

That cost motivates KHA-574: gate future MarkedUp bumps against the
integration contract, so a breaking change fails the bump PR rather than
silently corrupting manifests in production.

## Note on the referenced KHA-299 source-of-decision

The original task description pointed at
`docs/decisions/KHA-299-markedup-release-strategy.md` as the source-of-decision
for the May 26 strategy. That file does not exist in the repository at the
time of this decision (no `docs/decisions/` directory exists at all). This
ADR is therefore the **first** tracked decision about the MarkedUp
integration boundary; the KHA-574 PR should not be blocked on KHA-299
materialization. If KHA-299 is later written, it should reference this ADR
rather than supersede it — the contract test list below is the operational
truth, not the historical reasoning.

## Decision

1. **Pin by pseudo-version only.** No `@main`, `@latest`, or floating-branch
   imports. See [`pin.md`](../integrations/markedup/pin.md).

2. **Six public APIs to watch.** Enumerated in
   [`public-apis.md`](../integrations/markedup/public-apis.md). Any new
   import site in a future PR MUST extend that file in the same commit.

3. **Contract tests at the integration boundary.** Eight contract tests
   added under `internal/plugins/markedup/` and one end-to-end probe at
   `probes/kha-574/`. These tests MUST pass against any MarkedUp pin before
   merge. See the test list in
   [`upgrade-procedure.md`](../integrations/markedup/upgrade-procedure.md#step-4).

4. **No sidecar migration.** MarkedUp is a library imported into the
   Plexium binary, not a separately-running process. Plexium does not
   invoke `cmd/markedup` directly. This rules out the "sidecar migration"
   shape of integration; a future MarkedUp release that switches to a
   sidecar model would require a new ADR.

5. **Exclusion gap (F4) is acknowledged, not silently bypassed.** The
   contract test `TestRetrievalContract_RawDirectoryExcluded` documents the
   gap as a known issue. A bump is rejected if it doesn't close the gap;
   see [`upgrade-procedure.md`](../integrations/markedup/upgrade-procedure.md#known-issues).

## Consequences

- A MarkedUp bump that fails any contract test is rejected at PR time.
- The Plexium-side exclusion wrapper (when implemented) will close the F4
  gap and turn the red test green; that fix is intentionally **out of scope**
  for KHA-574.
- Adding new MarkedUp API call sites requires updating both the
  implementation and `public-apis.md` in the same commit. Reviewers should
  enforce this.

## Out of scope

- Bumping the MarkedUp dependency itself. KHA-574 only adds the gate.
- Fixing the F4 exclusion gap on the Plexium side.
- Refactoring `internal/plugins/markedup/` speculatively.
- Documentation in `docs/audits/` (the audit referenced by the task
  description does not exist in this repo).
