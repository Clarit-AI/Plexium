# Assistive Prompts

Plexium keeps its assistive-agent behavior in a repo-visible prompt pack at `.plexium/prompts/`.

That prompt pack is meant to be edited. It is the contract that tells Plexium's assistive flows how to behave during:

- initial wiki population
- retrieval and context gathering
- documentation and wiki writing
- maintenance and refinement
- semantic lint passes such as contradiction, cross-reference, staleness, and missing-concept analysis

## Where the Prompt Pack Lives

When you run `plexium init` or `plexium setup <agent>`, Plexium materializes a default prompt pack into:

```text
.plexium/prompts/
  README.md
  assistive/
    initial-wiki-population.md
    retriever.md
    documenter.md
    maintenance.md
    contradiction.md
    cross-reference.md
    staleness.md
    missing-concepts.md
  profiles/
    constrained-local.md
    balanced.md
    frontier-large-context.md
```

The files are intentionally plain Markdown with frontmatter so teams can tune them without recompiling Plexium.

## Capability Profiles

Assistive providers can declare a `capabilityProfile` in `.plexium/config.yml`.

The first version keeps this simple:

- `constrained-local` for small or local models that need tighter scope
- `balanced` for normal day-to-day cloud or local models
- `frontier-large-context` for models that can read more, synthesize more, and drive a broader first pass

Plexium applies the matching profile overlay on top of the base prompt.

Typical defaults:

- Ollama -> `constrained-local`
- OpenRouter -> `balanced`

You can override those values manually in `.plexium/config.yml`.

## Initial Wiki Build Guidance

For the first substantial wiki build after install:

1. Run `plexium convert` to bootstrap grounded content.
2. Prefer Claude agent teams or Codex sub-agents when supported.
3. Split the first pass into:
   - retriever / context gatherer
   - documenter / wiki writer
   - optional validator / linter
4. Use `assistive/initial-wiki-population.md` as the top-level contract.

Single-agent wiki population is still supported, but it is the fallback path rather than the preferred one.

## What Uses These Prompts Today

Today the prompt pack is already used by Plexium's semantic lint flows, and the generated agent instructions point users and coding agents at the same repo-local files for the first wiki build and later maintenance passes.

That means the prompt pack is not just documentation. It is the editable source of truth Plexium now uses for assistive behavior.
