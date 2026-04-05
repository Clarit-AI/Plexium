---
name: codex-cli-configuration
description: This skill should be used when the user asks to "configure Codex CLI", "edit config.toml", "set up Codex settings", "change the Codex model", "configure approval policy", "set sandbox mode", "add an MCP server to Codex", "create a Codex profile", "configure model providers", "set up OTel for Codex", or mentions Codex CLI configuration, config.toml, or settings. Provides comprehensive configuration reference for OpenAI Codex CLI.
---

# Codex CLI Configuration

Configure the OpenAI Codex CLI agent through layered TOML configuration files.

## Config File Locations

| File | Scope | Path |
|------|-------|------|
| User config | Personal defaults | `~/.codex/config.toml` |
| Project config | Per-repo overrides | `.codex/config.toml` |
| System config | Machine-wide (Unix) | `/etc/codex/config.toml` |
| Requirements | Admin-enforced | `requirements.toml` |

## Precedence (highest first)

1. CLI flags and `--config` overrides
2. Profile values (`--profile <name>`)
3. Project config files (closest `.codex/config.toml` wins; trusted projects only)
4. User config (`~/.codex/config.toml`)
5. System config (`/etc/codex/config.toml`)
6. Built-in defaults

Untrusted projects skip `.codex/` layers entirely.

## Quick Reference: Common Options

| Key | Values | Default | Purpose |
|-----|--------|---------|---------|
| `model` | string (e.g. `"gpt-5.4"`) | built-in | Default model |
| `approval_policy` | `untrusted`, `on-request`, `never`, `{ granular = {...} }` | `on-request` | When to prompt for approval |
| `sandbox_mode` | `read-only`, `workspace-write`, `danger-full-access` | `read-only` | Filesystem/network sandbox level |
| `web_search` | `disabled`, `cached`, `live` | `cached` | Web search mode |
| `model_reasoning_effort` | `minimal`, `low`, `medium`, `high`, `xhigh` | model default | Reasoning depth |
| `personality` | `none`, `friendly`, `pragmatic` | model default | Communication style |
| `model_provider` | provider id from `model_providers` | `"openai"` | Which provider to use |
| `file_opener` | `vscode`, `cursor`, `windsurf`, `none` | `vscode` | Citation link scheme |
| `profile` | profile name | unset | Default profile on startup |

## Feature Flags

Enable in `[features]` table or via `codex --enable <name>`:

| Flag | Default | Maturity | Purpose |
|------|---------|----------|---------|
| `codex_hooks` | false | Under development | Lifecycle hooks |
| `multi_agent` | true | Stable | Subagent collaboration |
| `fast_mode` | true | Stable | Fast mode + service_tier fast |
| `shell_snapshot` | true | Stable | Snapshot shell env |
| `undo` | false | Stable | Per-turn git undo |
| `smart_approvals` | false | Experimental | Guardian reviewer subagent |
| `apps` | false | Experimental | ChatGPT Apps/connectors |

## CLI One-off Overrides

```bash
# Dedicated flags
codex --model gpt-5.4

# Generic key/value (TOML syntax)
codex --config model='"gpt-5.4"'
codex --config sandbox_workspace_write.network_access=true
codex --config 'shell_environment_policy.include_only=["PATH","HOME"]'
```

## Profiles

Define named presets in `[profiles.<name>]` and switch with `codex --profile <name>`:

```toml
[profiles.deep-review]
model = "gpt-5-pro"
model_reasoning_effort = "high"
approval_policy = "never"
```

## Project Trust

```toml
[projects."/path/to/project"]
trust_level = "trusted"  # or "untrusted"
```

## Additional Resources

### Reference Files

- **`references/config-options.md`** — Detailed common options with examples and values
- **`references/advanced-config.md`** — Profiles, model providers, shell env policy, OTel, notifications, TUI, history, permissions
- **`references/config-reference-table.md`** — Full searchable key/type/description reference
- **`references/sample-config.md`** — Complete annotated config.toml with all sections

### Example Configs

- **`examples/minimal-config.toml`** — Minimal working config
- **`examples/developer-config.toml`** — Common developer setup with profiles and providers

### Scripts

- **`scripts/validate-config.sh`** — Validate config.toml structure and common errors

### External Source

For the latest configuration documentation: https://developers.openai.com/codex/config-basic
