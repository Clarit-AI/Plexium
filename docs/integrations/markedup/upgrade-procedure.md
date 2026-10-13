# MarkedUp Upgrade Procedure

This procedure gates every MarkedUp dependency bump against the integration
contract. **Breaking semantics fail before release** — if any step turns red,
the bump is rejected and a Plexium-side fix is required before merge.

The procedure assumes the standard Plexium dev workflow:

- Worktree: `bbrenner2217/kha-NNN-gate-markedup-bump-<short-sha>`
- Push remote: `upstream` (configured in `.dev.json`)
- PR target: `Clarit-AI/Plexium`, branch `main`

## Pre-bump baseline

Before touching `go.mod`, capture the current pin and confirm the tests pass.

```sh
CURRENT_PIN="$(grep 'Clarit-AI/markedup' go.mod | awk '{print $2}')"
echo "Current pin: $CURRENT_PIN"

go build ./...
go vet ./...
go test ./... -count=1 -timeout 180s
```

All three commands MUST exit 0 against the existing pin. If any fails before
the bump, fix Plexium first — do not pile a MarkedUp bump onto a broken tree.

## Step 1 — Pin the new commit

Get the new pseudo-version from the MarkedUp repo:

```sh
NEW_PIN="v0.0.0-$(git -C /path/to/markedup log -1 --format=%cI | sed 's/-//g; s/T//; s/://g; s/..$//')-$(git -C /path/to/markedup rev-parse --short=12 HEAD)"
echo "New pin: $NEW_PIN"

go get github.com/Clarit-AI/markedup@${NEW_PIN}
go mod tidy
```

The pseudo-version format MUST match `v0.0.0-YYYYMMDDHHMMSS-SHORT_SHA` or
`go mod tidy` will reject it. The exact format produced by `go get <commit>`
is the canonical form — use the value `go get` writes into `go.mod`, not
a hand-built string.

## Step 2 — Confirm no `@main` import

```sh
grep -E "Clarit-AI/markedup *@" go.mod go.sum 2>/dev/null
# Expected: no output. A `@main`, `@latest`, `@branch`, or `@tag` form is a
# contract violation — fix the pseudo-version before proceeding.
```

The current pin
(`v0.0.0-20260419063450-0c5745b5a986`) is acceptable because it resolves to
the commit `0c5745b5a986`. The KHA-574 acceptance criterion `no @main
dependency` is satisfied by the pseudo-version form itself.

## Step 3 — Build and vet

```sh
go build ./...
go vet ./...
```

Both must exit 0. If they fail, the bump is API-breaking — see
[`rollback.md`](rollback.md) or open a Plexium-side fix PR before retrying.

## Step 4 — Run the full test suite, including the contract tests

```sh
go test ./... -count=1 -timeout 180s
```

The contract tests under `internal/plugins/markedup/*_test.go` are the gate.
Specific tests that MUST pass after any bump:

| Test                                                  | What it covers                                                        |
| ----------------------------------------------------- | --------------------------------------------------------------------- |
| `TestEnricherContract_RelationshipsNotCollapsed`      | Wikilink `Relationships` and `SemanticRelationships` stay distinct.   |
| `TestEnricherContract_PageIDsPreserved`               | The `ID` field survives the enrich round-trip with no blanks.         |
| `TestEnricherContract_SummaryPreserved`               | The `Summary` field survives the enrich round-trip.                   |
| `TestEnricherContract_CaseNormalizedConfig`           | Upper-case YAML keys normalize the same way as lower-case keys.       |
| `TestRetrievalContract_PageIDsPreservedAcrossIndex`   | No blank/duplicate IDs in the loaded `*KnowledgeIndex`.               |
| `TestRetrievalContract_RoundTripEnrichRetrieve`       | The same page, ID, and title are observable through `markedup_search`. |
| `TestRetrievalContract_RawDirectoryExcluded`          | Pages under `raw/` do NOT appear in `markedup_search` results.        |
| `TestRetrievalContract_ToolRegistrationDistinct`      | `pageindex_search` and `markedup_search` are registered as distinct tools. |

