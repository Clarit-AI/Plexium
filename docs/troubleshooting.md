# Troubleshooting

Common issues and how to resolve them.

---

## Pre-commit Hook Blocks Your Commit

**Symptom:** `git commit` fails with a message about wiki not being updated.

**Cause:** The pre-commit hook detected that source files changed but the wiki was not updated to reflect those changes.

**Fix depends on your strictness level** (set in `.plexium/config.yml` under `enforcement.strictness`):

- **strict**: the commit is blocked. Run `plexium sync` to update the wiki, then commit again.
- **moderate**: the commit is allowed with a warning. The wiki update can happen later.
- **advisory**: a notice is logged, no blocking.

**To bypass once:**

```bash
git commit --no-verify -m "your message"
```

The post-commit hook records this as WIKI-DEBT. Run `plexium sync` later to clear the debt.

**To change the default strictness:**

```yaml
# .plexium/config.yml
enforcement:
  strictness: moderate  # strict | moderate | advisory
```

---

## Lint Reports Broken Links in `_schema.md`

**Symptom:** `plexium lint` reports broken `[[wiki-links]]` in `_schema.md`.

**Cause:** The generated schema contains `[[wiki-links]]` as documentation examples of Plexium's link syntax. The link crawler correctly identifies these as broken because no page named `wiki-links.md` exists. This is a known false positive (see [Known Limitations](status.md#known-limitations)).

**Fix:** These findings are safe to ignore. The `_schema.md` file is a reference document for agents, not a content page. The example links demonstrate syntax patterns, not actual cross-references.

---

## Freshly Initialized Pages Fail Frontmatter Lint

**Symptom:** After `plexium init`, running `plexium lint` reports missing frontmatter fields on scaffolded pages.

**Cause:** Scaffolded pages have minimal frontmatter (title, ownership, last-updated) but the schema prescribes additional fields (updated-by, related-modules, source-files, confidence, review-status, tags).

**Fix:** This is expected behavior. Agents fill in full frontmatter as they work on pages. The lint findings identify pages that have not yet been enriched by an agent. No action needed unless you want to manually add the fields.

---

## Doctor Reports Failures

**Symptom:** `plexium doctor` shows FAIL or WARN entries.

**Common failures and fixes:**

| Check | Failure | Fix |
|-------|---------|-----|
| Config file | Not found | Run `plexium init` to create `.plexium/config.yml` |
| Config parse | Invalid YAML | Check `.plexium/config.yml` syntax |
| Wiki root | Directory missing | Run `plexium init` or create `.wiki/` manually |
| Manifest | Cannot load | Run `plexium init` to create `.plexium/manifest.json` |
| Schema | Not found | Run `plexium init` to generate `_schema.md` |

Run `plexium doctor` after each fix to verify.

---

## Convert Produces Low-Quality Pages

**Symptom:** `plexium convert` generates pages with minimal or irrelevant content.

**Cause:** Convert uses heuristic-based content extraction. It analyzes file structure, names, and patterns to generate wiki pages. Results vary by codebase.

**Fixes:**

1. **Try deep scan:** `plexium convert --depth deep` performs AST-level analysis and produces richer pages.
2. **Preview first:** `plexium convert --dry-run` shows what would be generated without writing files.
3. **Edit after conversion:** Generated pages are a starting point. Edit them to add context, fix inaccuracies, and improve organization.
4. **Configure source filters:** Adjust `sources.include` and `sources.exclude` in config to focus conversion on the most relevant files.

---

## Wiki Out of Sync After `--no-verify` Commits

**Symptom:** Source files and wiki pages are out of sync. Lint reports stale pages.

**Cause:** Commits made with `--no-verify` bypass the pre-commit hook. The post-commit hook tracks these as WIKI-DEBT, but the wiki is not automatically updated.

**Fix:**

```bash
# Check what is stale
plexium lint --deterministic

# Update the wiki
plexium sync

# Recompile navigation
plexium compile
```

---

## Plugin Installation Fails

**Symptom:** `plexium plugin add <name>` fails with an error.

**Common causes:**

| Error | Cause | Fix |
|-------|-------|-----|
| `plugin not found` | The adapter name is unknown and no custom path was provided | Run `plexium plugin list` to see bundled adapters, or pass `--path` for a custom adapter |
| `reading plugin manifest` | Missing or malformed `manifest.json` | Create a valid `manifest.json` with `name`, `version`, `description`, and `instructionFile` fields |
| `making plugin executable` | Permission denied | Check file system permissions on the plugin directory |
| `running plugin` | `plugin.sh` execution error | Run `bash .plexium/plugins/<name>/plugin.sh` manually to see the error output |

**Plugin directory structure:**

```
.plexium/plugins/<name>/
  manifest.json
  plugin.sh
  <instruction-file>
```

---

## Agent Test Fails for All Providers

**Symptom:** `plexium agent test` reports failures for every provider.

**Common causes:**

1. **No providers configured.** Add providers to `.plexium/config.yml`:

```yaml
assistiveAgent:
  enabled: true
  providers:
    - name: ollama-local
      enabled: true
      type: ollama
      endpoint: http://localhost:11434
      model: llama3.2
```

2. **API key not set.** For OpenAI-compatible providers, set the environment variable specified in `apiKeyEnv`:

```yaml
providers:
  - name: openrouter
    type: openai-compatible
    endpoint: https://openrouter.ai/api/v1
    apiKeyEnv: OPENROUTER_API_KEY
```

Then export the key: `export OPENROUTER_API_KEY=your-key`

3. **Ollama not running.** Start Ollama: `ollama serve`

4. **Test a single provider** to isolate the failure: `plexium agent test --provider ollama-local`

---

## Manifest Is Corrupted or Missing

**Symptom:** Commands fail with manifest-related errors.

**Fix:** The manifest is a JSON file at `.plexium/manifest.json`.

If missing, run `plexium init` to create a fresh one. Init is non-destructive and skips files that already exist.

If corrupted (invalid JSON), restore from git:

```bash
git checkout -- .plexium/manifest.json
```

If the manifest has diverged from the actual wiki state, run:

```bash
plexium sync
plexium compile
```

---

## Publish Fails with Authentication Error

**Symptom:** `plexium publish` fails when pushing to the GitHub Wiki remote.

**Cause:** Git authentication is not configured for the wiki repository.

**Fix:**

1. Verify the remote URL resolves: `git remote -v`
2. For HTTPS remotes, configure a credential helper or use a personal access token
3. For SSH remotes, verify your SSH key is added to the agent: `ssh-add -l`
4. Test access: `git ls-remote <your-repo>.wiki.git`

---

## Compile Produces Empty Navigation

**Symptom:** `plexium compile` generates empty or minimal `_index.md` and `_Sidebar.md`.

**Cause:** The manifest has no pages, or all pages lack section assignments.

**Fix:**

1. Check manifest state: `cat .plexium/manifest.json | python3 -m json.tool | grep -c wikiPath`
2. If the manifest is empty, run `plexium init` or `plexium convert` first
3. If pages exist but sections are missing, verify that `taxonomy.sections` in config matches the section values assigned to pages
