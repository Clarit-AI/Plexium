#!/usr/bin/env bash
# test-hook.sh — Test a Codex CLI hook script with sample input
# Usage: test-hook.sh <event> <hook_script> [extra_json]
# Example: test-hook.sh PreToolUse ./pre_tool_use_policy.py '{"tool_input":{"command":"rm -rf /"}}'
set -euo pipefail

EVENT="${1:?Usage: test-hook.sh <event> <hook_script> [extra_json]}"
SCRIPT="${2:?Provide path to hook script}"
EXTRA="${3:-{}}"

# Build sample input
case "$EVENT" in
    SessionStart)
        BASE='{"session_id":"test-session","transcript_path":null,"cwd":"/tmp/test","hook_event_name":"SessionStart","model":"o3","source":"startup"}'
        ;;
    PreToolUse)
        BASE='{"session_id":"test-session","transcript_path":null,"cwd":"/tmp/test","hook_event_name":"PreToolUse","model":"o3","turn_id":"turn-1","tool_name":"Bash","tool_use_id":"call-1","tool_input":{"command":"echo hello"}}'
        ;;
    PostToolUse)
        BASE='{"session_id":"test-session","transcript_path":null,"cwd":"/tmp/test","hook_event_name":"PostToolUse","model":"o3","turn_id":"turn-1","tool_name":"Bash","tool_use_id":"call-1","tool_input":{"command":"echo hello"},"tool_response":"hello\n"}'
        ;;
    UserPromptSubmit)
        BASE='{"session_id":"test-session","transcript_path":null,"cwd":"/tmp/test","hook_event_name":"UserPromptSubmit","model":"o3","turn_id":"turn-1","prompt":"hello world"}'
        ;;
    Stop)
        BASE='{"session_id":"test-session","transcript_path":null,"cwd":"/tmp/test","hook_event_name":"Stop","model":"o3","turn_id":"turn-1","stop_hook_active":false,"last_assistant_message":"Done!"}'
        ;;
    *)
        echo "ERROR: Unknown event: $EVENT"
        echo "Valid events: SessionStart PreToolUse PostToolUse UserPromptSubmit Stop"
        exit 1
        ;;
esac

# Merge extra JSON into base
INPUT=$(python3 -c "
import json, sys
base = json.loads('$BASE')
extra = json.loads('$EXTRA')
base.update(extra)
print(json.dumps(base))
")

echo "=== Testing $EVENT hook: $SCRIPT ==="
echo "Input: $INPUT"
echo "---"

# Run hook
echo "$INPUT" | python3 "$SCRIPT"
EXIT_CODE=$?

echo "---"
echo "Exit code: $EXIT_CODE"

if [ $EXIT_CODE -eq 2 ]; then
    echo "HOOK BLOCKED (exit code 2)"
elif [ $EXIT_CODE -eq 0 ]; then
    echo "HOOK PASSED (exit code 0)"
else
    echo "HOOK FAILED (exit code $EXIT_CODE)"
fi
