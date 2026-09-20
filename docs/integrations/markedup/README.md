# MarkedUp Integration

Plexium consumes the [`github.com/Clarit-AI/markedup`](https://github.com/Clarit-AI/markedup)
knowledge-graph library through two plugins (enricher + retrieval) wired by
`internal/plugins/markedup/`. This directory documents the integration boundary
so a future MarkedUp bump can be validated mechanically rather than by reading
both codebases.

## Contents

| File                          | Purpose                                                                            |
| ----------------------------- | ---------------------------------------------------------------------------------- |
| [`pin.md`](pin.md)            | Exact pin (commit SHA / pseudo-version) and version history.                       |
| [`public-apis.md`](public-apis.md) | MarkedUp public APIs Plexium depends on, with `file:line` references.        |
| [`upgrade-procedure.md`](upgrade-procedure.md) | Steps to validate a MarkedUp bump before merge.                      |
| [`rollback.md`](rollback.md)  | How to revert to the previous pin if a bump breaks Plexium.                        |

## Scope

This integration contract covers the MarkedUp library consumed as a Go module
dependency in `go.mod`. MarkedUp is a **library**, not a binary: it does not run
as a sidecar process and Plexium does not depend on `@main` of the MarkedUp
repo. All wiring lives inside the Plexium process at
`internal/plugins/markedup/*`.

## Related artifacts

- `internal/plugins/markedup/` — the integration code (enricher, retrieval, config).
- `internal/plugins/markedup/*_test.go` — the contract tests that fail before
  release if a MarkedUp bump breaks the boundary.
- `probes/kha-574/` — fixture-style end-to-end probe that exercises the full
  enrich → retrieve round-trip plus the exclusion contract.
- `internal/manifest/manifest.go` — `GraphMetadata`, `RelationshipRef`,
  `GraphMetadataSemanticEqual` — the manifest shape the enricher writes to.

## Origin

Created under Linear KHA-574 to gate MarkedUp dependency bumps against the
integration contract. The pre-KHA-574 state shipped with `c5b6ff8 chore: update
markedup dependency to github.com/Clarit-AI/markedup (#24)` and the fix
`ed2a09b fix(markedup): preserve semantic relationships after module bump (#26)`
which itself demonstrated the cost of bumping without a contract.
