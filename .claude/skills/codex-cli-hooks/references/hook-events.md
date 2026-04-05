# Codex CLI Hook Events — Detailed Reference

## SessionStart

**Matcher**: Applied to `source` field. Values: `startup`, `resume`.

### Input Fields

| Field | Type | Meaning |
|-------|------|---------|
| `session_id` | `string` | Current session ID |
| `transcript_path` | `string \| null` | Transcript file path |
| `cwd` | `string` | Working directory |
| `hook_event_name` | `string` | `SessionStart` |
| `model` | `string` | Active model slug |
| `source` | `string` | `startup` or `resume` |

### Output

**Plain text stdout**: Added as extra developer context.

**JSON stdout** — Common output fields plus:

```json
{
  "hookSpecificOutput": {
    "hookEventName": "SessionStart",
    "additionalContext": "Load the workspace conventions before editing."
  }
}
```

`additionalContext` is added as extra developer context.

### Use Cases

- Load workspace-specific conventions or notes
- Inject project context on session start
- Resume-aware behavior (different actions for startup vs resume)

---

## PreToolUse

**Matcher**: Applied to `tool_name`. Currently always `Bash`.

### Input Fields

| Field | Type | Meaning |
|-------|------|---------|
| `session_id` | `string` | Current session ID |
| `transcript_path` | `string \| null` | Transcript file path |
| `cwd` | `string` | Working directory |
| `hook_event_name` | `string` | `PreToolUse` |
| `model` | `string` | Active model slug |
| `turn_id` | `string` | Active Codex turn ID |
| `tool_name` | `string` | Currently always `Bash` |
| `tool_use_id` | `string` | Tool-call ID for this invocation |
| `tool_input.command` | `string` | Shell command about to run |

### Output

**Plain text stdout**: Ignored.

**JSON stdout** — `systemMessage` supported. Block with:

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "deny",
    "permissionDecisionReason": "Destructive command blocked by hook."
  }
}
```

Legacy block shape also accepted:

```json
{
  "decision": "block",
  "reason": "Destructive command blocked by hook."
}
```

Exit code `2` with reason on `stderr` also blocks.

### Not Yet Supported (fail open)

`permissionDecision: "allow"` and `"ask"`, `decision: "approve"`, `updatedInput`, `additionalContext`, `continue: false`, `stopReason`, `suppressOutput`.

### Limitations

Only intercepts `Bash` tool calls. The model can bypass by writing a script to disk and running it. Treat as a guardrail, not complete enforcement.

---

## PostToolUse

**Matcher**: Applied to `tool_name`. Currently always `Bash`.

### Input Fields

| Field | Type | Meaning |
|-------|------|---------|
| `session_id` | `string` | Current session ID |
| `transcript_path` | `string \| null` | Transcript file path |
| `cwd` | `string` | Working directory |
| `hook_event_name` | `string` | `PostToolUse` |
| `model` | `string` | Active model slug |
| `turn_id` | `string` | Active Codex turn ID |
| `tool_name` | `string` | Currently always `Bash` |
| `tool_use_id` | `string` | Tool-call ID for this invocation |
| `tool_input.command` | `string` | Shell command that just ran |
| `tool_response` | `JSON value` | Bash output payload (usually JSON string) |

### Output

**Plain text stdout**: Ignored.

**JSON stdout** — `systemMessage` supported. Block shape:

```json
{
  "decision": "block",
  "reason": "The Bash output needs review before continuing.",
  "hookSpecificOutput": {
    "hookEventName": "PostToolUse",
    "additionalContext": "The command updated generated files."
  }
}
```

`additionalContext` is added as developer context.

`decision: "block"` does NOT undo the command. It replaces the tool result with feedback and continues the model from the hook message.

Exit code `2` with reason on `stderr` also works.

Return `continue: false` to stop normal processing of the original tool result.

### Not Yet Supported (fail open)

`updatedMCPToolOutput`, `suppressOutput`.

### Important

Fires even for commands that failed. Cannot undo side effects.

---

## UserPromptSubmit

**Matcher**: Not currently used. Any configured matcher is ignored.

### Input Fields

| Field | Type | Meaning |
|-------|------|---------|
| `session_id` | `string` | Current session ID |
| `transcript_path` | `string \| null` | Transcript file path |
| `cwd` | `string` | Working directory |
| `hook_event_name` | `string` | `UserPromptSubmit` |
| `model` | `string` | Active model slug |
| `turn_id` | `string` | Active Codex turn ID |
| `prompt` | `string` | User prompt about to be sent |

### Output

**Plain text stdout**: Added as extra developer context.

**JSON stdout** — Common output fields plus:

```json
{
  "hookSpecificOutput": {
    "hookEventName": "UserPromptSubmit",
    "additionalContext": "Ask for a clearer reproduction before editing files."
  }
}
```

Block the prompt:

```json
{
  "decision": "block",
  "reason": "Ask for confirmation before doing that."
}
```

Exit code `2` with reason on `stderr` also blocks.

---

## Stop

**Matcher**: Not currently used. Any configured matcher is ignored.

### Input Fields

| Field | Type | Meaning |
|-------|------|---------|
| `session_id` | `string` | Current session ID |
| `transcript_path` | `string \| null` | Transcript file path |
| `cwd` | `string` | Working directory |
| `hook_event_name` | `string` | `Stop` |
| `model` | `string` | Active model slug |
| `turn_id` | `string` | Active Codex turn ID |
| `stop_hook_active` | `boolean` | Whether turn was already continued by a Stop hook |
| `last_assistant_message` | `string \| null` | Latest assistant message text |

### Output

**Plain text stdout**: Invalid for this event. JSON required.

**JSON stdout** — Common output fields. To continue Codex:

```json
{
  "decision": "block",
  "reason": "Run one more pass over the failing tests."
}
```

`decision: "block"` does not reject — it creates a new continuation prompt using `reason` as the prompt text.

Exit code `2` with continuation reason on `stderr` also works.

**Precedence**: If any matching Stop hook returns `continue: false`, it takes precedence over continuation decisions from other matching Stop hooks.

### Guard Against Infinite Loops

Check `stop_hook_active` to prevent infinite continuation loops. If true, a Stop hook already continued this turn — consider allowing it to stop.
