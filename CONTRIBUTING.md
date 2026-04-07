# Contributing to Plexium

Contributions are welcome. This guide covers how to build, test, and submit changes.

---

## Prerequisites

- **Go 1.25+**
- **Git**
- **lefthook** (optional, for git hooks): `brew install lefthook` or see [lefthook docs](https://github.com/evilmartians/lefthook)

---

## Build

```bash
git clone https://github.com/Clarit-AI/Plexium.git
cd Plexium
go build -o plexium ./cmd/plexium
```

---

## Test

### Package tests

```bash
go test ./internal/... -count=1 -timeout 120s
```

### Validation suite

The validation suite covers safety invariants, determinism guarantees, cross-phase contracts, end-to-end workflows, lint quality, CLI contracts, and golden tests.

```bash
go test ./validation/ -count=1 -timeout 180s -v
```

### Full validation run

```bash
./validation/run_validation.sh
```

This builds the binary, runs all package tests, runs the validation suite, checks determinism and safety invariants, and generates `validation/VALIDATION-REPORT.md`.

### Individual test categories

```bash
go test ./validation/ -run TestSafety -v
go test ./validation/ -run TestDeterminism -v
go test ./validation/ -run TestContract -v
go test ./validation/ -run TestE2E -v
go test ./validation/ -run TestLintQuality -v
go test ./validation/ -run TestCLI -v
go test ./validation/ -run TestGolden -v
```

### Compliance audit

```bash
go run ./validation/inspect/compliance_audit.go
```

---

## Code Style

- Format with `gofmt`
- Follow standard Go conventions
- Package-level tests go in `*_test.go` files alongside the code
- Validation-level tests go in `validation/`

---

## PR Workflow

1. Create a branch from `main`
2. Make your changes
3. Run tests: `go test ./internal/... && go test ./validation/`
4. Verify the binary builds: `go build ./cmd/plexium`
5. Submit a PR with a clear description of what changed and why

One logical change per PR. If a PR touches multiple unrelated areas, split it.

---

## Task Tracking with bd (Beads)

Plexium uses [bd (beads)](https://github.com/mandel-macaque/beads) for task management.

```bash
bd stats           # See all milestone epics and status
bd ready           # See next actionable tasks
bd dolt push       # Push beads data to remote (run before git push)
```

---

## Session Provenance with Memento

Plexium uses [memento](https://github.com/mandel-macaque/memento) to capture session context on commits.

```bash
git memento commit <session-id> -m "commit message"
```

Use `git memento commit` instead of `git commit` when working in a coding session to capture provenance.
