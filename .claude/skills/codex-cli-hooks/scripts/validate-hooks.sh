#!/usr/bin/env bash
# validate-hooks.sh — Validate a Codex CLI hooks.json file
# Usage: validate-hooks.sh [path/to/hooks.json]
set -euo pipefail

HOOKS_FILE="${1:-.codex/hooks.json}"

if [ ! -f "$HOOKS_FILE" ]; then
    echo "ERROR: File not found: $HOOKS_FILE"
    exit 1
fi

# Check valid JSON
if ! python3 -c "import json; json.load(open('$HOOKS_FILE'))" 2>/dev/null; then
    echo "ERROR: Invalid JSON in $HOOKS_FILE"
    exit 1
fi

# Check top-level "hooks" key
if ! python3 -c "
import json
data = json.load(open('$HOOKS_FILE'))
if 'hooks' not in data:
    print('ERROR: Missing top-level \"hooks\" key')
    exit(1)
hooks = data['hooks']
valid_events = {'SessionStart', 'PreToolUse', 'PostToolUse', 'UserPromptSubmit', 'Stop'}
for event in hooks:
    if event not in valid_events:
        print(f'WARNING: Unknown event type: {event}')
    groups = hooks[event]
    if not isinstance(groups, list):
        print(f'ERROR: Event {event} should be an array')
        exit(1)
    for i, group in enumerate(groups):
        if 'hooks' not in group:
            print(f'ERROR: Group {i} in {event} missing \"hooks\" array')
            exit(1)
        for j, hook in enumerate(group['hooks']):
            if 'type' not in hook:
                print(f'ERROR: Hook {j} in {event} group {i} missing \"type\"')
                exit(1)
            if 'command' not in hook:
                print(f'ERROR: Hook {j} in {event} group {i} missing \"command\"')
                exit(1)
            if hook['type'] != 'command':
                print(f'WARNING: Hook type \"{hook[\"type\"]}\" — only \"command\" is supported')
print('VALID: hooks.json structure looks correct')
" 2>/dev/null; then
    exit 1
fi
