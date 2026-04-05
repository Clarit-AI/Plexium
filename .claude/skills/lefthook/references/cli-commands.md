# CLI Commands

Lefthook provides a comprehensive command-line interface for managing Git hooks across different development environments.

## Installation Commands

### install

Install Git hooks in the current repository.

```bash { .api }
lefthook install [flags]

# Flags:
--force, -f    # Overwrite .old hooks (deprecated, no longer needed)
```

**Usage Example:**
```bash
# Install hooks in repository
lefthook install

# Install with verbose output
lefthook install --verbose
```

### uninstall

Remove Git hooks from the current repository.

```bash { .api }
lefthook uninstall [flags]

# Flags:
--force, -f           # Remove all git hooks even not lefthook-related
--remove-configs, -c  # Remove lefthook main and secondary config files
```

**Usage Example:**
```bash
# Remove all lefthook hooks
lefthook uninstall
```

### check_install

Verify the installation status of Git hooks.

```bash { .api }
lefthook check_install [flags]

# No additional flags beyond global options
```

**Usage Example:**
```bash
# Check if hooks are properly installed
lefthook check_install
```

## Hook Management Commands

### run

Execute specific hooks or hook commands.

```bash { .api }
lefthook run <hook_name> [git_args...] [flags]

# Arguments:
#   hook_name  Required. Git hook name (pre-commit, pre-push, etc.)
#   git_args   Optional. Git hook arguments passed by Git itself

# Flags:
--force, -f           # Force execution of commands that can be skipped
--all-files           # Run on all files instead of changed files
--no-tty, -n         # Run hook non-interactively, disable spinner
--no-auto-install    # Skip updating git hooks
--skip-lfs           # Skip running git lfs
--files-from-stdin   # Get files from standard input, null-separated
--file strings       # Run on specified file (repeat for multiple files)
--exclude strings    # Exclude specified file (repeat for multiple files)
--commands strings   # Run only specified commands
--jobs strings       # Run only specified jobs
```

**Available Hook Names:**
- `applypatch-msg`
- `pre-applypatch`
- `post-applypatch`
- `pre-commit`
- `pre-merge-commit`
- `prepare-commit-msg`
- `commit-msg`
- `post-commit`
- `pre-rebase`
- `post-checkout`
- `post-merge`
- `pre-push`
- `pre-receive`
- `update`
- `proc-receive`
- `post-receive`
- `post-update`
- `reference-transaction`
- `push-to-checkout`
- `pre-auto-gc`
- `post-rewrite`
- `sendemail-validate`
- `fsmonitor-watchman`
- `p4-changelist`
- `p4-prepare-changelist`
- `p4-post-changelist`
- `p4-pre-submit`
- `post-index-change`

**Usage Examples:**
```bash
# Run all pre-commit hooks
lefthook run pre-commit

# Run specific commands in pre-commit hook
lefthook run pre-commit --commands lint,test

# Run specific jobs in pre-commit hook
lefthook run pre-commit --jobs build,deploy

# Run on all files
lefthook run pre-commit --all-files

# Force execution
lefthook run pre-commit --force

# Run with custom files
lefthook run pre-commit --file src/app.js --file src/utils.js
```

### add

Add new hook configuration to the lefthook configuration file.

```bash { .api }
lefthook add <hook_name> [flags]

# Arguments:
#   hook_name  Required. Git hook name to add

# Flags:
--dirs, -d     # Create directory for scripts
--force, -f    # Overwrite .old hooks
```

**Usage Example:**
```bash
# Add pre-commit hook configuration
lefthook add pre-commit

# Add pre-push hook configuration  
lefthook add pre-push
```

## Configuration Commands

### validate

Validate the lefthook configuration file syntax and structure.

```bash { .api }
lefthook validate [flags]

# No additional flags beyond global options
```

**Usage Example:**
```bash
# Validate configuration
lefthook validate

# Validate with verbose output
lefthook validate --verbose
```

### dump

Export the current configuration to stdout.

```bash { .api }
lefthook dump [flags]

# Flags:
--format, -f   # Output format: 'yaml', 'toml', or 'json'
--json, -j     # Dump in JSON format (deprecated)
--toml, -t     # Dump in TOML format (deprecated)
```

**Usage Example:**
```bash
# Export configuration
lefthook dump

# Export and save to file
lefthook dump > exported-config.yml
```

## Utility Commands

### version

Display version information.

```bash { .api }
lefthook version [flags]

# Flags:
--full, -f     # Full version with commit hash
```

**Usage Example:**
```bash
# Show version
lefthook version
```

### self-update

Update lefthook binary to the latest version.

```bash { .api }
lefthook self-update [flags]

# Flags:
--yes, -y      # No prompt
--force, -f    # Force upgrade
--verbose, -v  # Show verbose logs
```

**Usage Example:**
```bash
# Update to latest version
lefthook self-update
```

## Global Flags

All commands support these global flags:

```bash { .api }
--verbose, -v     # Enable verbose output
--colors string   # Color output: 'auto', 'on', or 'off' (default: 'auto')
--no-colors       # Disable colored output (deprecated, use --colors=off)
```

## Command Completion

Lefthook supports shell auto-completion for commands and hook names. The completion system provides:

- Command name completion
- Hook name completion for `run` and `add` commands
- Flag completion for all commands

## Error Handling

Each command returns appropriate exit codes:
- `0`: Success
- `1`: Error occurred

Commands provide detailed error messages and support verbose output for troubleshooting configuration and execution issues.