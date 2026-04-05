# Codex CLI Hooks — Common Patterns

## Security: Block Destructive Commands

Prevent dangerous shell commands from executing via `PreToolUse`.

```python
#!/usr/bin/env python3
import sys, json

DESTRUCTIVE_PATTERNS = [
    r"\brm\s+-rf\s+/",
    r"\bgit\s+push\s+--force",
    r"\bdd\s+if=",
    r"\bformat\s+[A-Z]:",
    r">\s*/dev/sd",
    r"\bchmod\s+-R\s+777\s+/",
]

def main():
    data = json.load(sys.stdin)
    command = data.get("tool_input", {}).get("command", "")

    import re
    for pattern in DESTRUCTIVE_PATTERNS:
        if re.search(pattern, command):
            print(json.dumps({
                "decision": "block",
                "reason": f"Blocked: command matches destructive pattern '{pattern}'"
            }))
            sys.exit(0)

    # Allow — exit 0 with no output
    sys.exit(0)

if __name__ == "__main__":
    main()
```

## Security: Block API Keys in Prompts

Scan user prompts for accidentally pasted secrets via `UserPromptSubmit`.

```python
#!/usr/bin/env python3
import sys, json, re

SECRET_PATTERNS = [
    r"sk-[a-zA-Z0-9]{20,}",        # OpenAI keys
    r"ghp_[a-zA-Z0-9]{36}",        # GitHub PATs
    r"AKIA[0-9A-Z]{16}",           # AWS keys
    r"xox[bpas]-[a-zA-Z0-9-]+",    # Slack tokens
]

def main():
    data = json.load(sys.stdin)
    prompt = data.get("prompt", "")

    for pattern in SECRET_PATTERNS:
        if re.search(pattern, prompt):
            print(json.dumps({
                "decision": "block",
                "reason": "Prompt contains what appears to be an API key or secret. Please remove it before continuing."
            }))
            sys.exit(0)

    sys.exit(0)

if __name__ == "__main__":
    main()
```

## Logging: Conversation Analytics

Send conversation data to a logging endpoint on each turn stop.

```python
#!/usr/bin/env python3
import sys, json

def main():
    data = json.load(sys.stdin)

    # Log session info (send to your analytics service)
    log_entry = {
        "session_id": data.get("session_id"),
        "model": data.get("model"),
        "cwd": data.get("cwd"),
        "event": data.get("hook_event_name"),
    }

    # Example: write to local log file
    # In production, POST to your analytics endpoint
    with open("/tmp/codex-analytics.jsonl", "a") as f:
        f.write(json.dumps(log_entry) + "\n")

    # Continue normally
    sys.exit(0)

if __name__ == "__main__":
    main()
```

## Auto-Continue: Enforce Test Pass

Keep Codex running until tests pass via `Stop`.

```python
#!/usr/bin/env python3
import sys, json, subprocess

def main():
    data = json.load(sys.stdin)

    # Prevent infinite loops
    if data.get("stop_hook_active", False):
        sys.exit(0)

    last_message = data.get("last_assistant_message") or ""
    cwd = data.get("cwd", ".")

    # Only continue if the assistant was working on tests
    if "test" not in last_message.lower():
        sys.exit(0)

    # Run tests
    result = subprocess.run(
        ["npm", "test"],
        cwd=cwd,
        capture_output=True,
        text=True,
        timeout=60
    )

    if result.returncode != 0:
        # Tests failed — continue to fix
        print(json.dumps({
            "decision": "block",
            "reason": f"Tests are still failing. Output:\n{result.stdout[-500:]}\nPlease fix the failing tests."
        }))
    # else: tests pass, allow stop
    sys.exit(0)

if __name__ == "__main__":
    main()
```

## Session Context: Load Workspace Notes

Inject project-specific context when a session starts.

```python
#!/usr/bin/env python3
import sys, json, os

def main():
    data = json.load(sys.stdin)
    cwd = data.get("cwd", ".")
    source = data.get("source", "startup")

    context_parts = []

    # Load workspace conventions if present
    conventions_path = os.path.join(cwd, ".codex", "CONVENTIONS.md")
    if os.path.exists(conventions_path):
        with open(conventions_path) as f:
            context_parts.append(f.read())

    # Load session notes from previous session
    if source == "resume":
        notes_path = os.path.join(cwd, ".codex", "session-notes.md")
        if os.path.exists(notes_path):
            with open(notes_path) as f:
                context_parts.append("## Previous Session Notes\n" + f.read())

    if context_parts:
        print(json.dumps({
            "hookSpecificOutput": {
                "hookEventName": "SessionStart",
                "additionalContext": "\n\n".join(context_parts)
            }
        }))

    sys.exit(0)

if __name__ == "__main__":
    main()
```

## Post-Command Review: Check Generated Files

Review Bash output and flag when generated files are updated.

```python
#!/usr/bin/env python3
import sys, json, re

def main():
    data = json.load(sys.stdin)
    command = data.get("tool_input", {}).get("command", "")
    response = data.get("tool_response", "")

    # Flag when generated files change
    gen_patterns = [
        r"generating",
        r"writing.*\.(json|yaml|toml)",
        r"created.*files",
    ]

    flagged = False
    for pattern in gen_patterns:
        if re.search(pattern, command, re.IGNORECASE):
            flagged = True
            break

    if flagged:
        print(json.dumps({
            "hookSpecificOutput": {
                "hookEventName": "PostToolUse",
                "additionalContext": "Generated files were modified. Consider running validation."
            }
        }))

    sys.exit(0)

if __name__ == "__main__":
    main()
```
