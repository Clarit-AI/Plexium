# Getting Started

This guide takes you from zero to a working Plexium wiki. You will initialize a wiki, validate the setup, run your first lint, and generate navigation files.

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
mv plexium /usr/local/bin/
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

### Preview first

```bash
plexium init --dry-run
```

Dry-run shows what files and directories would be created without writing anything.

---

## Verify the Setup

```bash
plexium doctor
```

Doctor validates that config parses, required directories exist, the manifest loads, and integrations are configured. Each check reports PASS, FAIL, WARN, or SKIP.

If any checks fail, see [Troubleshooting: Doctor Reports Failures](troubleshooting.md#doctor-reports-failures).

---

## Run Your First Lint

```bash
plexium lint --deterministic
```

This runs six structural checks on the wiki: link validation, orphan detection, staleness detection, manifest validation, sidebar validation, and frontmatter validation.

**Expected on a fresh wiki:** You will see two types of findings:

1. **Broken links in `_schema.md`**: The schema contains `[[wiki-links]]` as syntax examples. The link crawler correctly flags these as broken. This is a known false positive, safe to ignore. See [Troubleshooting](troubleshooting.md#lint-reports-broken-links-in-_schemamd).

2. **Missing frontmatter fields**: Scaffolded pages have minimal frontmatter. Agents fill in the remaining fields as they work. See [Troubleshooting](troubleshooting.md#freshly-initialized-pages-fail-frontmatter-lint).

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
