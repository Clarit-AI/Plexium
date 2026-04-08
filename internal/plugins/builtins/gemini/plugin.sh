#!/bin/bash
# Generates .gemini/config.md for Gemini CLI.

set -eu

if [ -n "${PLEXIUM_DIR:-}" ]; then
  REPO_ROOT="$PLEXIUM_DIR"
elif git_root="$(git rev-parse --show-toplevel 2>/dev/null)"; then
  REPO_ROOT="$git_root"
else
  REPO_ROOT="$(pwd)"
fi

SCHEMA_PATH="$REPO_ROOT/.wiki/_schema.md"
OUTPUT_PATH="$REPO_ROOT/.gemini/config.md"

if [ ! -f "$SCHEMA_PATH" ]; then
  echo "Error: Schema file not found at $SCHEMA_PATH" >&2
  exit 1
fi

mkdir -p "$(dirname "$OUTPUT_PATH")"

cat > "$OUTPUT_PATH" <<'HEADER'
# Gemini CLI — Plexium Wiki Maintenance

You are working inside a Plexium repository. Keep `.wiki/` aligned with code changes.

## Plexium Schema

<!-- SCHEMA_INJECT_START -->
HEADER

cat "$SCHEMA_PATH" >> "$OUTPUT_PATH"

cat >> "$OUTPUT_PATH" <<'FOOTER'
<!-- SCHEMA_INJECT_END -->

## Reference

- Wiki: `.wiki/`
- Manifest: `.plexium/manifest.json`
- Retrieval: `plexium retrieve "<query>"`
FOOTER

echo "Generated $OUTPUT_PATH"
