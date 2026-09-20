# MarkedUp Pin

## Current pin

```
github.com/Clarit-AI/markedup v0.0.0-20260419063450-0c5745b5a986
```

Pseudo-version decoded:

- **Date:** 2026-04-19 06:34:50 UTC
- **Short SHA:** `0c5745b5a986`

This is the version Plexium is pinned to as of the KHA-574 contract gate. The
pin is recorded verbatim in `go.mod` and the corresponding `go.sum` hash is the
authoritative checksum.

## Why a pseudo-version (not a tagged release)

MarkedUp does not yet publish semver tags to its Go module proxy; every
Plexium integration to date has pinned by commit pseudo-version. The
pseudo-version format (`v0.0.0-YYYYMMDDHHMMSS-SHORT_SHA`) is the Go module
system's way of representing a specific commit on a branch without a tag.

This has two operational implications:

1. **No semantic version upgrades.** A "major" MarkedUp change that renames
   `schema.GraphFrontmatter.SemanticRelationships` would look the same in our
   `go.mod` as a no-op patch — both produce a new pseudo-version line. The
   contract tests in `internal/plugins/markedup/*_test.go` are the only thing
   that catches the difference.
2. **No `@main` import allowed.** The Linear KHA-574 acceptance criterion
   `no @main dependency` is satisfied by the current pin: `v0.0.0-20260419...`
   resolves to the commit `0c5745b5a986`, not a floating `@main` reference.
   This is checked by `grep -E "Clarit-AI/markedup @main" go.mod` returning no
   matches.

## Version history

| Date       | Change                                                                                                       | Commit  |
| ---------- | ------------------------------------------------------------------------------------------------------------ | ------- |
| 2026-04-19 | Initial Plexium pin (introduced with the MarkedUp plugin wiring in KHA-259 Phase C/D).                       | (start) |
| 2026-09-20 | `fix(markedup): preserve semantic relationships after module bump (#26)` — first KHA-287-followup contract repair. | `ed2a09b` |

The 2026-09-20 entry is the **only** MarkedUp bump to date in the Plexium
repo, and it is the incident that motivated KHA-574: the bump silently dropped
`SemanticRelationships` from the manifest because `GraphMetadataSemanticEqual`
didn't compare them. The KHA-574 contract gate is what catches the next such
incident before it ships.

## How to read the pin from `go.mod`

```sh
grep "Clarit-AI/markedup" go.mod
#   github.com/Clarit-AI/markedup v0.0.0-20260419063450-0c5745b5a986
```

```sh
grep "Clarit-AI/markedup" go.sum
# github.com/Clarit-AI/markedup v0.0.0-20260419063450-0c5745b5a986 h1:...
```

Both lines must agree. A `go.sum` mismatch against `go.mod` means either the
lock file is stale or the registry returned a different module under the same
pseudo-version (rare but possible); resolve before merge.

## When to bump

A bump is justified when any of the following is true, **and** the
[`upgrade-procedure.md`](upgrade-procedure.md) passes end-to-end:

- MarkedUp publishes a semver tag (move from pseudo-version to tagged version).
- MarkedUp's `schema.GraphFrontmatter`, `schema.Page`, or `index.KnowledgeIndex`
  surface changes in a way Plexium relies on.
- MarkedUp fixes a bug Plexium's contract tests document as a known issue
  (e.g. the `raw/` exclusion gap called out in
  [`upgrade-procedure.md`](upgrade-procedure.md#known-issues)).

It is **not** justified by MarkedUp's release notes alone — Plexium's contract
tests are the gate, not MarkedUp's changelog.
