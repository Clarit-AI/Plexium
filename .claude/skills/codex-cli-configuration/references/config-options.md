# Common Configuration Options

Detailed reference for the most commonly changed Codex CLI configuration options.

## Model Selection

### model
Primary model Codex uses in the CLI and IDE.

```toml
model = "gpt-5.4"
```

### review_model
Optional model override used by `/review`. Defaults to current session model.

```toml
review_model = "gpt-5-pro"
```

### model_provider
Provider id from `[model_providers]` table. Default: `"openai"`.

```toml
model_provider = "openai"
```

### openai_base_url
Override base URL for the built-in `openai` provider. Use instead of defining a separate provider when pointing at a proxy or data-residency endpoint.

```toml
openai_base_url = "https://us.api.openai.com/v1"
```

### oss_provider
Default local provider for `--oss` sessions. Values: `lmstudio`, `ollama`.

```toml
oss_provider = "ollama"
```

## Approval and Sandbox

### approval_policy

Controls when Codex pauses to ask before running commands.

| Value | Behavior |
|-------|----------|
| `untrusted` | Only known-safe read-only commands auto-run; all others prompt |
| `on-request` | Model decides when to ask (default, recommended) |
| `never` | Never prompt (risky — use only in CI or fully trusted environments) |
| `{ granular = { ... } }` | Per-category allow/auto-reject |

```toml
approval_policy = "on-request"
```

Granular example:

```toml
approval_policy = { granular = {
  sandbox_approval = true,
  rules = true,
  mcp_elicitations = true,
  request_permissions = false,
  skill_approval = false
} }
```

### sandbox_mode

Adjusts filesystem and network access during command execution.

| Value | Access |
|-------|--------|
| `read-only` | Read-only filesystem, no network (default) |
| `workspace-write` | Write to workspace dirs, configurable network and writable roots |
| `danger-full-access` | No sandbox (use only if environment already isolates processes) |

```toml
sandbox_mode = "workspace-write"
```

### Workspace-write settings

```toml
[sandbox_workspace_write]
writable_roots = ["/Users/YOU/.pyenv/shims"]
network_access = false
exclude_tmpdir_env_var = false
exclude_slash_tmp = false
```

### allow_login_shell
Default: `true`. Set `false` to reject login-shell semantics in shell tools.

```toml
allow_login_shell = false
```

## Web Search

| Value | Behavior |
|-------|----------|
| `"cached"` | Results from OpenAI-maintained index (default, safer) |
| `"live"` | Fetch most recent data from web (same as `--search`) |
| `"disabled"` | Turn off web search tool |

```toml
web_search = "cached"
```

When using `--yolo` or full-access sandbox, web search defaults to `"live"`.

## Reasoning and Verbosity

### model_reasoning_effort
Values: `minimal`, `low`, `medium`, `high`, `xhigh` (xhigh is model-dependent).

```toml
model_reasoning_effort = "high"
```

### plan_mode_reasoning_effort
Override for plan mode specifically. Values: `none`, `minimal`, `low`, `medium`, `high`, `xhigh`.

```toml
plan_mode_reasoning_effort = "high"
```

### model_reasoning_summary
Values: `auto`, `concise`, `detailed`, `none`.

```toml
model_reasoning_summary = "none"
```

### model_verbosity
Values: `low`, `medium`, `high`. Applies only to Responses API providers.

```toml
model_verbosity = "low"
```

### model_supports_reasoning_summaries
Force enable/disable reasoning metadata.

```toml
model_supports_reasoning_summaries = true
```

### model_context_window
Manual override for context window tokens. When unset, uses model defaults.

```toml
model_context_window = 128000
```

## Communication Style

### personality
Values: `none`, `friendly`, `pragmatic`. Can be overridden per session with `/personality`.

```toml
personality = "friendly"
```

## Windows Sandbox

```toml
[windows]
sandbox = "elevated"    # Recommended
# sandbox = "unelevated" # Fallback if admin unavailable
```

## Project Discovery

### project_root_markers
Directories containing any listed marker are treated as project roots. Default: `[".git"]`.

```toml
project_root_markers = [".git", ".hg", ".sl"]
```

Set to `[]` to treat CWD as project root.

### project_doc_max_bytes
Max bytes read from each `AGENTS.md`. Default: `32768`.

```toml
project_doc_max_bytes = 32768
```

### project_doc_fallback_filenames
Additional filenames to try when `AGENTS.md` is missing at a directory level.

```toml
project_doc_fallback_filenames = ["CONTRIBUTING.md", "DEVELOPMENT.md"]
```

## Reasoning Display

```toml
hide_agent_reasoning = true       # Suppress reasoning events
show_raw_agent_reasoning = true   # Show raw reasoning content
```
