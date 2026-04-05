# Hook Execution

Lefthook's hook execution engine provides powerful capabilities for running Git hooks with parallel execution, file filtering, and comprehensive error handling.

## Execution Modes

### Parallel Execution

Execute multiple commands concurrently to improve performance:

```yaml { .api }
pre-commit:
  parallel: true  # Enable parallel execution
  commands:
    lint:
      run: eslint {staged_files}
    test:
      run: npm test
    format:
      run: prettier --write {staged_files}
```

### Sequential Execution

Execute commands one after another (default behavior):

```yaml { .api }
pre-commit:
  parallel: false  # Sequential execution (default)
  commands:
    build:
      run: npm run build
    test:
      run: npm test  # Runs after build completes
```

### Piped Execution

Chain command outputs together:

```yaml { .api }
pre-commit:
  piped: true  # Enable piped execution
  commands:
    generate:
      run: generate-data
    process:
      run: process-data  # Receives output from generate-data
```

## File Processing

### Staged Files Processing

Process only Git staged files (default for pre-commit):

```yaml { .api }
pre-commit:
  commands:
    lint:
      glob: "*.js"
      run: eslint {staged_files}  # Only staged JS files
```

### All Files Processing

Process all repository files:

```bash { .api }
# Via command line
lefthook run pre-commit --all-files

# Via configuration
pre-commit:
  commands:
    full-lint:
      run: eslint {all_files}
```

### Custom File Selection

Use Git commands to select specific files:

```yaml { .api }
pre-push:
  commands:
    test-changed:
      files: git diff --name-only HEAD @{push}
      glob: "*.js"
      run: jest {files}
```

## File Filtering

### Glob Pattern Matching

```yaml { .api }
# Single file type
glob: "*.js"

# Multiple file types  
glob: "*.{js,ts,jsx,tsx}"

# Directory-specific
glob: "src/**/*.js"

# Complex patterns
glob: "{src,test}/**/*.{js,ts}"
```

### Exclusion Patterns

```yaml { .api }
commands:
  lint:
    glob: "*.js"
    exclude: "(test|spec)\.js$"  # Regex pattern to exclude test files
    run: eslint {files}
```

### File Template Variables

```yaml { .api }
# Available file templates
{files}        # Files matched by glob after filtering
{staged_files} # All staged files (unfiltered)
{push_files}   # Files being pushed
{all_files}    # All repository files
```

## Execution Control

### Priority-based Execution

Control execution order with priority values:

```yaml { .api }
pre-commit:
  commands:
    format:
      priority: 1      # Runs first
      run: prettier --write {staged_files}
      stage_fixed: true
      
    lint:
      priority: 2      # Runs second  
      run: eslint {staged_files}
      
    test:
      priority: 3      # Runs last
      run: npm test
```

### Conditional Execution

#### Git State Conditions

```yaml { .api }
# Skip during merge conflicts
skip: merge

# Skip during rebase
skip: rebase

# Only run during merge
only: merge

# Multiple conditions
skip: [merge, rebase]
only: [merge, rebase]
```

#### Tag-based Filtering

```yaml { .api }
pre-commit:
  exclude_tags: [slow]  # Exclude commands tagged as 'slow'
  
  commands:
    quick-lint:
      tags: [fast, required]
      run: quick-linter
      
    slow-test:
      tags: [slow]
      run: comprehensive-test-suite
```

## Interactive Execution

### Interactive Commands

Allow commands to interact with user input:

```yaml { .api }
pre-push:
  commands:
    confirm-deploy:
      interactive: true
      run: ./scripts/confirm-deployment.sh
```

### Standard Input Handling

Provide stdin to commands:

```yaml { .api }
commit-msg:
  commands:
    validate-message:
      use_stdin: true
      run: ./scripts/validate-commit-msg.sh
```

## Error Handling and Recovery

### Stage Fixed Files

Automatically stage files modified by commands:

```yaml { .api }
pre-commit:
  commands:
    format:
      run: prettier --write {staged_files}
      stage_fixed: true  # Stage files after formatting
      
    lint:
      run: eslint --fix {staged_files}  
      stage_fixed: true  # Stage files after linting
```

### Custom Failure Messages

Provide helpful error messages:

```yaml { .api }
pre-commit:
  commands:
    dependencies:
      run: npm ci
      fail_text: "Dependencies failed to install. Run 'npm install' to fix."
      
    test:
      run: npm test
      fail_text: "Tests failed. Fix failing tests before committing."
```

### Execution Strategies

```yaml { .api }
pre-commit:
  follow: true  # Continue execution even if commands fail
  
  commands:
    lint:
      run: eslint {staged_files}
    test:
      run: npm test    # Runs even if lint fails when follow: true
```

## Environment and Context

### Environment Variables

```yaml { .api }
pre-commit:
  commands:
    test:
      env:
        NODE_ENV: test
        CI: true
        DATABASE_URL: test-db-url
      run: npm test
```

### Working Directory

```yaml { .api }
pre-commit:
  commands:
    frontend-lint:
      root: ./frontend  # Change to frontend directory
      run: npm run lint
      
    backend-test:
      root: ./backend   # Change to backend directory  
      run: go test ./...
```

## Script Execution

### Script Runners

Execute custom scripts with specific interpreters:

```yaml { .api }
pre-commit:
  scripts:
    bash-script:
      runner: bash
      # Executes ./lefthook/pre-commit/bash-script
      
    node-script:
      runner: node
      env:
        NODE_ENV: development
      # Executes ./lefthook/pre-commit/node-script
      
    python-script:
      runner: python
      # Executes ./lefthook/pre-commit/python-script
```

### Script Directory Structure

```
.lefthook/
├── pre-commit/
│   ├── bash-script          # Bash script
│   ├── node-script.js       # Node.js script  
│   └── python-script.py     # Python script
├── pre-push/
│   └── deployment-check.sh
└── commit-msg/
    └── validate-format.rb
```

## Performance Optimization

### Parallel Processing

```yaml { .api }
pre-commit:
  parallel: true  # Enable parallel execution
  commands:
    # These run concurrently
    lint: { run: "eslint {staged_files}" }
    test: { run: "npm test" }
    format: { run: "prettier --write {staged_files}" }
```

### File Filtering Optimization

```yaml { .api }
pre-commit:
  commands:
    # Only process relevant files
    js-lint:
      glob: "*.{js,ts}"          # Pre-filter files
      run: eslint {staged_files}
      
    # Use exclude for better performance
    all-lint:
      glob: "*"
      exclude: "node_modules|dist|build"  # Exclude large directories
      run: generic-linter {files}
```

## Debugging and Monitoring

### Verbose Output

```bash { .api }
# Enable verbose logging
lefthook run pre-commit --verbose

# Show execution details, timing, and file processing
```

### Execution Summary

Lefthook provides execution summaries showing:
- Commands executed
- Execution time
- Files processed  
- Success/failure status
- Skip reasons

### Log Output Control

```yaml { .api }
# Control output verbosity
output:
  - meta     # Show metadata (default)
  - success  # Show success messages
  - failure  # Show failure messages (default)
  - summary  # Show execution summary (default)
  - skips    # Show skipped commands
```