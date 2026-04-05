#!/usr/bin/env python3
"""SessionStart hook: Load workspace conventions and session notes."""
import sys, json, os

def main():
    data = json.load(sys.stdin)
    cwd = data.get("cwd", ".")
    source = data.get("source", "startup")

    context_parts = []

    # Load workspace conventions
    conventions_path = os.path.join(cwd, ".codex", "CONVENTIONS.md")
    if os.path.exists(conventions_path):
        with open(conventions_path) as f:
            context_parts.append(f.read())

    # On resume, load previous session notes
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
