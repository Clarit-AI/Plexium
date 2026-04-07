# Phase 0: Project Setup

> **Model:** Budget — Sonnet 4 mini (primary), GLM-5, Minimax acceptable
> **Execution:** Solo Agent
> **Status:** Complete
> **bd Epic:** `plexium-p0`
> **Prerequisites:** Language choice (Go vs Rust), CI provider selection

## Objective

Initialize the repository, select toolchain, configure build tools (bd, memento), and establish the development workflow before any Plexium code is written. This phase must be completed before any milestone work begins.

## Prerequisite Decisions

These decisions must be resolved before or during Phase 0:

| Decision | Options | Recommendation |
|----------|---------|----------------|
| **Language** | Go or Rust | Go: faster prototyping, easier cross-compilation via `go build`. Rust: safer, single binary, but slower iteration. |
| **Package name** | `github.com/Clarit-AI/Plexium` | Match the actual repo |
| **CI provider** | GitHub Actions (assumed) | Only GitHub Actions supported in Phase 1-8 |
| **Test framework** | Standard library + testify (Go) or standard library + rstest (Rust) | Keep it simple |

## Architecture Context

This phase creates the physical project structure. See:
- [Vault Structure](../architecture/core-architecture.md#vault-structure) — understand what `.wiki/` and `.plexium/` directories contain
- No other architecture context needed for Phase 0

## Spec Sections Covered

- §9 The CLI — `plexium init` command scaffolding
- §22 Phased Delivery Plan — Phase 0 scope

## Tasks

### T0.1: Initialize Repository

```bash
# If starting fresh (repo may already exist):
git init
git add .
git commit -m "chore: initial commit"

# Verify memento captures this session:
git memento doctor
```

### T0.2: Create Directory Structure

```
plexium/                     # Or your chosen package name
├── cmd/
│   └── plexium/
│       └── main.go          # CLI entry point
├── internal/
│   ├── config/              # Config loading
│   ├── scanner/             # File scanner with glob support
│   ├── markdown/            # Normalizer, frontmatter extraction/injection
│   ├── template/            # Page template engine
│   ├── wiki/                 # Wiki operations (read, write, link)
│   ├── manifest/            # Manifest CRUD, hash computation
│   ├── publish/              # GitHub Wiki publishing
│   ├── lint/                 # Deterministic lint checks
│   ├── convert/              # Brownfield conversion pipeline
│   ├── plugins/              # Agent adapter plugins
│   ├── generate/             # Page generators (module, decision, concept)
│   └── reports/             # Report generation
├── .plexium/                 # Plexium config (created by CLI, not here)
├── .wiki/                    # Wiki vault (created by CLI, not here)
└── docs/                     # Phase doc structure already in place
```

### T0.3: Set Up Language Toolchain

**If Go:**
```bash
go mod init github.com/Clarit-AI/Plexium
go get github.com/spf13/cobra@latest    # CLI framework
go get github.com/spf13/viper@latest    # Config management
go get gopkg.in/yaml.v3@latest          # YAML parsing
go get github.com/stretchr/testify@latest # Testing
```

**If Rust:**
```bash
cargo init --name plexium
cargo add clap@4                       # CLI framework (with derive macros)
cargo add serde_yaml@0.9               # YAML parsing
cargo add serde@1 --features derive    # Serialization
cargo add tempdir@0.3                  # Testing utilities
```

### T0.4: Create Initial CI Skeleton

**.github/workflows/plexium-ci.yml:**
```yaml
name: Plexium CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Set up toolchain
        run: |
          # Go: setup-go@v4 or
          # Rust: actions-rust-lang/setup-rust-toolchain@v1
      - name: Run tests
        run: |
          # Go: go test ./...
          # Rust: cargo test
      - name: Lint
        run: |
          # Go: golangci-lint run
          # Rust: cargo clippy -- -D warnings
```

### T0.5: Initialize bd (Beads)

```bash
# Verify bd is installed
bd --version

# Initialize the project with bd
bd init

# Create epics for all 10 milestones
bd epic add plexium-p0 "Project Setup" --status done
bd epic add plexium-m1 "CLI Foundation" --status todo
bd epic add plexium-m2 "Page Generation" --status todo
bd epic add plexium-m3 "State & Publishing" --status todo
bd epic add plexium-m4 "Convert (Brownfield)" --status todo
bd epic add plexium-m5 "Agent Adapters" --status todo
bd epic add plexium-m6 "Deterministic Lint" --status todo
bd epic add plexium-m7 "Reporting & Obsidian" --status todo
bd epic add plexium-m8 "Enforcement" --status todo
bd epic add plexium-m9 "Tool Integrations" --status todo
bd epic add plexium-m10 "Orchestration" --status todo

# Create initial Phase 0 tasks
bd task add plexium-p0 "Choose language (Go or Rust)" --priority high
bd task add plexium-p0 "Set up toolchain and directory structure" --priority high
bd task add plexium-p0 "Initialize bd epics" --priority high
bd task add plexium-p0 "Initialize memento" --priority high
bd task add plexium-p0 "Create CI skeleton" --priority medium
bd task add plexium-p0 "First commit with session captured" --priority high
```

### T0.6: Initialize memento

```bash
# Verify memento is installed
git memento --version || brew install memento  # or appropriate install

# Initialize memento for this repo
git memento init

# Verify it's working
git memento doctor

# Create initial commit — memento will capture this session
git add .
git commit -m "chore: project setup

- Initialize repository structure
- Choose language: <FILL IN: Go or Rust>
- Configure bd and memento
- Create CI skeleton"
```

### T0.7: Create CLAUDE.md

Create `CLAUDE.md` at repo root (will be expanded by later phases):

```markdown
# Plexium

Self-documenting repositories via Karpathy's LLM Wiki pattern.

## Build Guide

Primary build orchestration: `docs/phases/OVERVIEW.md`
Architecture reference: `docs/architecture/core-architecture.md`
Archived specification: `docs/reference/plexium-spec-full.md`

## Build Tooling

- **bd**: Task management. `bd stats` shows milestone progress.
- **memento**: Session provenance on every commit.

## Current Phase

Phase 0: Project Setup (in progress)

## Quick Commands

```bash
# Build
go build -o plexium ./cmd/plexium

# Test
go test ./...

# Run CLI
./plexium --help
```
```

### T0.8: Create .plexium/ Directory Stub

```bash
mkdir -p .plexium/
```

Placeholder for config.yml (generated by `plexium init` in Phase 3, but directory should exist now).

## Interfaces

**Provides to Phase 1:**
- CLI binary skeleton with `plexium --help`
- Config loader stub
- Test infrastructure
- CI pipeline that runs
- `bd` epics for all milestones

**Consumes from:** Nothing (this is the starting point)

## Acceptance Criteria

- [ ] Language chosen and toolchain installed
- [ ] Project structure created (`cmd/`, `internal/`, `.plexium/`)
- [ ] `bd stats` shows all milestone epics
- [ ] `git memento doctor` passes
- [ ] CI pipeline runs (even if only "hello world" test)
- [ ] CLAUDE.md references phase docs
- [ ] First commit made with memento session captured

## bd Task Mapping

```
plexium-p0
├── ✅ T0.1: Initialize repository
├── ✅ T0.2: Create directory structure
├── ✅ T0.3: Set up language toolchain
├── ✅ T0.4: Create CI skeleton
├── ✅ T0.5: Initialize bd epics
├── ✅ T0.6: Initialize memento
├── ✅ T0.7: Create CLAUDE.md
└── ✅ T0.8: Create .plexium/ stub
```

## Notes

- **Do not implement any CLI commands in Phase 0.** Only scaffold the binary skeleton. Command implementation starts in Phase 1.
- **Do not create `config.yml` yet.** That will be generated by `plexium init` in Phase 3.
- **Language choice is the only blocking decision.** All other Phase 0 tasks are scaffold-only.
