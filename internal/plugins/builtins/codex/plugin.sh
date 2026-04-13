#!/bin/bash
# Generates AGENTS.md for Codex.

set -eu

if [ -n "${PLEXIUM_DIR:-}" ]; then
  REPO_ROOT="$PLEXIUM_DIR"
elif git_root="$(git rev-parse --show-toplevel 2>/dev/null)"; then
  REPO_ROOT="$git_root"
else
  REPO_ROOT="$(pwd)"
fi

SCHEMA_PATH="$REPO_ROOT/.wiki/_schema.md"
OUTPUT_PATH="$REPO_ROOT/AGENTS.md"
_PLEXIUM_TMPFILES=""
cleanup_tmpfiles() { rm -f $_PLEXIUM_TMPFILES; }
trap cleanup_tmpfiles EXIT

GENERATED_PATH="$(mktemp "${TMPDIR:-/tmp}/plexium-codex-agents.XXXXXX")"
_PLEXIUM_TMPFILES="$_PLEXIUM_TMPFILES $GENERATED_PATH"

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
PROJECT_NAME="$(basename "$REPO_ROOT")"

# Prefer env vars exported by the Go adapter runner; fall back to awk only
# when the env var is unset (e.g. running standalone during development).
if [ -n "${PLEXIUM_MEMENTO_ENABLED+x}" ]; then
  MEMENTO_ENABLED="$PLEXIUM_MEMENTO_ENABLED"
elif [ -f "$REPO_ROOT/.plexium/config.yml" ]; then
  MEMENTO_ENABLED="false"
  if awk '
    /^[^[:space:]]/ { in_integrations = ($0 ~ /^integrations:/); next }
    in_integrations && /memento:[[:space:]]*true/ { found = 1 }
    END { exit found ? 0 : 1 }
  ' "$REPO_ROOT/.plexium/config.yml"; then
    MEMENTO_ENABLED="true"
  fi
else
  MEMENTO_ENABLED="false"
fi

if [ -n "${PLEXIUM_ASSISTIVE_ENABLED+x}" ]; then
  ASSISTIVE_ENABLED="$PLEXIUM_ASSISTIVE_ENABLED"
elif [ -f "$REPO_ROOT/.plexium/config.yml" ]; then
  ASSISTIVE_ENABLED="false"
  if awk '
    /^[^[:space:]]/ {
      if ($0 ~ /^assistiveAgent:/) { in_assistive = 1; assistive_enabled = 0; provider_enabled = 0; next }
      in_assistive = 0
    }
    in_assistive && /enabled:[[:space:]]*true/ && !provider_enabled { assistive_enabled = 1 }
    in_assistive && /providers:/ { in_providers = 1; next }
    in_providers && /^[[:space:]]*-?[[:space:]]*name:/ { next }
    in_providers && /enabled:[[:space:]]*true/ { provider_enabled = 1 }
    END { exit (assistive_enabled && provider_enabled) ? 0 : 1 }
  ' "$REPO_ROOT/.plexium/config.yml"; then
    ASSISTIVE_ENABLED="true"
  fi
else
  ASSISTIVE_ENABLED="false"
fi

project_details_line() {
  awk '/^## / && index($0, " Project Details Start Here") { print NR; exit }' "$1"
}

merge_with_existing() {
  generated="$1"
  output="$2"

  if [ ! -f "$output" ]; then
    cp -f "$generated" "$output"
    return
  fi

  generated_details_line="$(project_details_line "$generated")"
  existing_details_line="$(project_details_line "$output")"
  if [ -z "$generated_details_line" ] || [ -z "$existing_details_line" ]; then
    cp -f "$generated" "$output"
    return
  fi

  merged_path="$(mktemp "${TMPDIR:-/tmp}/plexium-codex-agents-merged.XXXXXX")"
  _PLEXIUM_TMPFILES="$_PLEXIUM_TMPFILES $merged_path"
  {
    head -n "$((generated_details_line - 1))" "$generated"
    printf '\n'
    tail -n +"$existing_details_line" "$output"
  } > "$merged_path"
  mv -f "$merged_path" "$output"
}

cat > "$GENERATED_PATH" <<'HEADER'
# OpenAI Codex — Plexium Wiki Maintenance

## Important: Plexium Wiki Maintenance Layer

> This section describes Plexium's repository knowledge layer. It is not the main project brief.
> Project-specific context starts at the project details heading below.

You are working on a **Plexium** project. This repository uses an LLM-maintained
wiki (`.wiki/`) that you are responsible for keeping current.

## Your Responsibilities

1. **Before any code change**: Read `.wiki/_index.md` and relevant wiki pages
2. **After any code change**: Update affected wiki pages
3. **Never modify** pages with `ownership: human-authored`
4. **Treat the starter scaffold as incomplete** until `plexium convert` and a real first-pass population run have happened

HEADER

cat >> "$GENERATED_PATH" <<'RETRIEVAL'
## Retrieval Protocol

- If the `pageindex_search` MCP tool is available, use it to retrieve wiki context before shelling out.
- Otherwise run `plexium retrieve "<topic>"` to gather wiki context from the CLI.

RETRIEVAL

if [ "$MEMENTO_ENABLED" = "true" ]; then
  cat >> "$GENERATED_PATH" <<'MEMENTO'
## Commit Protocol

- Use `git memento commit` instead of `git commit` so session provenance is recorded with commits.

MEMENTO
fi

if [ "$ASSISTIVE_ENABLED" = "true" ]; then
  cat >> "$GENERATED_PATH" <<'ASSISTIVE'
## Assistive Agent

- An assistive provider is configured for semantic review and optional provider-primary daemon work.
- Use `plexium lint --full` when semantic wiki review is needed.
- Keep the normal wiki workflow unless a daemon/provider-primary run is explicitly handling it.

ASSISTIVE
fi

cat >> "$GENERATED_PATH" <<'HEADER'
## First Population Pass

When the wiki is mostly starter scaffold:

1. Run `plexium convert` first to bootstrap grounded content
2. Prefer **Codex sub-agents** for the first wiki build when available
3. Split the first pass into:
   - retriever / context gatherer
   - documenter / wiki writer
   - optional validator / linter
4. Use `.plexium/prompts/assistive/initial-wiki-population.md` as the operating contract
5. Use `.plexium/prompts/assistive/retriever.md` and `.plexium/prompts/assistive/documenter.md` for role-specific guidance

## Plexium Schema

<!-- SCHEMA_INJECT_START -->
Read `.wiki/_schema.md` for the full wiki constitution and ownership rules.
<!-- SCHEMA_INJECT_END -->

## Quick Reference

- Wiki: `.wiki/`
- Manifest: `.plexium/manifest.json`
- Report issues: `plexium lint --ci`

## Detected Stack
HEADER

printf '[%s]\n\n' "$DETECTED_STACK" >> "$GENERATED_PATH"

cat >> "$GENERATED_PATH" <<'COMMANDS'
## Commands

```bash
plexium convert   # Bootstrap useful content from the current repository
plexium sync      # Update wiki after changes
plexium lint      # Check wiki health
plexium retrieve  # Query the wiki
```
COMMANDS

printf '\n## %s Project Details Start Here\n\nAdd project-specific context below this heading.\n' "$PROJECT_NAME" >> "$GENERATED_PATH"

merge_with_existing "$GENERATED_PATH" "$OUTPUT_PATH"

echo "Generated $OUTPUT_PATH"
