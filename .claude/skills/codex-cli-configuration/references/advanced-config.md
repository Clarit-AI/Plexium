# Advanced Configuration

Profiles, model providers, shell environment policy, observability, notifications, TUI, history, and permissions.

## Profiles

Named configuration presets switchable via `--profile`. Not currently supported in the IDE extension.

```toml
[profiles.deep-review]
model = "gpt-5-pro"
model_reasoning_effort = "high"
approval_policy = "never"
model_catalog_json = "/Users/me/.codex/model-catalogs/deep-review.json"

[profiles.lightweight]
model = "gpt-4.1"
approval_policy = "untrusted"
```

Set a default profile:

```toml
profile = "deep-review"
```

Profile-scoped overrides: `service_tier`, `plan_mode_reasoning_effort`, `web_search`, `personality`, `model_catalog_json`, `model_instructions_file`, `oss_provider`, `tools_view_image`, `analytics.enabled`, `windows.sandbox`.

## Custom Model Providers

Define providers under `[model_providers.<id>]`:

```toml
model = "gpt-5.1"
model_provider = "proxy"

[model_providers.proxy]
name = "OpenAI using LLM proxy"
base_url = "http://proxy.example.com"
env_key = "OPENAI_API_KEY"

[model_providers.ollama]
name = "Ollama"
base_url = "http://localhost:11434/v1"

[model_providers.mistral]
name = "Mistral"
base_url = "https://api.mistral.ai/v1"
env_key = "MISTRAL_API_KEY"
```

### Provider fields

| Key | Purpose |
|-----|---------|
| `name` | Display name |
| `base_url` | API base URL |
| `env_key` | Env var for API key |
| `env_key_instructions` | Setup guidance for the key |
| `wire_api` | Protocol (only `"responses"` supported) |
| `query_params` | Extra query parameters |
| `http_headers` | Static HTTP headers |
| `env_http_headers` | Headers from environment variables |
| `request_max_retries` | HTTP retry count (default: 4) |
| `stream_max_retries` | SSE stream retry count (default: 5) |
| `stream_idle_timeout_ms` | SSE idle timeout (default: 300000) |
| `supports_websockets` | Enable WebSocket transport |

### Azure example

```toml
[model_providers.azure]
name = "Azure"
base_url = "https://YOUR_PROJECT.openai.azure.com/openai"
env_key = "AZURE_OPENAI_API_KEY"
query_params = { api-version = "2025-04-01-preview" }
wire_api = "responses"
```

### Data residency example

```toml
model_provider = "openaidr"
[model_providers.openaidr]
name = "OpenAI Data Residency"
base_url = "https://us.api.openai.com/v1"
```

## Shell Environment Policy

Control which env vars Codex passes to subprocesses:

```toml
[shell_environment_policy]
inherit = "none"                              # all | core | none
set = { PATH = "/usr/bin", MY_FLAG = "1" }    # Explicit overrides
ignore_default_excludes = false                # Keep KEY/SECRET/TOKEN filter
exclude = ["AWS_*", "AZURE_*"]                 # Remove matching patterns
include_only = ["PATH", "HOME"]                # Whitelist (if non-empty)
experimental_use_profile = false               # Use user shell profile
```

Patterns are case-insensitive globs (`*`, `?`, `[A-Z]`).

## Observability (OTel)

### Configuration

```toml
[otel]
environment = "staging"     # Tag (default: "dev")
exporter = "none"           # none | otlp-http | otlp-grpc
trace_exporter = "none"     # none | otlp-http | otlp-grpc
metrics_exporter = "statsig" # none | statsig | otlp-http | otlp-grpc
log_user_prompt = false     # Include raw prompts (opt-in)
```

### HTTP exporter

