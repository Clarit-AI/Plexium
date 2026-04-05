---
name: Codex CLI Hooks
description: This skill should be used when the user asks to "create a Codex hook", "configure Codex CLI hooks", "add a PreToolUse hook for Codex", "block commands in Codex", "create a stop hook", "set up SessionStart hooks", "write Codex hooks.json", or mentions Codex CLI hook events (SessionStart, PreToolUse, PostToolUse, UserPromptSubmit, Stop). Covers the Codex CLI hooks extensibility framework, configuration, event types, input/output wire format, and common patterns.
---

# Codex CLI Hooks

Hooks are an extensibility framework for OpenAI's Codex CLI that inject custom scripts into the agentic loop. They enable command interception, prompt filtering, session customization, and turn continuation.

## Prerequisites

Hooks require enabling the feature flag in `config.toml`:

```toml
[features]
codex_hooks = true
```

Hooks are currently disabled on Windows.

## Hook Discovery

Codex discovers `hooks.json` next to active config layers. The two primary locations:

- `~/.codex/hooks.json` (global)
- `<repo>/.codex/hooks.json` (repo-local)

Multiple `hooks.json` files are merged — higher-precedence layers do not replace lower-precedence hooks.

## Config Structure

Hooks are organized in three levels: event type, matcher group, and handlers.

```json
{
  "hooks": {
    "<EventName>": [
      {
        "matcher": "<regex>",
        "hooks": [
          {
            "type": "command",
            "command": "<shell command>",
            "timeout": 600,
            "statusMessage": "optional UI message"
          }
        ]
      }
    ]
  }
}
```

Key notes:
- `timeout` defaults to 600 seconds. `timeoutSec` is an accepted alias.
- `statusMessage` is optional.
- Commands run with session `cwd` as working directory.
- For repo-local hooks, resolve paths from git root: `"$(git rev-parse --show-toplevel)/.codex/hooks/..."`

## Supported Events

| Event | Matcher Filters | Purpose |
|-------|----------------|---------|
| `SessionStart` | `startup` or `resume` | Load context at session start |
| `PreToolUse` | tool name (currently `Bash` only) | Intercept/block commands before execution |
| `PostToolUse` | tool name (currently `Bash` only) | Review command output after execution |
| `UserPromptSubmit` | not supported | Inspect/modify user prompts |
| `Stop` | not supported | Continue or inspect turns at stop |

Matcher is a regex string. Use `"*"`, `""`, or omit to match all. Multiple matching hooks run concurrently.

## Common I/O Fields

All hooks receive JSON on stdin with these shared fields:

| Field | Type | Meaning |
|-------|------|---------|
| `session_id` | `string` | Session/thread ID |
| `transcript_path` | `string \| null` | Path to transcript file |
| `cwd` | `string` | Working directory |
| `hook_event_name` | `string` | Event name |
| `model` | `string` | Active model slug |

Common JSON output fields (events vary):

```json
{
  "continue": true,
  "stopReason": "optional",
  "systemMessage": "optional warning text",
  "suppressOutput": false
}
```

Exit `0` with no output = success, Codex continues.

## Quick Reference by Event

### SessionStart
- **Input extra**: `source` (`startup` or `resume`)
- **Plain text stdout**: added as developer context
- **JSON stdout**: `additionalContext` in `hookSpecificOutput`

### PreToolUse
- **Input extra**: `turn_id`, `tool_name`, `tool_use_id`, `tool_input.command`
- **Block command**: return `{"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": "..."}}` or `{"decision": "block", "reason": "..."}`
- **Also**: exit code `2` + reason on stderr

### PostToolUse
- **Input extra**: `turn_id`, `tool_name`, `tool_use_id`, `tool_input.command`, `tool_response`
- **Block**: `{"decision": "block", "reason": "..."}` replaces tool result with feedback (does NOT undo the command)
- **Stop processing**: return `continue: false`

### UserPromptSubmit
- **Input extra**: `turn_id`, `prompt`
- **Plain text stdout**: added as developer context
- **Block prompt**: `{"decision": "block", "reason": "..."}`

### Stop
- **Input extra**: `turn_id`, `stop_hook_active`, `last_assistant_message`
- **JSON stdout required** (plain text is invalid)
- **Continue turn**: `{"decision": "block", "reason": "..."}` — creates continuation prompt from `reason`
- `continue: false` takes precedence over other Stop hooks' continuation decisions

## Important Limitations

- `PreToolUse` and `PostToolUse` currently only intercept `Bash` tool calls. The model can bypass by writing scripts to disk.
- `PostToolUse` cannot undo side effects from commands that already ran.
- Several fields (`permissionDecision: "allow"/"ask"`, `updatedInput`, `suppressOutput`) are parsed but not supported — they fail open.
- `Stop` requires JSON output; plain text is invalid.

## Additional Resources

### Reference Files

- **`references/hook-events.md`** — Complete input/output schemas for each event
- **`references/wire-format.md`** — Full wire format details and edge cases
- **`references/patterns.md`** — Common hook patterns (security, logging, auto-continue)

### Examples

- **`examples/hooks.json`** — Complete configuration with all event types
- **`examples/session_start.py`** — SessionStart hook loading workspace notes
- **`examples/pre_tool_use_policy.py`** — PreToolUse blocking destructive commands
- **`examples/stop_continue.py`** — Stop hook for auto-continuation

### Scripts

- **`scripts/validate-hooks.sh`** — Validate hooks.json structure
- **`scripts/test-hook.sh`** — Test a hook script with sample input

### External Reference

Full generated schemas: [Codex GitHub repository](https://github.com/openai/codex/tree/main/codex-rs/hooks/schema/generated)
