# Codex CLI Plugin Manifest — Complete Reference

## Full Manifest Example

```json
{
  "name": "my-plugin",
  "version": "0.1.0",
  "description": "Bundle reusable skills and app integrations.",
  "author": {
    "name": "Your team",
    "email": "team@example.com",
    "url": "https://example.com"
  },
  "homepage": "https://example.com/plugins/my-plugin",
  "repository": "https://github.com/example/my-plugin",
  "license": "MIT",
  "keywords": ["research", "crm"],
  "skills": "./skills/",
  "mcpServers": "./.mcp.json",
  "apps": "./.app.json",
  "interface": {
    "displayName": "My Plugin",
    "shortDescription": "Reusable skills and apps",
    "longDescription": "Distribute skills and app integrations together.",
    "developerName": "Your team",
    "category": "Productivity",
    "capabilities": ["Read", "Write"],
    "websiteURL": "https://example.com",
    "privacyPolicyURL": "https://example.com/privacy",
    "termsOfServiceURL": "https://example.com/terms",
    "defaultPrompt": [
      "Use My Plugin to summarize new CRM notes.",
      "Use My Plugin to triage new customer follow-ups."
    ],
    "brandColor": "#10A37F",
    "composerIcon": "./assets/icon.png",
    "logo": "./assets/logo.png",
    "screenshots": ["./assets/screenshot-1.png"]
  }
}
```

## Field Reference

### Core Identity

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | `string` | Yes | Plugin identifier. Use kebab-case. Codex uses this as namespace. |
| `version` | `string` | Yes | Semantic version (semver). |
| `description` | `string` | Yes | Short description of what the plugin provides. |

### Publisher Metadata

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `author` | `object` | No | `name`, `email`, `url` fields |
| `homepage` | `string` | No | Plugin homepage URL |
| `repository` | `string` | No | Source repository URL |
| `license` | `string` | No | SPDX license identifier |
| `keywords` | `string[]` | No | Discovery keywords |

### Component Paths

All paths are relative to plugin root, prefixed with `./`.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `skills` | `string` | No | Path to skills directory |
| `mcpServers` | `string` | No | Path to `.mcp.json` file |
| `apps` | `string` | No | Path to `.app.json` file |

### Interface (Install Surface)

Controls how Codex presents the plugin in the Plugin Directory.

| Field | Type | Description |
|-------|------|-------------|
| `displayName` | `string` | Title shown in Codex UI |
| `shortDescription` | `string` | One-line summary |
| `longDescription` | `string` | Full description |
| `developerName` | `string` | Publisher/team name |
| `category` | `string` | e.g., `"Productivity"`, `"Developer Tools"` |
| `capabilities` | `string[]` | e.g., `["Read", "Write"]` |
| `websiteURL` | `string` | External website link |
| `privacyPolicyURL` | `string` | Privacy policy URL |
| `termsOfServiceURL` | `string` | Terms of service URL |
| `defaultPrompt` | `string[]` | Starter prompts for users |
| `brandColor` | `string` | Hex color for branding |
| `composerIcon` | `string` | Path to composer icon (e.g., `"./assets/icon.png"`) |
| `logo` | `string` | Path to logo image |
| `screenshots` | `string[]` | Array of screenshot paths |

## File Placement Rules

- `plugin.json` must be at `.codex-plugin/plugin.json` — nothing else goes in `.codex-plugin/`
- `skills/`, `assets/`, `.mcp.json`, `.app.json` live at the plugin root
- Asset paths in manifest use `./assets/` prefix
- Skill directories follow `skills/<skill-name>/SKILL.md` structure

## Minimal vs Published Manifests

**Minimal** (local/testing):
```json
{
  "name": "my-plugin",
  "version": "1.0.0",
  "description": "Quick workflow",
  "skills": "./skills/"
}
```

**Published** (distribution):
Include `author`, `license`, `interface` with `displayName`, `shortDescription`, `privacyPolicyURL`, `termsOfServiceURL`, and visual assets.

## Common `categories`

- `"Productivity"`
- `"Developer Tools"`
- `"Research"`
- `"Communication"`
- `"Data Analysis"`
- `"Security"`
- `"Design"`