```toml
[otel.exporter."otlp-http"]
endpoint = "https://otel.example.com/v1/logs"
protocol = "binary"          # binary | json

[otel.exporter."otlp-http".headers]
"x-otlp-api-key" = "${OTLP_TOKEN}"

[otel.exporter."otlp-http".tls]
ca-certificate = "certs/otel-ca.pem"
client-certificate = "/etc/codex/certs/client.pem"
client-private-key = "/etc/codex/certs/client-key.pem"
```

### gRPC exporter

```toml
[otel.trace_exporter."otlp-grpc"]
endpoint = "https://otel.example.com:4317"
headers = { "x-otlp-meta" = "abc123" }
```

### Emitted events

- `codex.conversation_starts` — model, reasoning, sandbox settings
- `codex.api_request` — attempt, status, duration
- `codex.sse_event` — stream event kind, success
- `codex.user_prompt` — length (content redacted unless enabled)
- `codex.tool_decision` — approved/denied, source
- `codex.tool_result` — duration, success

### OTel metrics

| Metric | Type | Fields |
|--------|------|--------|
| `codex.api_request` | counter | `status`, `success` |
| `codex.api_request.duration_ms` | histogram | `status`, `success` |
| `codex.sse_event` | counter | `kind`, `success` |
| `codex.tool.call` | counter | `tool`, `success` |
| `codex.tool.call.duration_ms` | histogram | `tool`, `success` |

## Analytics

```toml
[analytics]
enabled = false  # Disable anonymous usage data
```

## Notifications

External program invoked on events (currently `agent-turn-complete`):

```toml
notify = ["python3", "/path/to/notify.py"]
```

The script receives a single JSON argument with fields: `type`, `thread-id`, `turn-id`, `cwd`, `input-messages`, `last-assistant-message`.

## Feedback

```toml
[feedback]
enabled = false  # Disable /feedback submissions
```

## History

```toml
[history]
persistence = "save-all"    # save-all | none
max_bytes = 104857600       # 100 MiB cap
```

## TUI Options

```toml
[tui]
notifications = false                     # true | false | ["agent-turn-complete"]
notification_method = "auto"              # auto | osc9 | bel
animations = true                         # ASCII animations
show_tooltips = true                      # Welcome screen tooltips
alternate_screen = "auto"                 # auto | always | never
# status_line = ["model-with-reasoning", "context-remaining", "current-dir"]
# theme = "catppuccin-mocha"              # Syntax highlighting theme
```

## Permissions (Network Proxy)

```toml
[permissions.network]
enabled = true
proxy_url = "http://127.0.0.1:43128"
mode = "limited"                          # limited | full
allowed_domains = ["api.openai.com"]
denied_domains = ["example.com"]
enable_socks5 = false
allow_local_binding = false
```

## Apps / Connectors

```toml
[apps._default]
enabled = true
destructive_enabled = true
open_world_enabled = true

[apps.google_drive]
enabled = false
destructive_enabled = false
default_tools_approval_mode = "prompt"

[apps.google_drive.tools."files/delete"]
enabled = false
approval_mode = "approve"
```

## File Opener

Clickable citations URI scheme:

```toml
file_opener = "vscode"  # vscode | vscode-insiders | cursor | windsurf | none
```

## MCP OAuth

```toml
mcp_oauth_credentials_store = "auto"    # auto | file | keyring
mcp_oauth_callback_port = 4321          # Fixed port for OAuth callback
mcp_oauth_callback_url = "https://devbox.example.internal/callback"
```

## Authentication

```toml
cli_auth_credentials_store = "file"     # file | keyring | auto
chatgpt_base_url = "https://chatgpt.com/backend-api/"
forced_login_method = "chatgpt"         # chatgpt | api
forced_chatgpt_workspace_id = "uuid"    # Restrict to workspace
```

## Codex Home

Codex stores state under `CODEX_HOME` (defaults to `~/.codex`):

- `config.toml` — configuration
- `auth.json` — credentials (if file-based)
- `history.jsonl` — session transcripts
- `log/` — log files
- `themes/` — custom .tmTheme files
