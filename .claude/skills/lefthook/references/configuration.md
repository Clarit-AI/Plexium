# Configuration System

Lefthook uses YAML configuration files to define Git hooks and their execution parameters. The configuration system provides comprehensive control over hook execution, file filtering, and command management.

## Configuration Files

Lefthook looks for configuration files in the following order:
1. `.lefthook.yml` (recommended)
2. `lefthook.yml`
3. `.lefthook.yaml`
4. `lefthook.yaml`

## Global Configuration Options

```yaml
# Global settings
assert_lefthook_installed: boolean  # Require lefthook to be installed
skip_lfs: boolean                   # Skip Git LFS files  
colors: boolean|object              # Enable colored output or custom color settings
no_tty: boolean                     # Disable TTY output
min_version: string                 # Specify minimum lefthook version
lefthook: string                    # Lefthook executable path or command
source_dir: string                  # Directory for script files (default: .lefthook/)
source_dir_local: string            # Directory for local script files (default: .lefthook-local/)
rc: string                          # Provide an rc file - a simple sh script
skip_output: boolean|array          # Skip output of some steps
extends: [string]                   # Specify files to extend config with

# Output control
output:
  - meta        # Show metadata (default)
  - success     # Show success messages  
  - failure     # Show failure messages
  - summary     # Show execution summary
  - skips       # Show skipped commands

# CI/CD environments
ci:
  - GITHUB_ACTIONS  # GitHub Actions
  - GITLAB_CI       # GitLab CI
  - BUILDKITE       # Buildkite

# Remote configurations
remotes: [Remote]                   # Remote configurations for shared configs

# Custom templates
templates: { [string]: string }     # Custom templates for replacements
```

## Hook Configuration

Each Git hook can be configured with specific execution parameters:

```yaml
# Hook definition structure
<hook_name>:
  parallel: boolean                 # Execute commands in parallel (default: false)
  piped: boolean                   # Pipe commands together
  follow: boolean                  # Continue execution on command failure
  exclude_tags: [string]           # Tags to exclude from execution
  skip: [merge|rebase] | boolean   # Skip conditions
  only: [merge|rebase] | boolean   # Only run conditions
  files: string                    # Global file selection for hook
  commands: { [string]: Command }  # Command definitions
  scripts: { [string]: Script }   # Script definitions
  jobs: [Job]                      # Job definitions (alternative to commands/scripts)
```

### Hook Names

Supported Git hook names:
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

## Command Configuration

Commands define executable operations within hooks:

```yaml
Command:
  # Execution
  run: string                      # Command to execute (required)
  root: string                     # Working directory for execution
  env: { [string]: string }        # Environment variables
  
  # File filtering
  glob: string | [string]          # File matching glob patterns
  files: string                    # Git command to generate file list
  exclude: string | [string]       # Files to exclude (regex patterns)
  file_types: [string]             # Filter by file extensions
  
  # Conditional execution
  skip: [merge|rebase] | boolean   # Skip conditions
  only: [merge|rebase] | boolean   # Only run conditions
  tags: [string]                   # Command tags for filtering
  
  # Behavior options
  stage_fixed: boolean             # Stage files after command execution
  interactive: boolean             # Run in interactive mode
  use_stdin: boolean               # Provide stdin to command
  priority: integer                # Execution priority (0-100)
  
  # Output control
  fail_text: string                # Custom failure message
```

### Command Examples

```yaml
pre-commit:
  commands:
    # Lint JavaScript files
    eslint:
      glob: "*.{js,ts,jsx,tsx}"
      run: npx eslint --fix {staged_files}
      stage_fixed: true
      
    # Run tests
    test:
      run: npm test
      skip: merge
      
    # Format code
    prettier:
      glob: "*.{js,ts,json,md}"  
      run: npx prettier --write {staged_files}
      stage_fixed: true
      priority: 10
```

## Script Configuration

Scripts allow execution of custom script files:

```yaml
Script:
  # Script execution
  runner: string                   # Script interpreter (bash, node, python, etc.)
  env: { [string]: string }        # Environment variables
  
  # Conditional execution  
  skip: [merge|rebase] | boolean   # Skip conditions
  only: [merge|rebase] | boolean   # Only run conditions
  tags: [string]                   # Script tags for filtering
  
  # Behavior options
  stage_fixed: boolean             # Stage files after script execution
  interactive: boolean             # Run in interactive mode
  use_stdin: boolean               # Provide stdin to script
  priority: integer                # Execution priority (0-100)
  
  # Output control
  fail_text: string                # Custom failure message
```

### Script Examples

```yaml
pre-commit:
  scripts:
    # Bash script
    check-secrets:
      runner: bash
      
    # Node.js script
    custom-linter:
      runner: node
      env:
        NODE_ENV: development
        
    # Python script
    code-analysis:
      runner: python
      skip: rebase
```

## Job Configuration

Jobs provide a unified interface for both commands and scripts within hooks:

