# Configuration Reference Table

Complete searchable key reference for `config.toml` and `requirements.toml`.

## Core Model

| Key | Type | Description |
|-----|------|-------------|
| `model` | string | Model to use (e.g. `gpt-5-codex`) |
| `review_model` | string | Model override for `/review` |
| `model_provider` | string | Provider id from `model_providers` (default: `openai`) |
| `openai_base_url` | string | Base URL override for built-in `openai` provider |
| `model_context_window` | number | Context window tokens |
| `model_auto_compact_token_limit` | number | Token threshold for auto compaction |
| `model_catalog_json` | string (path) | JSON model catalog path |
| `oss_provider` | `lmstudio` \| `ollama` | Default local provider for `--oss` |

## Reasoning & Verbosity

| Key | Type | Description |
|-----|------|-------------|
| `model_reasoning_effort` | `minimal` \| `low` \| `medium` \| `high` \| `xhigh` | Reasoning depth |
| `plan_mode_reasoning_effort` | `none` \| `minimal` \| `low` \| `medium` \| `high` \| `xhigh` | Plan-mode override |
| `model_reasoning_summary` | `auto` \| `concise` \| `detailed` \| `none` | Summary detail |
| `model_verbosity` | `low` \| `medium` \| `high` | Text verbosity (Responses API only) |
| `model_supports_reasoning_summaries` | boolean | Force reasoning metadata on/off |

## Approval & Sandbox

| Key | Type | Description |
|-----|------|-------------|
| `approval_policy` | `untrusted` \| `on-request` \| `never` \| `{ granular = {...} }` | Approval prompt control |
| `approval_policy.granular.sandbox_approval` | boolean | Allow sandbox escalation prompts |
| `approval_policy.granular.rules` | boolean | Allow execpolicy rule prompts |
| `approval_policy.granular.mcp_elicitations` | boolean | Allow MCP elicitation prompts |
| `approval_policy.granular.request_permissions` | boolean | Allow request_permissions prompts |
| `approval_policy.granular.skill_approval` | boolean | Allow skill-script prompts |
| `allow_login_shell` | boolean | Allow login-shell semantics (default: true) |
| `sandbox_mode` | `read-only` \| `workspace-write` \| `danger-full-access` | Sandbox policy |
| `sandbox_workspace_write.writable_roots` | array\<string\> | Extra writable roots |
| `sandbox_workspace_write.network_access` | boolean | Allow outbound network in sandbox |
| `sandbox_workspace_write.exclude_tmpdir_env_var` | boolean | Exclude $TMPDIR from writable roots |
| `sandbox_workspace_write.exclude_slash_tmp` | boolean | Exclude /tmp from writable roots |
| `windows.sandbox` | `unelevated` \| `elevated` | Windows sandbox mode |
| `windows.sandbox_private_desktop` | boolean | Private desktop for sandbox (Windows) |

## Web Search & Tools

| Key | Type | Description |
|-----|------|-------------|
| `web_search` | `disabled` \| `cached` \| `live` | Web search mode (default: `cached`) |
| `tools.web_search` | boolean \| object | Web search tool config (context_size, allowed_domains, location) |
| `tools.view_image` | boolean | Enable local-image attachment tool |
| `tool_output_token_limit` | number | Token budget per tool output |
| `background_terminal_max_timeout` | number | Max poll window in ms (default: 300000) |

## Features

| Key | Default | Maturity | Description |
|-----|---------|----------|-------------|
| `features.apps` | false | Experimental | ChatGPT Apps/connectors |
| `features.codex_hooks` | false | Under development | Lifecycle hooks |
| `features.fast_mode` | true | Stable | Fast mode + service_tier fast |
| `features.multi_agent` | true | Stable | Subagent collaboration |
| `features.personality` | true | Stable | Personality selection |
| `features.shell_snapshot` | true | Stable | Snapshot shell env |
| `features.shell_tool` | true | Stable | Default shell tool |
| `features.smart_approvals` | false | Experimental | Guardian reviewer |
| `features.unified_exec` | true (not Windows) | Stable | Unified PTY-backed exec |
| `features.undo` | false | Stable | Per-turn git undo |
| `features.enable_request_compression` | true | Stable | Zstd request compression |
| `features.skill_mcp_dependency_install` | true | Stable | Auto-install MCP deps |
| `features.prevent_idle_sleep` | false | Experimental | Prevent sleep during turn |

## Shell Environment Policy

| Key | Type | Description |
|-----|------|-------------|
| `shell_environment_policy.inherit` | `all` \| `core` \| `none` | Baseline env inheritance |
| `shell_environment_policy.ignore_default_excludes` | boolean | Keep KEY/SECRET/TOKEN vars |
| `shell_environment_policy.exclude` | array\<string\> | Glob patterns to remove |
| `shell_environment_policy.include_only` | array\<string\> | Whitelist patterns |
| `shell_environment_policy.set` | map\<string,string\> | Explicit env overrides |
| `shell_environment_policy.experimental_use_profile` | boolean | Use user shell profile |

