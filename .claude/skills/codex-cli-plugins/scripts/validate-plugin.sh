#!/usr/bin/env bash
# validate-plugin.sh — Validate a Codex CLI plugin structure
# Usage: validate-plugin.sh [path/to/plugin]
set -euo pipefail

PLUGIN_DIR="${1:-.}"

echo "=== Validating plugin at: $PLUGIN_DIR ==="

ERRORS=0
WARNINGS=0

# 1. Check manifest exists
MANIFEST="$PLUGIN_DIR/.codex-plugin/plugin.json"
if [ ! -f "$MANIFEST" ]; then
    echo "ERROR: Missing required manifest at .codex-plugin/plugin.json"
    ERRORS=$((ERRORS + 1))
else
    # 2. Check valid JSON
    if ! python3 -c "import json; json.load(open('$MANIFEST'))" 2>/dev/null; then
        echo "ERROR: Invalid JSON in $MANIFEST"
        ERRORS=$((ERRORS + 1))
    else
        # 3. Check required fields
        for field in name version description; do
            if ! python3 -c "
import json
data = json.load(open('$MANIFEST'))
if '$field' not in data:
    print('ERROR: Missing required field: $field')
    exit(1)
if not data['$field']:
    print('ERROR: Empty field: $field')
    exit(1)
" 2>/dev/null; then
                ERRORS=$((ERRORS + 1))
            fi
        done

        # 4. Check name is kebab-case
        python3 -c "
import json, re
data = json.load(open('$MANIFEST'))
name = data.get('name', '')
if not re.match(r'^[a-z][a-z0-9-]*$', name):
    print(f'ERROR: Plugin name must be kebab-case, got: {name}')
    exit(1)
" 2>/dev/null || ERRORS=$((ERRORS + 1))

        # 5. Check version is semver-like
        python3 -c "
import json, re
data = json.load(open('$MANIFEST'))
version = data.get('version', '')
if not re.match(r'^\d+\.\d+\.\d+', version):
    print(f'WARNING: Version should be semver (x.y.z), got: {version}')
    exit(1)
" 2>/dev/null || WARNINGS=$((WARNINGS + 1))

        # 6. Check skills directory exists if referenced
        SKILLS_PATH=$(python3 -c "
import json
data = json.load(open('$MANIFEST'))
print(data.get('skills', ''))
" 2>/dev/null)

        if [ -n "$SKILLS_PATH" ]; then
            # Strip ./ prefix for check
            SKILLS_DIR="$PLUGIN_DIR/${SKILLS_PATH#./}"
            if [ ! -d "$SKILLS_DIR" ]; then
                echo "WARNING: Skills directory not found: $SKILLS_DIR"
                WARNINGS=$((WARNINGS + 1))
            else
                # Check for SKILL.md files
                SKILL_COUNT=$(find "$SKILLS_DIR" -name "SKILL.md" | wc -l | tr -d ' ')
                if [ "$SKILL_COUNT" -eq 0 ]; then
                    echo "WARNING: No SKILL.md files found in skills directory"
                    WARNINGS=$((WARNINGS + 1))
                else
                    echo "  Found $SKILL_COUNT skill(s)"
                fi
            fi
        fi
    fi
fi

# 7. Check nothing else in .codex-plugin/
EXTRA_FILES=$(find "$PLUGIN_DIR/.codex-plugin" -type f ! -name "plugin.json" 2>/dev/null | wc -l | tr -d ' ')
if [ "$EXTRA_FILES" -gt 0 ]; then
    echo "WARNING: Extra files in .codex-plugin/ (only plugin.json should be there)"
    WARNINGS=$((WARNINGS + 1))
fi

echo ""
if [ $ERRORS -eq 0 ] && [ $WARNINGS -eq 0 ]; then
    echo "VALID: Plugin structure looks correct"
elif [ $ERRORS -eq 0 ]; then
    echo "VALID with $WARNINGS warning(s)"
else
    echo "INVALID: $ERRORS error(s), $WARNINGS warning(s)"
    exit 1
fi
