# Codex CLI Hooks — Wire Format Details

## Common Input (All Events)

Every command hook receives one JSON object on `stdin`.

```json
{
  "session_id": "abc-123",
  "transcript_path": "/path/to/transcript.jsonl",
  "cwd": "/Users/dev/project",
  "hook_event_name": "PreToolUse",
  "model": "o3"
}
```

Turn-scoped events (`PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `Stop`) also include `turn_id`.

## Common Output Fields

### SessionStart, UserPromptSubmit, Stop

```json
{
  "continue": true,
  "stopReason": "optional string",
  "systemMessage": "optional warning shown in UI",
  "suppressOutput": false
}
```

| Field | Effect |
|-------|--------|
| `continue` | `false` = mark hook run as stopped |
| `stopReason` | Recorded as stop reason |
| `systemMessage` | Warning in UI/event stream |
| `suppressOutput` | Parsed but not yet implemented |

Exit `0` with no output = success, Codex continues.

### PreToolUse Output

Supports `systemMessage` only from common fields. `continue`, `stopReason`, `suppressOutput` not supported.

### PostToolUse Output

Supports `systemMessage`, `continue: false`, `stopReason`. `suppressOutput` parsed but not implemented.

## Event-Specific Input Schemas

### SessionStart Input

```json
{
  "session_id": "string",
  "transcript_path": "string | null",
  "cwd": "string",
  "hook_event_name": "SessionStart",
  "model": "string",
  "source": "startup | resume"
}
```

### PreToolUse Input

```json
{
  "session_id": "string",
  "transcript_path": "string | null",
  "cwd": "string",
  "hook_event_name": "PreToolUse",
  "model": "string",
  "turn_id": "string",
  "tool_name": "Bash",
  "tool_use_id": "string",
  "tool_input": {
    "command": "string"
  }
}
```

### PostToolUse Input

```json
{
  "session_id": "string",
  "transcript_path": "string | null",
  "cwd": "string",
  "hook_event_name": "PostToolUse",
  "model": "string",
  "turn_id": "string",
  "tool_name": "Bash",
  "tool_use_id": "string",
  "tool_input": {
    "command": "string"
  },
  "tool_response": "JSON value (usually string)"
}
```

### UserPromptSubmit Input

```json
{
  "session_id": "string",
  "transcript_path": "string | null",
  "cwd": "string",
  "hook_event_name": "UserPromptSubmit",
  "model": "string",
  "turn_id": "string",
  "prompt": "string"
}
```

### Stop Input

```json
{
  "session_id": "string",
  "transcript_path": "string | null",
  "cwd": "string",
  "hook_event_name": "Stop",
  "model": "string",
  "turn_id": "string",
  "stop_hook_active": false,
  "last_assistant_message": "string | null"
}
```

## Event-Specific Output Schemas

### SessionStart Output

```json
{
  "hookSpecificOutput": {
    "hookEventName": "SessionStart",
    "additionalContext": "string"
  }
}
```

### PreToolUse Output (block)

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "deny",
    "permissionDecisionReason": "string"
  }
}
```

Legacy shape (also accepted):

```json
{
  "decision": "block",
  "reason": "string"
}
```

### PostToolUse Output (block)

```json
{
  "decision": "block",
  "reason": "string",
  "hookSpecificOutput": {
    "hookEventName": "PostToolUse",
    "additionalContext": "string"
  }
}
```

### UserPromptSubmit Output

```json
{
  "hookSpecificOutput": {
    "hookEventName": "UserPromptSubmit",
    "additionalContext": "string"
  }
}
```

Block:

```json
{
  "decision": "block",
  "reason": "string"
}
```

### Stop Output (continue)

```json
{
  "decision": "block",
  "reason": "string used as continuation prompt"
}
```

## Exit Codes

| Exit Code | Meaning |
|-----------|---------|
| `0` | Success — process output |
| `2` | Block/feedback — read reason from `stderr` |

## External Schema Reference

Generated JSON schemas are available in the Codex repository:
`https://github.com/openai/codex/tree/main/codex-rs/hooks/schema/generated`
