---
name: plexium-workflows
description: Use when the user wants to install Plexium, set up the current repo for Claude Code, verify readiness, or query the Plexium wiki from Claude Code.
---

# Plexium Workflows

## Overview

This plugin wraps the Plexium CLI so Claude Code can bootstrap, set up, verify, and query Plexium without leaving the TUI.

## Workflow

1. If `plexium` may be missing, use `/plexium-install`.
2. For repository onboarding, use `/plexium-setup` or `/plexium-setup-auto`.
3. For health checks, use `/plexium-verify`.
4. For retrieval, use `/plexium-retrieve`.
5. For MCP-only guidance, use `/plexium-connect`.

## Secret Handling

- Never ask the user to paste API keys or other secrets into chat.
- Prefer terminal-native secret handling such as exported env vars or direct terminal flags.
- If a secret was already pasted and memento is in use, tell the user to rewind the session if possible and not commit that session to memento.
