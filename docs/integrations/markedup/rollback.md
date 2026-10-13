# MarkedUp Rollback

If a MarkedUp bump fails any step of the
[`upgrade-procedure.md`](upgrade-procedure.md), revert the pin to the last
known-good state and re-run the gates. **Breaking semantics fail before
release** — this is the recovery path.

## Step 1 — Identify the last known-good pin

The current known-good pin (as of the KHA-574 gate):

```
github.com/Clarit-AI/markedup v0.0.0-20260419063450-0c5745b5a986
```

For past bumps, look at `git log --oneline -- go.mod` for the most recent
"chore(markedup):" or "fix(markedup):" commit. The pin before that commit is
the rollback target.

## Step 2 — Revert the pin

```sh
# Pin back to the known-good version
go get github.com/Clarit-AI/markedup@v0.0.0-20260419063450-0c5745b5a986
go mod tidy

# Verify the diff
git diff go.mod go.sum
# Expected: only the markedup line in go.mod + the corresponding go.sum entries
```

## Step 3 — Re-run the gates

```sh
go build ./...
go vet ./...
go test ./... -count=1 -timeout 180s
go run ./probes/kha-574 -mode verify
```

All four MUST exit 0. If they don't, the rollback didn't actually land — check
that `go.mod` shows the old pin (not the new one) and that no test files
were left referencing the new API surface.

## Step 4 — Commit the revert

```sh
git add go.mod go.sum
git commit -m "chore(markedup): revert to <OLD_PIN> after failed bump

Rollback executed because the <NEW_PIN> bump failed step <N> of the
KHA-574 upgrade procedure:
- <test name or command that failed>
- <one-line summary of the failure>

See docs/integrations/markedup/upgrade-procedure.md."

git push upstream <branch>
gh pr create --base main \
  --title "chore(markedup): revert to <OLD_PIN>" \
  --body "Rollback of a MarkedUp bump that failed the KHA-574 gate."
```

If the original bump PR was merged before the failure surfaced, the rollback
PR becomes an emergency hotfix — open it against `main` directly per the dev
SOP, not against a feature branch.

## Step 5 — Diagnose the underlying break

The rollback is a stopgap. After it lands, the next step is to figure out why
the bump broke the contract. Common failure modes:

| Failure                                                          | Likely cause                                                    |
| ---------------------------------------------------------------- | --------------------------------------------------------------- |
| `TestEnricherContract_RelationshipsNotCollapsed` fails            | MarkedUp merged `Relationships` and `SemanticRelationships`.    |
| `TestEnricherContract_PageIDsPreserved` fails                    | MarkedUp rewrites or strips the `ID` field during enrichment.   |
| `TestEnricherContract_SummaryPreserved` fails                    | MarkedUp drops or moves the `Summary` field.                    |
| `TestRetrievalContract_PageIDsPreservedAcrossIndex` fails        | MarkedUp's `KnowledgeIndex` allows duplicate or blank IDs.      |
| `TestRetrievalContract_RawDirectoryExcluded` fails (still)       | The bump didn't fix the F4 gap. Open a MarkedUp issue or a Plexium wrapper fix. |
| `go build` fails                                                 | MarkedUp renamed or removed a public API we call. Update `internal/plugins/markedup/*` and `docs/integrations/markedup/public-apis.md` before re-bumping. |
| `go vet` fails                                                   | Likely a struct-tag or exported-name change in markedup.        |

After diagnosis, either:

1. **Open a MarkedUp issue / PR** for the upstream fix, then re-bump once
   the fix lands, **or**
2. **Open a Plexium-side PR** that works around the change (e.g. add a
   field-mapping shim in `toGraphMetadata`), then re-bump.

Do NOT silently bump past the contract. The KHA-574 gate is the whole point.
