#!/usr/bin/env python3
"""Stop hook: Auto-continue when tests are failing."""
import sys, json, subprocess

def main():
    data = json.load(sys.stdin)

    # Prevent infinite loops
    if data.get("stop_hook_active", False):
        sys.exit(0)

    last_message = data.get("last_assistant_message") or ""
    cwd = data.get("cwd", ".")

    # Only continue if assistant was working on tests
    if "test" not in last_message.lower():
        sys.exit(0)

    # Run tests
    try:
        result = subprocess.run(
            ["npm", "test"],
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=60
        )

        if result.returncode != 0:
            output_tail = result.stdout[-500:] if len(result.stdout) > 500 else result.stdout
            print(json.dumps({
                "decision": "block",
                "reason": f"Tests are still failing. Output:\n{output_tail}\nPlease fix the failing tests."
            }))
    except (subprocess.TimeoutExpired, FileNotFoundError):
        pass

    sys.exit(0)

if __name__ == "__main__":
    main()
