#!/bin/bash
# Generates CLAUDE.md for Claude Code.

set -eu

if [ -n "${PLEXIUM_DIR:-}" ]; then
  REPO_ROOT="$PLEXIUM_DIR"
elif git_root="$(git rev-parse --show-toplevel 2>/dev/null)"; then
  REPO_ROOT="$git_root"
else
  REPO_ROOT="$(pwd)"
fi

SCHEMA_PATH="$REPO_ROOT/.wiki/_schema.md"
OUTPUT_PATH="$REPO_ROOT/CLAUDE.md"

if [ ! -f "$SCHEMA_PATH" ]; then
  echo "Error: Schema file not found at $SCHEMA_PATH" >&2
  exit 1
fi

detect_stack() {
  if [ -f "$REPO_ROOT/package.json" ] && [ -f "$REPO_ROOT/tsconfig.json" ]; then
    echo "typescript"
  elif [ -f "$REPO_ROOT/requirements.txt" ] || [ -f "$REPO_ROOT/setup.py" ]; then
    echo "python"
  elif [ -f "$REPO_ROOT/Cargo.toml" ]; then
    echo "rust"
  elif [ -f "$REPO_ROOT/go.mod" ]; then
    echo "go"
  elif [ -f "$REPO_ROOT/pom.xml" ]; then
    echo "java"
  elif [ -f "$REPO_ROOT/package.json" ]; then
    echo "javascript"
  else
    echo "generic"
  fi
}

DETECTED_STACK="$(detect_stack)"

cat > "$OUTPUT_PATH" <<'HEADER'
# Claude Code — Plexium Wiki Maintenance

You are working on a **Plexium** project. This repository uses an LLM-maintained
wiki (`.wiki/`) that you are responsible for keeping current.

## Your Responsibilities

1. **Before any code change**: Read `.wiki/_index.md` and relevant wiki pages
2. **After any code change**: Update affected wiki pages
3. **Never modify** pages with `ownership: human-authored`

## Plexium Schema

<!-- SCHEMA_INJECT_START -->
HEADER

cat "$SCHEMA_PATH" >> "$OUTPUT_PATH"

cat >> "$OUTPUT_PATH" <<'FOOTER'
<!-- SCHEMA_INJECT_END -->

## Quick Reference

- Wiki: `.wiki/`
- Manifest: `.plexium/manifest.json`
- Report issues: `plexium lint --ci`

## Detected Stack
FOOTER

printf '[%s]\n\n' "$DETECTED_STACK" >> "$OUTPUT_PATH"

cat >> "$OUTPUT_PATH" <<'COMMANDS'
## Commands

```bash
plexium sync      # Update wiki after changes
plexium lint      # Check wiki health
plexium retrieve  # Query the wiki
```
COMMANDS

echo "Generated $OUTPUT_PATH"
