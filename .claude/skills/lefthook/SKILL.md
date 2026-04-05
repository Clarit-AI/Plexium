---
name: lefthook
description: This skill should be used when the user asks to "configure git hooks", "set up lefthook", "add a pre-commit hook", "create a lefthook config", "install git hooks", or mentions lefthook, .lefthook.yml, git hooks management, or hook enforcement in the context of Plexium or any project using Lefthook.
---

# Lefthook — Fast Git Hooks Manager

Lefthook is a dependency-free Go binary that manages Git hooks via YAML config. It supports parallel execution, glob-based file filtering, script runners, Docker, and remote shared configs.

## Installation

```bash
# Go
go install github.com/evilmartians/lefthook@latest

# NPM
npm install lefthook --save-dev

# Ruby
gem install lefthook

# Python
pip install lefthook

# Homebrew
brew install lefthook
```

## Core Workflow

```bash
lefthook install       # Install hooks into .git/hooks/
lefthook run pre-commit # Manually run a specific hook
lefthook validate       # Check config syntax
lefthook add pre-push   # Add a new hook section to config
lefthook uninstall      # Remove hooks
lefthook dump           # Export resolved config
lefthook self-update    # Update binary
```

## Quick Reference: .lefthook.yml Structure

```yaml
# Global options
assert_lefthook_installed: true
skip_lfs: false
colors: true
source_dir: .lefthook/
source_dir_local: .lefthook-local/

# Hook definitions — one section per Git hook
pre-commit:
  parallel: true
  commands:
    lint:
      glob: "*.{js,ts}"
      run: npx eslint {staged_files}
      stage_fixed: true
    format:
      glob: "*.{js,ts,json,md}"
      run: npx prettier --write {staged_files}
      stage_fixed: true
      priority: 1  # Runs first (lower = earlier)

pre-push:
  commands:
    test:
      run: npm test
      skip: merge  # Skip during merge commits

commit-msg:
  scripts:
    validate:
      runner: bash
```

## Template Variables

| Variable | Expands To |
|----------|-----------|
| `{staged_files}` | Git staged files |
| `{files}` | Files matched by glob after filtering |
| `{push_files}` | Files being pushed |
| `{all_files}` | All repo files |
| `{0}` | All CLI args to hook |
| `{1}`, `{2}` | Positional args |
| `{lefthook_job_name}` | Current job name |

## Commands vs. Scripts vs. Jobs

| Type | Config Key | When to Use |
|------|-----------|-------------|
| **Command** | `commands:` | Inline shell commands. Most common. |
| **Script** | `scripts:` | Separate script files in `.lefthook/<hook>/`. Specify `runner:` (bash, node, python). |
| **Job** | `jobs:` | Unified interface supporting both `run:` and `script:`. Supports `group:` for nested parallel/piped execution. |

## Key Execution Options

| Option | Type | Effect |
|--------|------|--------|
| `parallel` | bool | Run commands concurrently |
| `piped` | bool | Chain command outputs |
| `follow` | bool | Continue on failure |
| `skip` | merge/rebase/bool | Skip conditionally |
| `only` | merge/rebase | Run only during |
| `tags` | [string] | Tag-based filtering |
| `exclude_tags` | [string] | Exclude tagged commands |
| `priority` | int | Execution order (lower = first) |
| `stage_fixed` | bool | Auto-stage modified files |
| `interactive` | bool | Allow user input |
| `root` | string | Working directory |
| `env` | map | Environment variables |

## Plexium Integration Pattern

Plexium uses Lefthook as the enforcement layer for wiki maintenance:

```yaml
# .lefthook.yml (Plexium project)
pre-commit:
  parallel: false  # Wiki checks must be sequential
  commands:
    wiki-sync:
      glob: "src/**"
      run: plexium hook pre-commit
      fail_text: "Wiki sync failed. Run 'plexium sync' to fix."

commit-msg:
  scripts:
    wiki-log:
      runner: bash
      # Lives at .lefthook/commit-msg/wiki-log
```

## Additional Resources

### Reference Files

- **`references/configuration.md`** — Full config schema with all options, remote configs, and advanced patterns
- **`references/cli-commands.md`** — Complete CLI command reference with flags and examples
- **`references/hook-execution.md`** — Execution modes, file filtering, conditional execution, and debugging