If any of these tests fail, the bump is rejected. See
[`rollback.md`](rollback.md).

## Step 5 — Run the end-to-end probe

```sh
go run ./probes/kha-574 -mode verify
```

Expected output:

```
mode: verify
PASS: enrich writes graph metadata to manifest
PASS: enrich preserves Relationships separately from SemanticRelationships
PASS: enrich preserves page IDs and summaries across the boundary
PASS: case-normalized configuration parses upper-case keys identically
PASS: retrieve loads index and returns the enriched pages
PASS: markedup_search excludes raw/audit-private.md (F4 exclusion contract)
PASS: markedup_search and pageindex_search are distinct registered tools
all checks passed
```

Any `FAIL:` line blocks the bump. A `SKIP:` line means the probe was
configured to skip that check; do not interpret `SKIP:` as pass.

## Step 6 — Cross-check the public APIs surface

After the bump, diff the list in [`public-apis.md`](public-apis.md) against
the actual call sites:

```sh
grep -rn "Clarit-AI/markedup" internal/ cmd/ | grep -v "_test.go" | sort
```

Compare against the "Imports summary" block at the bottom of `public-apis.md`.
Any new import site means the bump added or moved a MarkedUp API call —
update `public-apis.md` in the same PR, and consider adding a contract test
for the new surface.

## Step 7 — Commit, push, PR

```sh
git add go.mod go.sum docs/integrations/markedup/ internal/plugins/markedup/ probes/kha-574/
git diff --cached --stat   # sanity check: only the expected files
git commit -m "chore(markedup): bump to <NEW_PIN>

Contract test results:
- go build: pass
- go vet: pass
- go test ./...: pass
- probe kha-574 verify: pass

Public API surface unchanged from <OLD_PIN>."

git push upstream bbrenner2217/kha-NNN-gate-markedup-bump-<short-sha>
gh pr create --base main \
  --title "chore(markedup): bump to <NEW_PIN>" \
  --body "KHA-NNN — MarkedUp bump gated by the KHA-574 contract tests."
```

If `git push upstream` is blocked by a local pre-push hook, use the
Core-Memory `push_files` integration per the KHA-287 playbook — **do not**
`gh auth switch` to bypass it.

## Known issues

### F4 — `raw/` directory is not excluded from `markedup_search`

The built-in `pageindex` indexer at
`internal/integrations/pageindex/index.go:50` excludes the `raw/` directory.
The MarkedUp retrieval plugin at
`internal/plugins/markedup/retrieval.go:71-83` does not, because
`index.Load` does not currently expose a directory-exclusion option
(`WithFilePattern` is basename-only, no `WithExcludeDirs`).

The contract test `TestRetrievalContract_RawDirectoryExcluded` documents the
expected behavior and fails against the current MarkedUp
`v0.0.0-20260419063450-0c5745b5a986`. It MUST pass before a bump merges —
either MarkedUp adds an exclusion option that Plexium adopts, or Plexium
implements the exclusion on the wrapper side (e.g. by walking the wiki root
with `filepath.SkipDir` in a Plexium-side loader wrapper).

This is **not** a reason to skip the bump. It IS a reason to fix the gap
before declaring the bump successful.

## What this procedure does NOT cover

- **Tier 2 (LLM) extraction changes.** Those go through
  `internal/plugins/markedup/llm_adapter.go` and the assistive cascade; a
  bump that changes the `enrich.ModelResult` shape will surface in
  `enricher_modelenrich_test.go` rather than the contract tests here.
- **MarkedUp CLI changes.** MarkedUp ships a `cmd/markedup` binary that
  Plexium never invokes directly. Any change there is Plexium-irrelevant.
- **MarkedUp cache format changes.** A bump that changes the on-disk shape
  of `.plexium/knowledge/` will surface in the first semantic-search run
  after upgrade; the contract tests don't catch that because they use a
  fresh temp directory every run.