```yaml
Job:
  # Identification
  name: string                     # Optional job name for display
  
  # Execution (exactly one required)
  run: string                      # Command to execute
  script: string                   # Script file to execute
  
  # Script execution
  runner: string                   # Script interpreter when using script
  
  # File filtering
  glob: string | [string]          # File matching glob patterns
  files: string                    # Git command to generate file list
  exclude: string | [string]       # Files to exclude (regex patterns)
  file_types: [string]             # Filter by file extensions
  root: string                     # Working directory for execution
  
  # Conditional execution
  skip: [merge|rebase] | boolean   # Skip conditions
  only: [merge|rebase] | boolean   # Only run conditions
  tags: [string]                   # Job tags for filtering
  
  # Environment and behavior
  env: { [string]: string }        # Environment variables
  stage_fixed: boolean             # Stage files after job execution
  interactive: boolean             # Run in interactive mode
  use_stdin: boolean               # Provide stdin to job
  
  # Output control
  fail_text: string                # Custom failure message
  
  # Group execution (alternative to individual job)
  group: Group                     # Define a group of sub-jobs
```

### Group Configuration

```yaml
Group:
  root: string                     # Working directory for group
  parallel: boolean                # Execute group jobs in parallel
  piped: boolean                   # Pipe group jobs together
  jobs: [Job]                      # Array of jobs in the group
```

### Job Examples

```yaml
pre-commit:
  jobs:
    # Command job
    - name: "lint"
      run: npx eslint --fix {staged_files}
      glob: "*.{js,ts}"
      stage_fixed: true
      
    # Script job  
    - name: "custom check"
      script: "check-commit.sh"
      runner: bash
      
    # Group job
    - name: "test suite"
      group:
        parallel: true
        jobs:
          - run: npm test
          - run: npm run type-check
```

## Template Variables

Lefthook provides template variables for dynamic file and context substitution:

```yaml
# File-based templates
{files}          # Files matched by glob pattern
{staged_files}   # Git staged files
{push_files}     # Files being pushed
{all_files}      # All repository files

# Context templates  
{0}              # All command-line arguments
{1}              # First positional argument
{2}              # Second positional argument
{lefthook_job_name}  # Name of the current job being executed
```

### Template Usage Examples

```yaml
pre-commit:
  commands:
    lint:
      glob: "*.js"
      run: eslint {files}
      
    format:
      run: prettier --write {staged_files}
      
    test-changed:
      files: git diff --name-only HEAD~1
      run: jest {files}
```

## File Filtering

### Glob Patterns

```yaml
# Single pattern
glob: "*.js"

# Multiple extensions
glob: "*.{js,ts,jsx,tsx}"

# Directory patterns  
glob: "src/**/*.js"

# Exclude pattern (using exclude field)
glob: "*.js"
exclude: "(test|spec)\.js$"
```

### File Sources

```yaml
# Use staged files (default for most hooks)
# No additional configuration needed

# Use specific git command
files: git diff --name-only HEAD~1

# Use all files
# Set via --all-files flag or in command
```

## Conditional Execution

### Skip Conditions

```yaml
# Skip during merge
skip: merge

# Skip during rebase  
skip: rebase

# Skip during both
skip: [merge, rebase]

# Always skip
skip: true
```

### Only Conditions

```yaml
# Only run during merge
only: merge

# Only run during rebase
only: rebase  

# Only run during merge or rebase
only: [merge, rebase]
```

## Advanced Configuration

### Remote Configuration

```yaml
# Extend from remote configuration
extend: https://raw.githubusercontent.com/company/hooks/main/.lefthook.yml

# Local overrides apply after remote config is loaded
pre-commit:
  commands:
    local-lint:
      run: custom-linter
```

### Tag-based Filtering

```yaml
pre-commit:
  # Exclude specific tags
  exclude_tags: [slow, optional]
  
  commands:
    quick-lint:
      tags: [fast, required]
      run: quick-linter
      
    slow-test:
      tags: [slow, comprehensive]  
      run: full-test-suite
```

### Docker Integration

```yaml
pre-commit:
  commands:
    docker-lint:
      run: docker run --rm -v $(pwd):/app linter:latest
      
    compose-test:
      run: docker-compose run --rm test-runner
```

## Remote Configuration

Remote configurations allow sharing lefthook configurations across repositories:

```yaml
Remote:
  git_url: string            # Git repository URL (required)
  ref: string                # Git reference (branch, tag, commit)
  configs: [string]          # Array of config file paths (default: ["lefthook.yml"])
  refetch: boolean           # Always refetch the remote
  refetch_frequency: string  # Frequency for refetching (e.g., "24h")
```

### Remote Configuration Examples

```yaml
# Single remote configuration
extends: https://raw.githubusercontent.com/company/hooks/main/.lefthook.yml

# Multiple remote configurations
remotes:
  - git_url: https://github.com/company/shared-hooks
    ref: main
    configs: [".lefthook.yml"]
    refetch_frequency: "24h"
  - git_url: https://github.com/company/security-hooks  
    ref: v1.2.0
    configs:
      - security/.lefthook.yml
      - compliance/.lefthook.yml

# Local overrides apply after remote config is loaded
pre-commit:
  commands:
    local-lint:
      run: custom-linter
```

## Configuration Validation

Use `lefthook validate` to check configuration syntax and structure:

```bash
# Validate current configuration
lefthook validate

# Common validation errors:
# - Invalid YAML syntax
# - Unknown hook names  
# - Missing required fields
# - Invalid glob patterns
# - Circular remote configuration references
```
