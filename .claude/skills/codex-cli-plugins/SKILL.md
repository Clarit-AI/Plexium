---
name: Codex CLI Plugins
description: This skill should be used when the user asks to "create a Codex plugin", "build a plugin for Codex CLI", "package a Codex skill", "set up a Codex marketplace", "publish a Codex plugin", "configure plugin.json manifest", "add MCP to a Codex plugin", "install a local plugin in Codex", or mentions Codex plugin structure, marketplace metadata, or plugin distribution. Covers the full Codex CLI plugin lifecycle from scaffolding through packaging and distribution.
---

# Codex CLI Plugins

Plugins are distributable packages for OpenAI's Codex CLI that bundle skills, app integrations, and MCP server configurations. Build a plugin when sharing workflows across teams, bundling integrations, or publishing stable packages.

## Quick Start

### Scaffold with `$plugin-creator`

The built-in `$plugin-creator` skill generates the required `.codex-plugin/plugin.json` manifest and a local marketplace entry for testing.

### Manual Minimal Plugin

```
my-plugin/
├── .codex-plugin/
│   └── plugin.json      # Required manifest
└── skills/
    └── my-skill/
        └── SKILL.md
```

Minimal `plugin.json`:

```json
{
  "name": "my-plugin",
  "version": "1.0.0",
  "description": "Reusable workflow",
  "skills": "./skills/"
}
```

Use kebab-case for `name` — Codex uses it as the plugin identifier and namespace.

Add a skill at `skills/<skill-name>/SKILL.md` with standard frontmatter (`name` + `description`).

## Plugin Structure

```
my-plugin/
├── .codex-plugin/
│   └── plugin.json      # Required: manifest (only file in this dir)
├── skills/              # Optional: bundled skills
│   └── my-skill/
│       └── SKILL.md
├── .app.json            # Optional: app/connector mappings
├── .mcp.json            # Optional: MCP server configuration
└── assets/              # Optional: icons, logos, screenshots
```

Only `plugin.json` belongs in `.codex-plugin/`. Everything else lives at the plugin root.

## Manifest Reference

The manifest has three jobs: identify the plugin, point to components, and provide install-surface metadata.

### Core Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Plugin identifier (kebab-case) |
| `version` | Yes | Semver version |
| `description` | Yes | Short description |
| `skills` | No | Path to skills directory (e.g., `"./skills/"`) |
| `mcpServers` | No | Path to `.mcp.json` |
| `apps` | No | Path to `.app.json` |

### Publisher Fields

`author` (object with `name`, `email`, `url`), `homepage`, `repository`, `license`, `keywords`.

### Interface Fields (Install Surface)

| Field | Purpose |
|-------|---------|
| `displayName` | Title shown in Codex |
| `shortDescription` | One-line summary |
| `longDescription` | Full description |
| `developerName` | Publisher name |
| `category` | e.g., `"Productivity"` |
| `capabilities` | e.g., `["Read", "Write"]` |
| `websiteURL` | External link |
| `privacyPolicyURL` | Privacy policy |
| `termsOfServiceURL` | Terms |
| `defaultPrompt` | Array of starter prompts |
| `brandColor` | Brand hex color |
| `composerIcon` | Path to icon |
| `logo` | Path to logo |
| `screenshots` | Array of screenshot paths |

### Path Rules

- All paths relative to plugin root, prefixed with `./`.
- Visual assets go in `./assets/`.
- `skills` points to skills dir, `apps` to `.app.json`, `mcpServers` to `.mcp.json`.

For a complete manifest example, see `references/manifest-reference.md`.

## Marketplace System

A marketplace is a JSON catalog of plugins. Each marketplace appears as a selectable source in Codex's plugin directory.

### Marketplace Locations

| Scope | Path |
|-------|------|
| Repo | `$REPO_ROOT/.agents/plugins/marketplace.json` |
| Personal | `~/.agents/plugins/marketplace.json` |
| Official | Powers the Plugin Directory |

### Marketplace Format

```json
{
  "name": "local-example-plugins",
  "interface": {
    "displayName": "Local Example Plugins"
  },
  "plugins": [
    {
      "name": "my-plugin",
      "source": {
        "source": "local",
        "path": "./plugins/my-plugin"
      },
      "policy": {
        "installation": "AVAILABLE",
        "authentication": "ON_INSTALL"
      },
      "category": "Productivity"
    }
  ]
}
```

Key rules:
- `source.path` is relative to marketplace root, starts with `./`
- Always include `policy.installation`, `policy.authentication`, `category`
- `installation` values: `AVAILABLE`, `INSTALLED_BY_DEFAULT`, `NOT_AVAILABLE`
- `authentication` values: `ON_INSTALL` or on first use
- One marketplace can list one or many plugins

### Install Locations

Codex installs plugins to `~/.codex/plugins/cache/$MARKETPLACE_NAME/$PLUGIN_NAME/$VERSION/`. Local plugins use `$VERSION` of `local`.

Plugin on/off state is stored in `~/.codex/config.toml`.

## Local Plugin Installation

### Repo-Scoped

1. Copy plugin to `$REPO_ROOT/plugins/my-plugin`
2. Add entry to `$REPO_ROOT/.agents/plugins/marketplace.json`
3. Restart Codex

### Personal

1. Copy plugin to `~/.codex/plugins/my-plugin`
2. Add entry to `~/.agents/plugins/marketplace.json`
3. Restart Codex

After changing a plugin, update the plugin directory and restart Codex.

## Additional Resources

### Reference Files

- **`references/manifest-reference.md`** — Complete manifest with all fields documented
- **`references/marketplace-guide.md`** — Detailed marketplace setup and distribution

### Examples

- **`examples/minimal-plugin/`** — Minimal plugin with one skill
- **`examples/complete-plugin/`** — Full plugin with manifest, skills, and assets
- **`examples/repo-marketplace.json`** — Repo-scoped marketplace config
- **`examples/personal-marketplace.json`** — Personal marketplace config

### Scripts

- **`scripts/scaffold-plugin.sh`** — Scaffold a new plugin structure
- **`scripts/validate-plugin.sh`** — Validate plugin.json and structure

### External Reference

Official docs: [Codex Plugins](https://developers.openai.com/codex/plugins)