## Model Providers

| Key | Type | Description |
|-----|------|-------------|
| `model_providers.<id>.name` | string | Display name |
| `model_providers.<id>.base_url` | string | API base URL |
| `model_providers.<id>.env_key` | string | Env var for API key |
| `model_providers.<id>.env_key_instructions` | string | Key setup guidance |
| `model_providers.<id>.wire_api` | `responses` | Wire protocol |
| `model_providers.<id>.query_params` | map\<string,string\> | Extra query parameters |
| `model_providers.<id>.http_headers` | map\<string,string\> | Static HTTP headers |
| `model_providers.<id>.env_http_headers` | map\<string,string\> | Headers from env vars |
| `model_providers.<id>.request_max_retries` | number | HTTP retry count (default: 4) |
| `model_providers.<id>.stream_max_retries` | number | SSE retry count (default: 5) |
| `model_providers.<id>.stream_idle_timeout_ms` | number | SSE idle timeout (default: 300000) |
| `model_providers.<id>.supports_websockets` | boolean | WebSocket transport support |
| `model_providers.<id>.requires_openai_auth` | boolean | Uses OpenAI auth |
| `model_providers.<id>.experimental_bearer_token` | string | Direct bearer token (discouraged) |

## MCP Servers

| Key | Type | Description |
|-----|------|-------------|
| `mcp_servers.<id>.command` | string | Launcher command (stdio) |
| `mcp_servers.<id>.args` | array\<string\> | Command arguments |
| `mcp_servers.<id>.env` | map\<string,string\> | Environment variables |
| `mcp_servers.<id>.env_vars` | array\<string\> | Env var whitelist |
| `mcp_servers.<id>.cwd` | string | Working directory |
| `mcp_servers.<id>.url` | string | Endpoint (HTTP) |
| `mcp_servers.<id>.bearer_token_env_var` | string | Bearer token env var |
| `mcp_servers.<id>.http_headers` | map\<string,string\> | Static HTTP headers |
| `mcp_servers.<id>.env_http_headers` | map\<string,string\> | Headers from env vars |
| `mcp_servers.<id>.enabled` | boolean | Enable/disable server |
| `mcp_servers.<id>.required` | boolean | Fail if server can't init |
| `mcp_servers.<id>.startup_timeout_sec` | number | Startup timeout (default: 10) |
| `mcp_servers.<id>.tool_timeout_sec` | number | Per-tool timeout (default: 60) |
| `mcp_servers.<id>.enabled_tools` | array\<string\> | Tool allow-list |
| `mcp_servers.<id>.disabled_tools` | array\<string\> | Tool deny-list |
| `mcp_servers.<id>.scopes` | array\<string\> | OAuth scopes |
| `mcp_servers.<id>.oauth_resource` | string | OAuth resource parameter |

## Agents

| Key | Type | Description |
|-----|------|-------------|
| `agents.max_threads` | number | Max concurrent agent threads (default: 6) |
| `agents.max_depth` | number | Max nested spawn depth (default: 1) |
| `agents.job_max_runtime_seconds` | number | Per-worker timeout (default: 1800) |
| `agents.<name>.description` | string | Role guidance for agent type |
| `agents.<name>.config_file` | string (path) | TOML config layer for role |
| `agents.<name>.nickname_candidates` | array\<string\> | Display nicknames |

## Profiles

| Key | Type | Description |
|-----|------|-------------|
| `profile` | string | Default profile name |
| `profiles.<name>.*` | various | Profile overrides for any config key |
| `profiles.<name>.service_tier` | `flex` \| `fast` | Profile service tier |
| `profiles.<name>.plan_mode_reasoning_effort` | various | Profile plan-mode reasoning |
| `profiles.<name>.web_search` | `disabled` \| `cached` \| `live` | Profile web search |
| `profiles.<name>.personality` | `none` \| `friendly` \| `pragmatic` | Profile personality |
| `profiles.<name>.model_catalog_json` | string (path) | Profile model catalog |
| `profiles.<name>.model_instructions_file` | string (path) | Profile instructions file |
| `profiles.<name>.oss_provider` | `lmstudio` \| `ollama` | Profile OSS provider |
| `profiles.<name>.tools_view_image` | boolean | Profile view_image toggle |
| `profiles.<name>.analytics.enabled` | boolean | Profile analytics |
| `profiles.<name>.windows.sandbox` | various | Profile Windows sandbox |

## OTel

