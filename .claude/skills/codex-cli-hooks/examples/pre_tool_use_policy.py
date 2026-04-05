#!/usr/bin/env python3
"""PreToolUse hook: Block destructive shell commands."""
import sys, json, re

DESTRUCTIVE_PATTERNS = [
    r"\brm\s+-rf\s+/",
    r"\bgit\s+push\s+--force",
    r"\bgit\s+push\s+-f\b",
    r"\bdd\s+if=",
    r">\s*/dev/sd",
    r"\bchmod\s+-R\s+777\s+/",
    r"\bdrop\s+database\b",
    r"\btruncate\s+table\b",
]

def main():
    data = json.load(sys.stdin)
    command = data.get("tool_input", {}).get("command", "")

    for pattern in DESTRUCTIVE_PATTERNS:
        if re.search(pattern, command, re.IGNORECASE):
            print(json.dumps({
                "decision": "block",
                "reason": f"Blocked: command matches destructive pattern '{pattern}'"
            }))
            sys.exit(0)

    # Allow
    sys.exit(0)

if __name__ == "__main__":
    main()
