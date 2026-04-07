# Plexium Validation Suite — Test Architecture

> Post-build hardening. This document defines the testing strategy for Plexium's production readiness.

---

## Test Categories

### 1. Unit Tests (`*_test.go` in each package)
**Scope:** Individual functions and types in isolation.
**Blocking:** Yes — any unit test failure blocks release.
**Covers:**
- Manifest CRUD (load/save/upsert/remove/mapping)
- Hash computation (file, directory, batch)
- Glob/path matching
- Ownership checks and enforcement
- Frontmatter parsing
- Page classification (taxonomy)
- Compile helpers (groupBySection, slugFromPath, generate*)
- Filter logic (publish include/exclude)
- Dry-run write wrappers
- Config loading and validation
- Scanner glob resolution

### 2. Integration Tests (`validation/integration_test.go`)
**Scope:** Multi-package interactions using real filesystem fixtures.
**Blocking:** Yes — integration failures block release.
**Covers:**
- init → scaffold output verification
- Manifest + config → lint pipeline
- Manifest + config → publish selection
- Compile regeneration from manifest state
- Dry-run isolation across full command paths
- Preserve unmanaged pages behavior
- Migration/schema compatibility
- Convert pipeline → manifest state

### 3. End-to-End Tests (`validation/e2e_test.go`)
**Scope:** Complete workflow sequences against realistic repo fixtures.
**Blocking:** Yes — E2E failures block release.
**Covers:**
- Fresh repo → init → compile → lint → publish
- Brownfield repo → convert → compile → lint
- Source edits → staleness detection
- Human-authored page preservation across operations
- Idempotency (repeated runs produce same output)
- Publish with assets and exclusions

### 4. Determinism Tests (`validation/determinism_test.go`)
**Scope:** Same input produces identical output across multiple runs.
**Blocking:** Yes — nondeterminism in core paths blocks release.
**Covers:**
- Manifest JSON ordering (pages sorted by WikiPath)
- Hash stability (same content → same hash)
- Compile output stability (_index.md, _Sidebar.md)
- Lint report ordering
- Publish candidate set ordering
- Dry-run directory structure

### 5. Safety Invariant Tests (`validation/safety_test.go`)
**Scope:** Prove the system never violates core guarantees.
**Blocking:** Yes — invariant violations are critical.
**Invariants:**
- S1: Source files unchanged after any wiki operation
- S2: Dry-run produces zero live side effects
- S3: Human-authored pages never overwritten by managed updates
- S4: Init is non-destructive (re-run doesn't clobber existing files)
- S5: Publish respects exclude patterns
- S6: Compile/lint do not mutate wiki content (compile writes only nav files)
- S7: Manifest updates preserve existing entries not targeted by the operation

### 6. CLI Contract Tests (`validation/cli_test.go`)
**Scope:** Command existence, flag behavior, exit codes, output format.
**Blocking:** Flag/command existence failures block. Output format changes are warnings.
**Covers:**
- All registered commands exist and are reachable
- Required flags are enforced
- --dry-run produces no side effects
- --output-json produces valid JSON
- Exit codes: 0 for success, 1 for errors, 2 for warnings (lint --ci)
- Error messages are actionable (not panics or stack traces)

### 7. Lint Quality Tests (`validation/lint_quality_test.go`)
**Scope:** Each lint rule tested with positive and negative fixtures.
**Blocking:** False negatives (missed real issues) block. False positives are warnings.
**Rules tested:**
- Broken link detection (true positive + true negative)
- Orphan page detection
- Staleness detection
- Manifest drift detection
- Sidebar validation
- Frontmatter validation
- Severity classification correctness

### 8. Cross-Phase Contract Tests (`validation/contract_test.go`)
**Scope:** Data shapes and assumptions between packages remain compatible.
**Blocking:** Yes — contract drift blocks release.
**Covers:**
- Manifest struct fields consumed by lint, publish, compile, CI, hooks
- Config struct fields consumed by all commands
- Ownership values ("managed", "human-authored", "co-maintained") consistent everywhere
- PageEntry fields expected by compile match what manifest produces
- InitResult shape consumable by downstream commands

### 9. Golden/Regression Tests (`validation/golden_test.go`)
**Scope:** Stable artifacts match expected golden files.
**Blocking:** Unexpected changes are warnings (may be intentional). Missing golden files block.
**Artifacts:**
- Scaffolded .wiki/ directory structure
- Compiled _index.md and _Sidebar.md
- Default config.yml content
- Lint report JSON shape
- Manifest JSON shape (empty + populated)

### 10. Beads/Phase Compliance Audit (`validation/compliance_audit.go`)
**Scope:** Planned deliverables vs actual implementation.
**Blocking:** No — informational. Gaps are flagged for human review.
**Outputs:**
- Phase → implemented packages mapping
- Acceptance criteria → test coverage mapping
- Unimplemented or weakly tested AC
- Scope drift (implemented outside plan)

---

## What Is Out of Scope

- Performance benchmarks (no latency/throughput requirements yet)
- LLM integration testing (requires external API, tested via interface mocks)
- GitHub Wiki push testing (requires network, tested via dry-run + unit)
- Daemon long-running behavior (tested via short-cycle unit tests)
- Memento/beads CLI integration (tested via interface abstraction)

---

## What Counts as Blocking

| Severity | Meaning | Action |
|----------|---------|--------|
| **BLOCK** | Core invariant violated, data corruption possible, contract broken | Must fix before release |
| **WARN** | Surprising behavior, weak coverage, cosmetic contract drift | Should fix, track as debt |
| **INFO** | Style, minor gap, acceptable divergence from plan | Review only |

---

## Test Infrastructure

- **Fixture location:** `validation/testdata/`
- **Test location:** `validation/*_test.go`
- **Inspection scripts:** `validation/inspect/`
- **Golden files:** `validation/testdata/golden/`
- **Runner:** `validation/run_validation.sh`
- **Report output:** `validation/VALIDATION-REPORT.md` (generated)

---

## Running

```bash
# Full suite
./validation/run_validation.sh

# Individual categories
go test ./validation/ -run TestSafety -v
go test ./validation/ -run TestDeterminism -v
go test ./validation/ -run TestContract -v
go test ./validation/ -run TestE2E -v
go test ./validation/ -run TestCLI -v
go test ./validation/ -run TestLintQuality -v
go test ./validation/ -run TestGolden -v
```
