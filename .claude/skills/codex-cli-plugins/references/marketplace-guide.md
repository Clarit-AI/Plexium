# Codex CLI Marketplace — Setup and Distribution Guide

## Marketplace Architecture

A marketplace is a JSON catalog that Codex reads to discover and install plugins. Each marketplace appears as a selectable source in the plugin directory.

### Three Marketplace Sources

| Source | Path | Scope |
|--------|------|-------|
| Official Plugin Directory | Remote (OpenAI-hosted) | All users |
| Repo marketplace | `$REPO_ROOT/.agents/plugins/marketplace.json` | Repo team |
| Personal marketplace | `~/.agents/plugins/marketplace.json` | Individual |

## Creating a Marketplace

### Repo Marketplace

Best for team-shared plugins specific to a repository.

```json
{
  "name": "local-repo",
  "interface": {
    "displayName": "Team Plugins"
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

Store plugins at `$REPO_ROOT/plugins/<plugin-name>/`.

### Personal Marketplace

Best for individual developer tooling.

```json
{
  "name": "personal-tools",
  "interface": {
    "displayName": "My Tools"
  },
  "plugins": [
    {
      "name": "my-plugin",
      "source": {
        "source": "local",
        "path": "./.codex/plugins/my-plugin"
      },
      "policy": {
        "installation": "AVAILABLE",
        "authentication": "ON_INSTALL"
      },
      "category": "Developer Tools"
    }
  ]
}
```

Store plugins at `~/.codex/plugins/<plugin-name>/`.

## Marketplace Field Reference

### Top Level

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Marketplace identifier |
| `interface.displayName` | No | Title shown in Codex picker |
| `plugins` | Yes | Array of plugin entries |

### Plugin Entry

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Plugin name (must match `plugin.json`) |
| `source.source` | Yes | `"local"` for filesystem plugins |
| `source.path` | Yes | Relative path with `./` prefix |
| `policy.installation` | Yes | `AVAILABLE`, `INSTALLED_BY_DEFAULT`, or `NOT_AVAILABLE` |
| `policy.authentication` | Yes | `ON_INSTALL` or on first use |
| `category` | Yes | Display category |

## Path Resolution

- `source.path` is resolved relative to the **marketplace root** (the directory containing `marketplace.json`), NOT relative to `.agents/plugins/`.
- Always prefix with `./`.
- Keep paths inside the marketplace root.

## Install Workflow

1. Codex reads marketplace files at startup.
2. User opens Plugin Directory, selects marketplace source.
3. User browses and installs plugins.
4. Codex copies plugin to `~/.codex/plugins/cache/$MARKETPLACE_NAME/$PLUGIN_NAME/$VERSION/`.
5. Local plugins use `$VERSION` of `local`.
6. Plugin state (on/off) stored in `~/.codex/config.toml`.

## Curated Lists

One marketplace can expose multiple plugins:

```json
{
  "name": "team-toolkit",
  "interface": {
    "displayName": "Team Toolkit"
  },
  "plugins": [
    {
      "name": "code-review-helper",
      "source": { "source": "local", "path": "./plugins/code-review-helper" },
      "policy": { "installation": "INSTALLED_BY_DEFAULT", "authentication": "ON_INSTALL" },
      "category": "Developer Tools"
    },
    {
      "name": "deploy-notifier",
      "source": { "source": "local", "path": "./plugins/deploy-notifier" },
      "policy": { "installation": "AVAILABLE", "authentication": "ON_INSTALL" },
      "category": "DevOps"
    }
  ]
}
```

Start with one plugin, grow into a catalog.

## After Plugin Changes

1. Update the plugin directory the marketplace entry points to.
2. Restart Codex so the local install picks up new files.

## Publishing to Official Directory

Self-serve plugin publishing to the official Plugin Directory is coming soon. Prepare by:
- Completing all manifest fields (author, license, interface)
- Adding privacy policy and terms of service URLs
- Including visual assets (icon, logo, screenshots)
- Writing clear `defaultPrompt` entries
