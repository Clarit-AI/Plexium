#!/usr/bin/env bash
# scaffold-plugin.sh — Scaffold a new Codex CLI plugin
# Usage: scaffold-plugin.sh <plugin-name> [target-dir]
set -euo pipefail

NAME="${1:?Usage: scaffold-plugin.sh <plugin-name> [target-dir]}"
TARGET="${2:-.}"

# Validate name (kebab-case)
if ! echo "$NAME" | grep -qE '^[a-z][a-z0-9-]*$'; then
    echo "ERROR: Plugin name must be kebab-case (lowercase, numbers, hyphens). Got: $NAME"
    exit 1
fi

PLUGIN_DIR="$TARGET/$NAME"

if [ -d "$PLUGIN_DIR" ]; then
    echo "ERROR: Directory already exists: $PLUGIN_DIR"
    exit 1
fi

# Create structure
mkdir -p "$PLUGIN_DIR/.codex-plugin"
mkdir -p "$PLUGIN_DIR/skills"
mkdir -p "$PLUGIN_DIR/assets"

# Write manifest
cat > "$PLUGIN_DIR/.codex-plugin/plugin.json" << EOF
{
  "name": "$NAME",
  "version": "0.1.0",
  "description": "TODO: Add description",
  "skills": "./skills/"
}
EOF

# Write example skill
SKILL_NAME="example"
mkdir -p "$PLUGIN_DIR/skills/$SKILL_NAME"

cat > "$PLUGIN_DIR/skills/$SKILL_NAME/SKILL.md" << 'EOF'
---
name: example
description: This skill should be used when the user asks to "example trigger phrase". TODO: Customize triggers and instructions.
---

TODO: Add skill instructions here.
EOF

echo "Created plugin at: $PLUGIN_DIR"
echo ""
echo "Structure:"
find "$PLUGIN_DIR" -type f | sed "s|$PLUGIN_DIR/||" | sort
echo ""
echo "Next steps:"
echo "  1. Edit $PLUGIN_DIR/.codex-plugin/plugin.json — update description"
echo "  2. Edit or replace skills/$SKILL_NAME/SKILL.md — add your skill"
echo "  3. Add to a marketplace — see references/marketplace-guide.md"
