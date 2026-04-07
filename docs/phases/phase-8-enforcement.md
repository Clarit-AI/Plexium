# Phase 8: Enforcement

> **Model:** Mid-tier — Sonnet 4 (primary), GPT 4.1, Gemini 2.5 Flash acceptable
> **Execution:** Solo Agent
> **Status:** Complete (implemented 2026-04-06)  
> **bd Epic:** `plexium-m8`  
> **Prerequisites:** Phase 7 complete

## Objective

Implement the enforcement layer: Git hooks via Lefthook (pre-commit, post-commit), strictness levels, GitHub Actions CI workflows, WIKI-DEBT tracking, and the `plexium migrate` command for schema versioning.

## Architecture Context

- [Invariants](../architecture/core-architecture.md#invariants--failure-modes) — Never-violated rules enforced by hooks
- [Configuration](../architecture/core-architecture.md#configuration) — `enforcement` and `strictness` config

## Spec Sections Covered

- §11 Enforcement Layers (Layers 1-3: Schema, Git hooks, CI/CD)
- §9 The CLI (`plexium hook pre-commit`, `plexium hook post-commit`, `plexium ci check`, `plexium migrate`)

## Deliverables

1. **Lefthook integration** — pre-commit and post-commit hooks via Lefthook
2. **`plexium hook pre-commit`** — Check if wiki updated when source changes
3. **`plexium hook post-commit`** — Track WIKI-DEBT on `--no-verify` bypass
4. **Strictness levels** — strict/moderate/advisory behavior
5. **GitHub Actions workflows** — `plexium-lint.yml`, `plexium-sync.yml`
6. **`plexium ci check` command** — Diff-aware wiki check for PRs
7. **WIKI-DEBT tracking** — Entries in `_log.md` for skipped wiki updates
8. **`plexium migrate` command** — Schema version migration

## Tasks

### M8.1: Lefthook Integration

Set up Lefthook configuration for Plexium.

**Installation (called by `plexium init`):**
```bash
# Add to .gitignore if not using system lefthook
# lefthook.yaml is committed to repo

# Run once during plexium init:
lefthook install
```

**lefthook.yml:**
```yaml
pre-commit:
  commands:
    plexium-check:
      run: plexium hook pre-commit {staged_files}
      fail_text: |
        ⚠️  Code files changed but .wiki/ was not updated.
        Ask your coding agent to document the changes, or run:
          plexium sync
        To bypass (with audit trail): git commit --no-verify

post-commit:
  commands:
    plexium-audit:
      run: plexium hook post-commit
```

### M8.2: plexium hook pre-commit

Check if wiki was updated when source files changed.

**Command:**
```bash
plexium hook pre-commit [--staged-files FILE1 FILE2 ...]
```

**Implementation:**
```go
// cmd/hook_precommit.go
type PreCommitHook struct {
    config      *config.Config
    manifestMgr *manifest.Manager
}

func (h *PreCommitHook) Run(stagedFiles []string) (*HookResult, error)

type HookResult struct {
    Allowed   bool
    Strictness string  // "strict", "moderate", "advisory"
    Reason    string
    FilesChanged []string
    WikiUpdated bool
}
```

**Logic:**
1. Get list of staged files from `git diff --cached --name-only`
2. If no staged files, exit 0
3. Filter to source files (match `sources.include` globs, not `sources.exclude`)
4. If no source files changed, exit 0
5. Check if any `.wiki/` files are also staged
6. If wiki updated: exit 0
7. If wiki NOT updated: apply strictness behavior

**Strictness behavior:**

| Strictness | Behavior |
|------------|----------|
| `strict` | Hard reject. Exit 1. No bypass without `--no-verify`. |
| `moderate` | Warn. Exit 1 unless `PLEXIUM_BYPASS_HOOK=1` or `--no-verify`. |
| `advisory` | Warn. Exit 0. Log the skip. |

**Environment variables:**
- `PLEXIUM_BYPASS_HOOK=1`: Explicit bypass for CI automation

### M8.3: plexium hook post-commit

Track WIKI-DEBT when `--no-verify` was used.

**Command:**
```bash
plexium hook post-commit
```

**Implementation:**
```go
// cmd/hook_postcommit.go
type PostCommitHook struct {
    config      *config.Config
    manifestMgr *manifest.Manager
}

func (h *PostCommitHook) Run() error {
    // 1. Check if --no-verify was used (via commit msg or git env)
    // 2. If yes, get the commit SHA
    // 3. Get list of files changed in that commit
    // 4. Append WIKI-DEBT entry to .wiki/_log.md
}
```

**WIKI-DEBT entry format:**
```markdown
## [YYYY-MM-DD] WIKI-DEBT | Commit abc123 bypassed wiki check
- Files changed: src/auth/middleware.ts
- Bypassed by: developer (--no-verify)
- Status: pending wiki update
```

### M8.4: plexium-lint.yml Workflow

GitHub Actions workflow for lint on PRs.

**File:** `.github/workflows/plexium-lint.yml`

```yaml
name: Plexium Lint

on:
  pull_request:
    branches: [main, develop]

jobs:
  wiki-check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
          submodules: true
          sparse-checkout: |
            .wiki/
            .plexium/
            src/
            docs/
            README.md
          sparse-checkout-cone-mode: false

      - name: Install Plexium
        run: curl -fsSL https://plexium.dev/install.sh | sh

      - name: Deterministic checks
        run: |
          plexium ci check \
            --base ${{ github.event.pull_request.base.sha }} \
            --head ${{ github.event.pull_request.head.sha }} \
            --output .plexium/reports/ci-check.json

      - name: Lint wiki integrity
        run: plexium lint --ci --fail-on error

      - name: Post results to PR
        if: always()
        uses: actions/github-script@v7
        with:
          script: |
            // Post formatted report as PR comment

      - name: Memento gate
        if: hashFiles('.plexium/config.yml') != ''
        run: |
          if grep -q 'memento: true' .plexium/config.yml; then
            git memento check --gate
          fi
```

### M8.5: plexium-sync.yml Workflow

GitHub Actions workflow for sync on merge to main.

**File:** `.github/workflows/plexium-sync.yml`

```yaml
name: Plexium Sync

on:
  push:
    branches: [main]
  workflow_dispatch:

jobs:
  sync-and-publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
          submodules: true

      - name: Install Plexium
        run: curl -fsSL https://plexium.dev/install.sh | sh

      - name: Sync wiki
        run: plexium sync --ci

      - name: Publish to GitHub Wiki
        env:
          GH_TOKEN: ${{ secrets.WIKI_PUSH_TOKEN }}
        run: plexium gh-wiki-sync --push

      - name: Emit sync report
        run: plexium report --format markdown --output $GITHUB_STEP_SUMMARY
```

### M8.6: plexium ci check Command

Diff-aware wiki check for CI pipelines.

**Command:**
```bash
plexium ci check --base SHA --head SHA [--output FILE]
```

**Implementation:**
```go
// cmd/ci_check.go
type CICheck struct {
    config      *config.Config
    manifestMgr *manifest.Manager
}

type CICheckResult struct {
    Commit         string `json:"commit"`
    WikiDebt       []WikiDebtEntry
    UntrackedChanges []string
    Passes         bool
}

type WikiDebtEntry struct {
    Commit  string
    Message string
    Files   []string
}

func (c *CICheck) Run(baseSHA, headSHA string) (*CICheckResult, error)
```

**Logic:**
1. Get files changed between base and head commits
2. For each changed source file, check if it has a wiki mapping in manifest
3. Check if wiki page was updated in same commit range
4. Accumulate WIKI-DEBT entries from `_log.md` since last CI run
5. Return non-zero if wiki debt exceeds threshold

### M8.7: plexium migrate Command

Apply schema migrations across existing wiki pages.

**Command:**
```bash
plexium migrate [--dry-run] [--version 2]
```

**Migration files location:** `.plexium/migrations/`

**Naming:** `001_add_tags_field.sql`, `002_rename_sections.sh`, etc.

**Implementation:**
```go
// cmd/migrate.go
type Migrator struct {
    config      *config.Config
    manifestMgr *manifest.Manager
    wikiPath    string
}

func (m *Migrator) Migrate(targetVersion int, dryRun bool) error {
    // 1. Read current schema version from _schema.md
    // 2. Find migration scripts in .plexium/migrations/
    // 3. Apply each migration in order
    // 4. Update schema version
    // 5. Emit migration report
}
```

**Migration script interface:**
```bash
# .plexium/migrations/001_add_tags_field.sh
#!/bin/bash
# Adds 'tags' field to frontmatter of all pages

for f in $(find .wiki -name "*.md" -not -path "./.wiki/.obsidian/*"); do
    if ! grep -q "^tags:" "$f"; then
        # Insert tags: [] after review-status line
        sed -i '/^review-status:/a tags: []' "$f"
    fi
done
```

### M8.8: Scheduled Lint Workflow

Add weekly scheduled lint run.

**File:** `.github/workflows/plexium-scheduled-lint.yml`

```yaml
name: Plexium Scheduled Lint

on:
  schedule:
    - cron: '0 9 * * 1'  # Monday 9am

jobs:
  weekly-lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
          submodules: true

      - name: Install Plexium
        run: curl -fsSL https://plexium.dev/install.sh | sh

      - name: Full lint
        run: plexium lint --full --ci --output .plexium/reports/weekly-lint.json

      - name: Upload report
        uses: actions/upload-artifact@v4
        with:
          name: plexium-weekly-lint
          path: .plexium/reports/weekly-lint.json
```

## Interfaces

**Consumes from Phase 6:**
- Doctor command (used by hook validation)
- Lint command

**Consumes from Phase 7:**
- gh-wiki-sync (used by sync workflow)

**Provides to Phase 9:**
- Hook infrastructure (memento integration uses hooks)
- CI workflows (memento gate added to lint workflow)

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | Lefthook pre-commit fires on `git commit` |
| AC2 | Pre-commit detects source files changed without wiki update |
| AC3 | Strict mode rejects commit without wiki update |
| AC4 | Moderate mode warns but allows commit |
| AC5 | Advisory mode logs and allows commit |
| AC6 | Post-commit logs WIKI-DEBT when `--no-verify` used |
| AC7 | CI workflow runs on PR |
| AC8 | CI workflow posts results as PR comment |
| AC9 | `plexium ci check` diffs base vs head SHA |
| AC10 | `plexium ci check` returns non-zero if wiki debt exceeds threshold |
| AC11 | `plexium migrate` applies migration scripts in order |
| AC12 | `plexium migrate --dry-run` shows what would change |
| AC13 | Scheduled lint runs weekly |
| AC14 | WIKI-DEBT entries appear in `_log.md` |

## bd Task Mapping

```
plexium-m8
├── M8.1: Lefthook integration
├── M8.2: plexium hook pre-commit
├── M8.3: plexium hook post-commit
├── M8.4: plexium-lint.yml workflow
├── M8.5: plexium-sync.yml workflow
├── M8.6: plexium ci check command
├── M8.7: plexium migrate command
└── M8.8: Scheduled lint workflow
```
