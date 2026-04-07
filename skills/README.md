# Plexium Agent Skills

Skills that teach coding agents how to work with Plexium-powered repositories.

## Available Skills

| Skill | Purpose | Install for |
|-------|---------|-------------|
| [plexium-user](plexium-user/) | Read-execute-document-validate loop for repos using Plexium | Any agent working in a Plexium repo |
| [plexium-dev](plexium-dev/) | Contributing to the Plexium codebase itself | Developers working on Plexium |

## Installation

### Claude Code

Copy the skill directory into your project's `.claude/skills/`:

```bash
cp -r skills/plexium-user .claude/skills/plexium-user
```

Or for system-wide availability:

```bash
cp -r skills/plexium-user ~/.claude/skills/plexium-user
```

### Other Agents

The `skill.md` file in each skill directory is self-contained markdown. Feed it to your agent as context, add it to your CLAUDE.md / .cursorrules / codex instructions, or reference it in your MCP configuration.

### Via Plugin Adapter

`plexium plugin add claude` generates a CLAUDE.md that references the schema and wiki structure. The skills here go deeper — they include the full workflow, retrieval patterns, and edge cases.

## Structure

Each skill follows this layout:

```
skill-name/
  skill.md              # Main instructions (concise, actionable)
  reference/            # Supporting context (loaded on demand)
    index.md            # What's in this folder and when to read each file
    ...                 # Detailed reference docs
```

The `skill.md` is always under 200 lines. Detailed context lives in `reference/` and is loaded only when the agent needs it. The `reference/index.md` tells the agent which file to read for which situation.
