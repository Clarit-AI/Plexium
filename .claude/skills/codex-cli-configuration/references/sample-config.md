# Sample Configuration

Complete annotated config.toml showing all sections. Copy only the sections needed.

```toml
#:schema https://developers.openai.com/codex/config-schema.json

# ── Core Model ──────────────────────────────────────────
model = "gpt-5.4"
model_provider = "openai"
approval_policy = "on-request"
sandbox_mode = "read-only"
web_search = "cached"
personality = "pragmatic"
file_opener = "vscode"

# ── Reasoning ───────────────────────────────────────────
# model_reasoning_effort = "medium"
# plan_mode_reasoning_effort = "high"
# model_reasoning_summary = "auto"
# model_verbosity = "medium"

# ── Project Discovery ───────────────────────────────────
project_doc_max_bytes = 32768
# project_root_markers = [".git"]
# project_doc_fallback_filenames = ["CONTRIBUTING.md"]

# ── Sandbox Settings ────────────────────────────────────
[sandbox_workspace_write]
writable_roots = []
network_access = false
exclude_tmpdir_env_var = false
exclude_slash_tmp = false

# ── Shell Environment Policy ────────────────────────────
[shell_environment_policy]
inherit = "all"
ignore_default_excludes = false
exclude = []
include_only = []
set = {}

# ── History ─────────────────────────────────────────────
[history]
persistence = "save-all"
# max_bytes = 5242880

# ── TUI ─────────────────────────────────────────────────
[tui]
notifications = false
animations = true
show_tooltips = true

# ── Analytics & Feedback ────────────────────────────────
[analytics]
enabled = true

[feedback]
enabled = true

# ── Features ────────────────────────────────────────────
[features]
# codex_hooks = false
# multi_agent = true
# undo = false
# smart_approvals = false

# ── MCP Servers ─────────────────────────────────────────
[mcp_servers]

# [mcp_servers.docs]
# enabled = true
# command = "docs-server"
# args = ["--port", "4000"]
# startup_timeout_sec = 10
# tool_timeout_sec = 60

# [mcp_servers.github]
# enabled = true
# url = "https://github-mcp.example.com/mcp"
# bearer_token_env_var = "GITHUB_TOKEN"

# ── Model Providers ─────────────────────────────────────
[model_providers]

# [model_providers.ollama]
# name = "Ollama"
# base_url = "http://localhost:11434/v1"

# [model_providers.azure]
# name = "Azure"
# base_url = "https://PROJECT.openai.azure.com/openai"
# env_key = "AZURE_OPENAI_API_KEY"
# query_params = { api-version = "2025-04-01-preview" }

# ── Profiles ────────────────────────────────────────────
[profiles]

# [profiles.deep-review]
# model = "gpt-5-pro"
# model_reasoning_effort = "high"
# approval_policy = "never"

# ── Agent Roles ─────────────────────────────────────────
[agents]
# max_threads = 6
# max_depth = 1

# [agents.reviewer]
# description = "Review code for correctness and security."
# config_file = "./agents/reviewer.toml"
# nickname_candidates = ["Athena", "Ada"]

# ── Apps / Connectors ───────────────────────────────────
[apps]
# [apps._default]
# enabled = true
# destructive_enabled = true

# ── Projects ────────────────────────────────────────────
[projects]
# [projects."/path/to/project"]
# trust_level = "trusted"

# ── OTel (disabled by default) ──────────────────────────
[otel]
environment = "dev"
exporter = "none"
trace_exporter = "none"
metrics_exporter = "statsig"
log_user_prompt = false

# ── Windows ─────────────────────────────────────────────
[windows]
# sandbox = "elevated"

# ── Skills Overrides ────────────────────────────────────
# [[skills.config]]
# path = "/path/to/skill/SKILL.md"
# enabled = false
```

## TOML Schema Autocompletion

For VS Code/Cursor, install the [Even Better TOML](https://marketplace.visualstudio.com/items?itemName=tamasfe.even-better-toml) extension and add to the top of `config.toml`:

```toml
#:schema https://developers.openai.com/codex/config-schema.json
```
