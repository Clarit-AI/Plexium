# Getting Started

This guide takes you from zero to a working Plexium wiki. The fastest path is now `plexium setup <agent>`, which prepares the repo, installs the right instruction file, materializes the editable prompt pack, and tells you whether any native MCP step is still outstanding.

Plexium is a per-repository system. Install the `plexium` binary once, then run `plexium init` or `plexium setup <agent>` inside each repository you want Plexium to manage.

---

## Prerequisites

- **Go 1.25+** (for building from source)
- **Git** (Plexium operates on git repositories)
- Optional: [lefthook](https://github.com/evilmartians/lefthook) for git hook management
- Optional: [Obsidian](https://obsidian.md) for viewing the wiki as a local vault

---

## Install

### From source

```bash
git clone https://github.com/Clarit-AI/Plexium.git
cd Plexium
go build -o plexium ./cmd/plexium
```

Move the binary to your PATH:

```bash
install -m 0755 plexium /usr/local/bin/plexium
```

### Via `go install`

```bash
go install github.com/Clarit-AI/Plexium/cmd/plexium@latest
```

### Verify

```bash
plexium --version
# plexium version 0.1.0
```

---

## Fastest Path

If you already know which agent you want to use, this is the canonical onboarding flow:

```bash
cd /path/to/your/repo
plexium setup claude
# or
plexium setup codex
```

Add `--write-config` to let Plexium run the native MCP configuration command for you:

```bash
plexium setup claude --write-config
plexium setup codex --write-config
```

If you also want repo-local session provenance, add `--with-memento`. Plexium will offer to install `git-memento` first if it is missing:

```bash
plexium setup claude --write-config --with-memento
plexium setup codex --write-config --with-memento
```

On Claude and Codex, Plexium also writes a temporary repo-local compatibility shim into the local `git-memento` config so users can keep using Memento until upstream provider support is patched:

- Claude: `.plexium/bin/claude-memento-bridge.cjs`
- Codex: `.plexium/bin/codex-memento-bridge.cjs`

After setup, verify readiness explicitly:

```bash
plexium verify claude
plexium verify codex
```

Then do the real first-pass population work:

```bash
plexium convert
plexium retrieve "what does this project do?"
```

Setup means the tooling is wired. It does not mean the wiki is already rich. The scaffold is intentionally minimal until `convert` and an agent-driven first pass fill it in.

If no assistive provider is configured yet, `plexium setup <agent>` now offers three paths:

- configure Ollama now
- configure OpenRouter now
- skip for now and use `plexium convert` plus your coding agent

For the initial bulk population pass, prefer Claude agent teams or Codex sub-agents when your primary coding agent supports them.

---

## Initialize a Wiki

Navigate to the repository where you want a wiki and run:

```bash
cd /path/to/your/repo
plexium init
```

Plexium creates two directory trees:

```
.wiki/                      # The wiki vault
  Home.md                   # Landing page (generated from README if present)
  _Sidebar.md               # Navigation sidebar
  _Footer.md                # Page footer
  _log.md                   # Change log
  _index.md                 # Master page index
  _schema.md                # Agent governance schema
  architecture/overview.md  # Starter architecture page
  modules/                  # Module documentation
  decisions/                # Architecture Decision Records
  patterns/                 # Design patterns
  concepts/                 # Domain concepts
  guides/                   # How-to guides
  raw/                      # Unprocessed source material
  onboarding.md             # Onboarding guide stub
  contradictions.md         # Tracked contradictions
  open-questions.md         # Unresolved questions

.plexium/                   # Plexium state
  config.yml                # Configuration
  manifest.json             # Source-to-wiki mapping
  plugins/                  # Agent adapter plugins
  hooks/                    # Git hook scripts
  migrations/               # Schema migrations
```

### With integrations

```bash
# Enable Obsidian vault configuration
plexium init --obsidian

# Enable all integrations
plexium init --with-memento --with-beads --with-pageindex

# Set strict enforcement from the start
plexium init --strictness strict
```

**A note on `--with-pageindex`:** This flag enables the PageIndex integration in config and writes a reference file at `.plexium/pageindex-mcp.json`. Agents do not read that file automatically. After init, prefer `plexium setup claude` or `plexium setup codex`, which installs the agent adapter, compiles navigation, and prints or applies the native MCP command for you.

The CLI retrieval command (`plexium retrieve`) works regardless of whether `--with-pageindex` was passed. The flag enables the PageIndex integration in config but the built-in search engine is always available.

**A note on `--with-memento`:** This flag is also per-repository. If `git-memento` is already installed, Plexium initializes it for the current repo. If it is missing, Plexium can offer to download the pinned release asset and install the `git-memento` binary before running `git memento init`. On Claude and Codex, Plexium additionally configures the temporary compatibility shim automatically.

### Preview first

```bash
plexium init --dry-run
```

Dry-run shows what files and directories would be created without writing anything.

---

## Verify the Setup

```bash
plexium doctor
plexium verify claude
# or
plexium verify codex
```

`plexium doctor` validates the general Plexium install. `plexium verify <agent>` adds agent-specific checks for the compiled navigation files, generated instruction file, PageIndex reference, deterministic lint status, and MCP configuration state.

The generated `CLAUDE.md` and `AGENTS.md` files now also point to `.plexium/prompts/assistive/initial-wiki-population.md` and the role prompts in `.plexium/prompts/assistive/` so the first wiki build follows a consistent, editable contract.

For a direct MCP-only path without the rest of setup, use:

```bash
plexium pageindex connect claude
plexium pageindex connect codex
```

If any checks fail, see [Troubleshooting: Doctor Reports Failures](troubleshooting.md#doctor-reports-failures).

---

## Run Your First Lint

```bash
plexium lint --deterministic
```

This runs six structural checks on the wiki: link validation, orphan detection, staleness detection, manifest validation, sidebar validation, and frontmatter validation.

On a fresh scaffold, deterministic lint should pass cleanly. If it does not, treat that as a real setup problem rather than an expected first-run warning.

---

## Compile Navigation

```bash
plexium compile
```

Compile reads the manifest and generates two navigation files:

- **`_index.md`**: a master list of all wiki pages grouped by section
- **`_Sidebar.md`**: a collapsible sidebar for GitHub Wiki or Obsidian

Compile is deterministic: the same manifest state always produces identical output.

---

## What Happened

Plexium operates on three layers:

1. **Source layer** (your code, docs, READMEs): immutable. Plexium reads from this layer but never modifies it.
2. **State manifest** (`.plexium/manifest.json`): tracks bidirectional mappings between source files and wiki pages, content hashes for staleness detection, and ownership metadata.
3. **Wiki layer** (`.wiki/`): the synthesized knowledge layer. Agents read the wiki before working and update it after every change.

When you run `plexium init`, all three layers are bootstrapped. The manifest starts empty. As you run `sync`, `convert`, or agents update pages, the manifest grows to track every source-to-wiki relationship.

---

## Optional: Brownfield Conversion

If your repository already has source code and you want to bootstrap wiki content from it:

```bash
# Preview what convert would generate
plexium convert --dry-run

# Run the conversion
plexium convert

# For deeper analysis (slower, richer output)
plexium convert --depth deep
```

Convert generates pages by analyzing source file structure, names, and patterns. Results vary by codebase. Review generated pages and edit as needed.

After conversion, compile navigation:

```bash
plexium compile
```

---

## TUI-Native Installs

### Claude Code

Plexium ships a GitHub-backed Claude marketplace entry:

```text
/plugin marketplace add Clarit-AI/Plexium
/plugin install plexium-tools@clarit-ai
```

The plugin provides:

- `/plexium-install`
- `/plexium-setup`
- `/plexium-setup-auto`
- `/plexium-verify`
- `/plexium-retrieve`
- `/plexium-connect`

### Codex

Codex does not yet have the same self-serve remote marketplace path in this repo that Claude does. Today, Plexium ships a repo-local Codex marketplace entry via `.agents/plugins/marketplace.json`, which is useful for repo teams and local testing. Once Codex self-serve remote publishing is available, this should move to the official remote Plugin Directory flow.

---

## Optional: Enable Git Hooks

Install [lefthook](https://github.com/evilmartians/lefthook) and add hooks to your `lefthook.yml`:

```yaml
pre-commit:
  commands:
    plexium-check:
      run: plexium hook pre-commit

post-commit:
  commands:
    plexium-debt:
      run: plexium hook post-commit
```

Then install:

```bash
lefthook install
```

The pre-commit hook checks whether wiki updates accompany source changes. The post-commit hook tracks documentation debt when commits bypass the hook. See [User Guide: The Agent Workflow](user-guide.md#the-agent-workflow) for details.

---

## Optional: Obsidian Integration

If you initialized with `--obsidian`, the `.wiki/` directory includes an `.obsidian/` configuration folder. Open `.wiki/` as a vault in Obsidian to browse the wiki with backlinks, graph view, and dataview queries.

If you initialized without `--obsidian` and want to add it later, re-run init:

```bash
plexium init --obsidian
```

Init is non-destructive: it skips files that already exist and only creates the Obsidian configuration.

---

## Next Steps

- [User Guide](user-guide.md): workflows for greenfield, brownfield, and incremental wiki maintenance
- [CLI Reference](cli-reference.md): every command, flag, and environment variable
- [Status](status.md): what is stable, experimental, and planned
