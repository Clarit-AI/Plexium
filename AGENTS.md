# Agent Instructions

Use the task-tracking workflow specified by the owner for the current work. Beads is not required for Plexium development. Historical Beads issues can provide context; Plexium's optional `plexium beads` commands remain available to users who link task IDs to wiki pages.

## Current Traycer Epic and Handoff

- **Epic:** Plexium MarkedUp Solo Restart (`1088c958-cb42-463c-8c23-d9e728f7a0aa`)
- **Epic artifacts:** `/Users/bbrenner/.traycer/epics/1088c958-cb42-463c-8c23-d9e728f7a0aa/artifacts/`
- **Most recent handoff (2026-09-26):** `artifacts/markedup-handoff-to-jev-orchestrator/index.md` under that epic
- **Repo copy for recovery:** [docs/handoffs/2026-09-26-jev-orchestrator.md](docs/handoffs/2026-09-26-jev-orchestrator.md)

The handoff covers the Jev follow-on work and the MarkedUp/Plexium Linear backlog. Read the repo copy when reopening this work; the Traycer handoff was previously swept out of the live artifacts directory and restored from quarantine.

## Non-Interactive Shell Commands

**ALWAYS use non-interactive flags** with file operations to avoid hanging on confirmation prompts.

Shell commands like `cp`, `mv`, and `rm` may be aliased to include `-i` (interactive) mode on some systems, causing the agent to hang indefinitely waiting for y/n input.

**Use these forms instead:**
```bash
# Force overwrite without prompting
cp -f source dest           # NOT: cp source dest
mv -f source dest           # NOT: mv source dest
rm -f file                  # NOT: rm file

# For recursive operations
rm -rf directory            # NOT: rm -r directory
cp -rf source dest          # NOT: cp -r source dest
```

**Other commands that may prompt:**
- `scp` - use `-o BatchMode=yes` for non-interactive
- `ssh` - use `-o BatchMode=yes` to fail instead of prompting
- `apt-get` - use `-y` flag
- `brew` - use `HOMEBREW_NO_AUTO_UPDATE=1` env var

## Session Completion

1. Record remaining work and update status in the workflow chosen for the task.
2. Run quality gates appropriate to the changes.
3. Commit only task-owned changes, preserve unrelated working-tree edits, and safely sync and push the branch. Work with committed changes is not complete until the push succeeds.
4. Verify the branch is up to date with its remote and provide a handoff for the next session.
