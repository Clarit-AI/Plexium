---
name: setup-plexium
description: Use when the user wants to initialize Plexium, set up the current repository for Codex, verify readiness, or connect Plexium's MCP server in Codex.
---

# Set Up Plexium For Codex

## Workflow

1. Confirm the current directory is inside a git repository.
2. If `plexium` is missing, use the `install-plexium` skill first.
3. For guided setup, run `plexium setup codex`.
4. If the user explicitly wants the native config applied, run `plexium setup codex --write-config`.
5. For a verification-only pass, run `plexium verify codex`.
6. If the user only wants the native MCP command, run `plexium pageindex connect codex`.

## Secret Handling

- Never ask the user to paste API keys or other secrets into chat.
- For provider setup, prefer env vars, `--api-key-file`, or `--api-key-stdin`, entered outside the chat transcript.
- If the user already pasted a secret and memento is enabled, tell them to rewind the session if possible and not commit that session to memento.

## Output

- Summarize whether the repository itself is ready.
- Separate structural repo problems from MCP configuration warnings.
- When MCP is not yet configured, show the exact native command Plexium recommends.