| Key | Type | Description |
|-----|------|-------------|
| `otel.environment` | string | Environment tag (default: `dev`) |
| `otel.exporter` | `none` \| `otlp-http` \| `otlp-grpc` | Log exporter |
| `otel.trace_exporter` | `none` \| `otlp-http` \| `otlp-grpc` | Trace exporter |
| `otel.metrics_exporter` | `none` \| `statsig` \| `otlp-http` \| `otlp-grpc` | Metrics exporter |
| `otel.log_user_prompt` | boolean | Include raw prompts (default: false) |
| `otel.exporter.<id>.endpoint` | string | Exporter endpoint |
| `otel.exporter.<id>.protocol` | `binary` \| `json` | Exporter protocol |
| `otel.exporter.<id>.headers` | map\<string,string\> | Exporter headers |
| `otel.exporter.<id>.tls.ca-certificate` | string | CA cert path |
| `otel.exporter.<id>.tls.client-certificate` | string | Client cert path |
| `otel.exporter.<id>.tls.client-private-key` | string | Client key path |

## TUI

| Key | Type | Description |
|-----|------|-------------|
| `tui.notifications` | boolean \| array\<string\> | TUI notifications |
| `tui.notification_method` | `auto` \| `osc9` \| `bel` | Notification mechanism |
| `tui.animations` | boolean | ASCII animations (default: true) |
| `tui.alternate_screen` | `auto` \| `always` \| `never` | Alternate screen usage |
| `tui.show_tooltips` | boolean | Welcome tooltips (default: true) |
| `tui.status_line` | array\<string\> \| null | Footer status items |
| `tui.theme` | string | Syntax highlighting theme |

## Other

| Key | Type | Description |
|-----|------|-------------|
| `notify` | array\<string\> | External notification command |
| `check_for_update_on_startup` | boolean | Check for updates (default: true) |
| `log_dir` | string (path) | Log directory |
| `sqlite_home` | string (path) | SQLite state directory |
| `file_opener` | `vscode` \| `cursor` \| `windsurf` \| `none` | Citation URI scheme |
| `hide_agent_reasoning` | boolean | Suppress reasoning events |
| `show_raw_agent_reasoning` | boolean | Show raw reasoning |
| `disable_paste_burst` | boolean | Disable burst-paste detection |
| `compact_prompt` | string | Compaction prompt override |
| `commit_attribution` | string | Co-author trailer override |
| `personality` | `none` \| `friendly` \| `pragmatic` | Communication style |
| `service_tier` | `flex` \| `fast` | Service tier preference |
| `developer_instructions` | string | Additional instructions |
| `model_instructions_file` | string (path) | Replacement instructions file |
| `history.persistence` | `save-all` \| `none` | History persistence |
| `history.max_bytes` | number | History file size cap |
| `project_doc_max_bytes` | number | Max AGENTS.md bytes (default: 32768) |
| `project_doc_fallback_filenames` | array\<string\> | AGENTS.md fallbacks |
| `project_root_markers` | array\<string\> | Root marker filenames |
| `cli_auth_credentials_store` | `file` \| `keyring` \| `auto` | Credential storage |
| `chatgpt_base_url` | string | ChatGPT auth base URL |
| `forced_login_method` | `chatgpt` \| `api` | Restrict auth method |
| `forced_chatgpt_workspace_id` | string (uuid) | Restrict to workspace |
| `mcp_oauth_credentials_store` | `auto` \| `file` \| `keyring` | MCP OAuth storage |
| `mcp_oauth_callback_port` | integer | Fixed OAuth callback port |
| `mcp_oauth_callback_url` | string | OAuth redirect URI override |
| `projects.<path>.trust_level` | string | `trusted` \| `untrusted` |
| `skills.config` | array\<object\> | Per-skill enablement overrides |
| `apps.<id>.enabled` | boolean | Per-app enabled state |
| `apps._default.enabled` | boolean | Default app state |
| `apps._default.destructive_enabled` | boolean | Default destructive allow |
| `apps._default.open_world_enabled` | boolean | Default open world allow |
| `analytics.enabled` | boolean | Anonymous usage data |
| `feedback.enabled` | boolean | `/feedback` submissions |
| `suppress_unstable_features_warning` | boolean | Suppress experimental warning |
| `experimental_compact_prompt_file` | string (path) | Compact prompt from file |
| `default_permissions` | string | Default permissions profile name |

## requirements.toml Keys

Admin-enforced constraints users cannot override:

| Key | Type | Description |
|-----|------|-------------|
| `allowed_approval_policies` | array\<string\> | Allowed approval_policy values |
| `allowed_sandbox_modes` | array\<string\> | Allowed sandbox_mode values |
| `allowed_web_search_modes` | array\<string\> | Allowed web_search values |
| `features.<name>` | boolean | Pinned feature flag |
| `mcp_servers.<id>.identity.command` | string | Allowed MCP stdio command |
| `mcp_servers.<id>.identity.url` | string | Allowed MCP HTTP URL |
| `rules.prefix_rules[].pattern[].token` | string | Command prefix token |
| `rules.prefix_rules[].pattern[].any_of` | array\<string\> | Alternative tokens |
| `rules.prefix_rules[].decision` | `prompt` \| `forbidden` | Enforced decision |
| `rules.prefix_rules[].justification` | string | Rationale for the rule |
