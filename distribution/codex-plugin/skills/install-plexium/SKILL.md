---
name: install-plexium
description: Use when the user wants to install or verify the Plexium CLI from inside Codex.
---

# Install Plexium

## Workflow

1. Check whether `plexium` is already installed with `plexium --version`.
2. If it is missing, check whether `go` is available with `go version`.
3. If Go is available, run `go install github.com/Clarit-AI/Plexium/cmd/plexium@latest`.
4. Verify the install with `plexium --version`.
5. If Go is missing, explain that the bootstrap is blocked and say exactly which command failed.

Do not modify the current repository in this skill; only install or verify the binary.

## Secret Handling

- Never ask the user to paste secrets into chat.
- If a secret was already pasted, tell the user to rewind the session if possible and not commit that session to memento.
