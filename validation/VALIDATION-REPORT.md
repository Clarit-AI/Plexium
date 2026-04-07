# Plexium Validation Report

Generated: 2026-04-07T00:37:15Z

## Build Status

| Check | Status |
|-------|--------|
| Binary builds | PASS |
| Package tests | PASS (22 packages) |
| Validation suite | PASS (100 tests) |
| Determinism | PASS |
| Safety invariants | PASS |

## Test Coverage

| Category | Count |
|----------|-------|
| CLI commands | 22 |
| Go packages | 23 |
| Test files | 53 |
| Test functions | 540 |

## Blocking Issues

None.

## Known Findings

1. **_schema.md lint false positive**: Generated schema contains `[[wiki-links]]` as documentation
   syntax examples. The link crawler flags these as broken. Not a code bug — the link crawler
   correctly finds unresolvable links. Fix: either escape the examples or exclude _schema.md.

2. **UnmanagedPages vs Pages ownership gap**: Human-authored page protection in UpsertPage only
   checks the Pages list. A page tracked only in UnmanagedPages can be added to Pages as managed.
   Low risk — the distinction is intentional (unmanaged = not in manifest, human-authored = in manifest).

3. **Freshly initialized pages trigger frontmatter lint**: Scaffolded pages have minimal frontmatter
   (title, ownership, last-updated) but the schema prescribes additional fields. Expected behavior —
   pages get fuller frontmatter as agents work on them.

## Invariants Proven

- S1: Source files unchanged after init, compile, lint
- S2: Dry-run init/compile produces no live writes
- S3: Human-authored pages in Pages list cannot be overwritten by managed
- S4: Init is non-destructive on re-run (skips existing files)
- S5: Compile only writes _index.md and _Sidebar.md
- S6: Manifest upsert/remove preserves unrelated entries

## Determinism Proven

- D1: Manifest pages sorted by WikiPath on every save
- D2: Hash of same content produces same result across runs
- D3: Compile output identical across 5 consecutive runs
- D4: Lint results stable across 3 consecutive runs
- D5: Empty manifest compile produces stable minimal output
- D6: Manifest JSON shape is stable

## Cross-Phase Contracts Verified

- PageEntry struct fields locked (10 fields)
- SourceFile struct fields locked (3 fields)
- UnmanagedEntry struct fields locked (3 fields)
- Manifest top-level fields locked (5 fields)
- Config struct fields locked (16 top-level)
- Wiki config fields verified (7 fields)
- Ownership values: "managed", "human-authored", "co-maintained"
- Lint report JSON shape stable
- Lint exit codes: 0=clean, 1=errors, 2=warnings
- Config validation catches missing required fields
