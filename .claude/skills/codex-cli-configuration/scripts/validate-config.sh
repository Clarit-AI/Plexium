#!/usr/bin/env bash
# validate-config.sh — Validate a Codex CLI config.toml
# Usage: validate-config.sh [path/to/config.toml]
set -euo pipefail

CONFIG="${1:-$HOME/.codex/config.toml}"

echo "=== Validating config: $CONFIG ==="

ERRORS=0
WARNINGS=0

# 1. Check file exists
if [ ! -f "$CONFIG" ]; then
    echo "ERROR: File not found: $CONFIG"
    exit 1
fi

# 2. Check valid TOML (basic syntax via python)
if ! python3 -c "
import tomllib
with open('$CONFIG', 'rb') as f:
    tomllib.load(f)
" 2>/dev/null; then
    echo "ERROR: Invalid TOML syntax"
    ERRORS=$((ERRORS + 1))
    # Can't continue parsing if TOML is invalid
    if [ $ERRORS -gt 0 ]; then
        echo "INVALID: $ERRORS error(s)"
        exit 1
    fi
fi

echo "  Valid TOML syntax"

# 3. Check for deprecated keys
python3 -c "
import tomllib
with open('$CONFIG', 'rb') as f:
    data = tomllib.load(f)

deprecated = {
    'experimental_instructions_file': 'model_instructions_file',
    'experimental_use_unified_exec_tool': 'features.unified_exec',
}

for old_key, new_key in deprecated.items():
    if old_key in data:
        print(f'WARNING: Deprecated key \"{old_key}\" — use \"{new_key}\" instead')
        exit(1)

# Check features table for deprecated web_search keys
features = data.get('features', {})
if 'web_search' in features:
    print('WARNING: features.web_search is deprecated — use top-level web_search setting')
    exit(1)
if 'web_search_cached' in features:
    print('WARNING: features.web_search_cached is deprecated — use web_search = \"cached\"')
    exit(1)
if 'web_search_request' in features:
    print('WARNING: features.web_search_request is deprecated — use web_search = \"live\"')
    exit(1)
" 2>/dev/null || WARNINGS=$((WARNINGS + 1))

# 4. Validate approval_policy values
python3 -c "
import tomllib
with open('$CONFIG', 'rb') as f:
    data = tomllib.load(f)

policy = data.get('approval_policy')
if policy is not None:
    valid = ['untrusted', 'on-request', 'never']
    if isinstance(policy, str) and policy not in valid:
        if isinstance(policy, dict) and 'granular' not in policy:
            print(f'ERROR: Invalid approval_policy: {policy}')
            exit(1)
        elif isinstance(policy, str):
            print(f'ERROR: Invalid approval_policy: {policy}. Valid: {valid}')
            exit(1)
" 2>/dev/null || ERRORS=$((ERRORS + 1))

# 5. Validate sandbox_mode values
python3 -c "
import tomllib
with open('$CONFIG', 'rb') as f:
    data = tomllib.load(f)

mode = data.get('sandbox_mode')
if mode is not None:
    valid = ['read-only', 'workspace-write', 'danger-full-access']
    if mode not in valid:
        print(f'ERROR: Invalid sandbox_mode: {mode}. Valid: {valid}')
        exit(1)
" 2>/dev/null || ERRORS=$((ERRORS + 1))

# 6. Validate web_search values
python3 -c "
import tomllib
with open('$CONFIG', 'rb') as f:
    data = tomllib.load(f)

ws = data.get('web_search')
if ws is not None:
    valid = ['disabled', 'cached', 'live']
    if ws not in valid:
        print(f'ERROR: Invalid web_search: {ws}. Valid: {valid}')
        exit(1)
" 2>/dev/null || ERRORS=$((ERRORS + 1))

# 7. Check model_provider references a defined provider
python3 -c "
import tomllib
with open('$CONFIG', 'rb') as f:
    data = tomllib.load(f)

provider = data.get('model_provider')
if provider and provider != 'openai':
    providers = data.get('model_providers', {})
    if provider not in providers:
        print(f'WARNING: model_provider \"{provider}\" not defined in [model_providers]')
        exit(1)
" 2>/dev/null || WARNINGS=$((WARNINGS + 1))

# 8. Check profile references
python3 -c "
import tomllib
with open('$CONFIG', 'rb') as f:
    data = tomllib.load(f)

profile = data.get('profile')
if profile:
    profiles = data.get('profiles', {})
    if profile not in profiles:
        print(f'ERROR: Default profile \"{profile}\" not defined in [profiles]')
        exit(1)
" 2>/dev/null || ERRORS=$((ERRORS + 1))

# 9. Check MCP servers have required fields
python3 -c "
import tomllib
with open('$CONFIG', 'rb') as f:
    data = tomllib.load(f)

for name, server in data.get('mcp_servers', {}).items():
    if server.get('enabled', True) is False:
        continue
    has_command = 'command' in server
    has_url = 'url' in server
    if not has_command and not has_url:
        print(f'ERROR: MCP server \"{name}\" missing command or url')
        exit(1)
" 2>/dev/null || ERRORS=$((ERRORS + 1))

echo ""
if [ $ERRORS -eq 0 ] && [ $WARNINGS -eq 0 ]; then
    echo "VALID: Configuration looks correct"
elif [ $ERRORS -eq 0 ]; then
    echo "VALID with $WARNINGS warning(s)"
else
    echo "INVALID: $ERRORS error(s), $WARNINGS warning(s)"
    exit 1
fi
