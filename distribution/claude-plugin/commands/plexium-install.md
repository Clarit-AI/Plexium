---
description: Bootstrap the Plexium CLI in the current environment
---

Check whether `plexium` is already installed by running `plexium --version`.

Important safety rule:
- Never ask the user to paste API keys or secrets into chat.
- If a secret was already pasted, tell the user to rewind the session if possible and avoid committing that session to memento.

If it is missing:
- Check whether `go` is available with `go version`.
- If Go is available, run `go install github.com/Clarit-AI/Plexium/cmd/plexium@latest`.
- Verify the install by running `plexium --version`.

If Go is missing, explain that Plexium currently installs via Go and tell the user exactly which command failed.

Do not modify the current repository in this command; only install or verify the binary.
