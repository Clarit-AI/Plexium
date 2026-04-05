---
name: Hooks Automation
version: 2.0.0
description: Automated coordination and formatting using standard Git hooks and Claude Code's native tool hooks (PreToolUse/PostToolUse).
tags: [hooks, automation, git, claude-code]
---

# Hooks Automation

Automate development workflows, enforce quality, and manage state using standard Git hooks and Claude Code's native hooks configuration.

## 1. Git Hooks

Git hooks are standard scripts executed by Git before or after events like commit, push, and merge.

### Pre-commit Hook Example
Create a script at `.git/hooks/pre-commit` (make sure to `chmod +x`):
```bash
#!/bin/bash
# Run linting and tests before allowing a commit

echo "Running pre-commit checks..."

# Run linter
npm run lint
if [ $? -ne 0 ]; then
  echo "Linting failed. Please fix errors before committing."
  exit 1
fi

# Run tests
npm test
if [ $? -ne 0 ]; then
  echo "Tests failed. Please fix tests before committing."
  exit 1
fi

echo "All checks passed!"
exit 0
```

*Note: For easier management across teams, use tools like Husky or Lefthook instead of manual `.git/hooks` scripts.*

## 2. Claude Code Native Hooks

Claude Code supports powerful event-driven hooks defined in `.claude/settings.json` or `~/.claude/hooks.json`. These hooks can run bash commands or LLM prompts before/after tools are used.

### Post-Edit Auto-Formatting
Automatically format files after Claude edits them:
```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write|Edit|NotebookEdit",
        "hooks": [
          {
            "type": "command",
            "command": "npx prettier --write \"\\${tool.params.file_path}\"",
            "continueOnError": true
          }
        ]
      }
    ]
  }
}
```

### Pre-Bash Command Validation
Prevent Claude from running dangerous shell commands:
```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "prompt",
            "prompt": "Command: $TOOL_INPUT.command. Check for destructive operations (rm -rf /, dd, mkfs). Return 'approve' or 'deny' with explanation.",
            "timeout": 15
          }
        ]
      }
    ]
  }
}
```

## Best Practices
1. **Keep Hooks Fast:** Pre-commit and PreToolUse hooks should be nearly instantaneous to avoid disrupting the developer workflow.
2. **Fail Gracefully:** Use `continueOnError` for non-critical post-operation hooks (like auto-formatting) so the agent loop isn't broken if the hook fails.
3. **Standardize:** Check Git hook configurations (like `.lefthook.yml`) into version control so the entire team uses the same automation.
