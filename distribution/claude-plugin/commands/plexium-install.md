---
description: Bootstrap the Plexium CLI in the current environment
---

Check whether `plexium` is already installed by running `plexium --version`.

If it is missing:
- Check whether `go` is available with `go version`.
- If Go is available, run `go install github.com/Clarit-AI/Plexium/cmd/plexium@latest`.
- Verify the install by running `plexium --version`.

If Go is missing, explain that Plexium currently installs via Go and tell the user exactly which command failed.

Do not modify the current repository in this command; only install or verify the binary.
