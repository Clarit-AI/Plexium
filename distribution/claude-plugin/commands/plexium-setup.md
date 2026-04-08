---
description: Prepare the current repository for Claude Code using Plexium's guided setup flow
---

Run `plexium setup claude` from the current repository root.

Before you run it:
- Confirm you are inside a git repository.
- If `plexium` is missing, run `/plexium-install` first.
- Never ask the user to paste secrets into chat. For provider setup, prefer terminal env vars or `plexium agent setup --api-key` entered directly in the terminal, outside the chat transcript.
- If the user already pasted a secret into chat, tell them to rewind the session if possible and not commit that session to memento.

After the command finishes:
- Summarize whether the repo is ready.
- If MCP is not configured yet, show the exact native command that Plexium printed and explain that rerunning with `--write-config` will apply it automatically.
