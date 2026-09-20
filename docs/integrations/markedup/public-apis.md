# MarkedUp Public APIs Plexium Consumes

Plexium imports five MarkedUp sub-packages and calls a documented subset of
their public API. This file enumerates those calls with `file:line` references
so a future bump can be inspected mechanically: any call site that breaks or
changes signature is a candidate for a Plexium-side fix or a contract-test
tightening.

The list is exhaustive as of the KHA-574 gate. Any new MarkedUp import added in
later PRs must extend this list in the same commit.

---

## `github.com/Clarit-AI/markedup/enrich`

Tier 1 / Tier 2 enrichment merge logic. Plexium calls two top-level functions
plus their `MergeOptions` parameter type and the `ModelResult` /
`EnrichmentDelta` value types they return.

| API                                | Used at                | Purpose                                                       |
| ---------------------------------- | ---------------------- | ------------------------------------------------------------- |
| `enrich.EnrichPage(parsed, filePath, rootDir, opts)` | `internal/plugins/markedup/enricher.go:234` | Tier 1 deterministic enrichment; populates `enriched.Frontmatter`. |
| `enrich.EnrichPageWithModel(enriched, model, summary, opts)` | `internal/plugins/markedup/enricher.go:258` | Tier 2 model-based enrichment; merges LLM-derived entities on top of Tier 1. |
| `enrich.MergeOptions` (zero value) | `internal/plugins/markedup/enricher.go:234, 258` | Passed as `enrich.MergeOptions{}` — no overrides.             |
| `enrich.ModelResult`               | `internal/plugins/markedup/enricher.go:41` (`LLMProvider.ExtractGraph` signature); also used by `internal/plugins/markedup/llm_adapter.go`. | Returned by the assistive cascade adapter. |
| `enrich.EnrichmentDelta`           | `internal/plugins/markedup/enricher.go:230` | Read for the `.Changed` boolean to decide whether to apply graph metadata to the manifest. |

**Contract guarantee:** `EnrichPage` MUST populate
`schema.GraphFrontmatter.Relationships` (wikilink-derived doc-to-doc edges) and
leave `schema.GraphFrontmatter.SemanticRelationships` (NER-derived
free-text-target edges) in a **separate** field. The two MUST NOT be collapsed
into one — see
[`enricher_contract_test.go`](../../../internal/plugins/markedup/enricher_contract_test.go)
for the regression test.

---

## `github.com/Clarit-AI/markedup/markdown`

Frontmatter parse/serialize round-trip.

| API                                          | Used at                | Purpose                                                       |
| -------------------------------------------- | ---------------------- | ------------------------------------------------------------- |
| `markdown.ParseBytesPermissive(raw)`         | `internal/plugins/markedup/enricher.go:223` | Parses the page body including the existing frontmatter; tolerates missing frontmatter. |
| `markdown.ReplaceFrontmatter(&fm, raw)`      | `internal/plugins/markedup/enricher.go:315` | Re-renders the file with the merged frontmatter (used only when `WriteEnrichedFrontmatter` is true). |
| `markdown.WriteFrontmatterFile(path, bytes)` | `internal/plugins/markedup/enricher.go:334` | Atomic file write. |

**Contract guarantee:** `ParseBytesPermissive` MUST preserve the
`Summary` field on `schema.GraphFrontmatter` when frontmatter is present, and
MUST NOT lose or rewrite the `ID` field. See
[`enricher_contract_test.go`](../../../internal/plugins/markedup/enricher_contract_test.go).

---

## `github.com/Clarit-AI/markedup/schema`

Core data model. Plexium reads field-by-field; it does not depend on the YAML
serialization tags directly.

| API                            | Used at                                       | Purpose                                                       |
| ------------------------------ | --------------------------------------------- | ------------------------------------------------------------- |
| `schema.Page`                  | `internal/plugins/markedup/enricher.go:229`   | In-memory page (frontmatter + body + source path).            |
| `schema.GraphFrontmatter`      | `internal/plugins/markedup/enricher.go:375`   | The merged frontmatter passed to `toGraphMetadata`.           |
| `schema.Entity`                | (transitively, via `schema.GraphFrontmatter.Entities`) | Read in `toGraphMetadata` (enricher.go:377–379) to populate `manifest.EntityRef`. |
| `schema.Relationship`          | (transitively, via `schema.GraphFrontmatter.Relationships` and `.SemanticRelationships`) | Read in `toGraphMetadata` (enricher.go:381–397) to populate both manifest relationship slices. |
| `schema.TemporalInfo`          | (transitively)                                | Read indirectly through `GraphFrontmatter`. **Not** persisted into the manifest in the current contract. |
| `schema.Provenance`            | (transitively)                                | Read indirectly through `GraphFrontmatter`. **Not** persisted into the manifest in the current contract. |
| `schema.ValidationError`       | (transitively, via `index.LoadWarning.Errors`) | Surfaced in `Load` warnings; not currently surfaced to the manifest. |

**Contract guarantee:** `schema.GraphFrontmatter.Relationships` and
`schema.GraphFrontmatter.SemanticRelationships` are two distinct `[]Relationship`
fields with identical element types. A MarkedUp change that merges them into a
single field (with a discriminator) would silently break
`manifest.GraphMetadataSemanticEqual` and lose Tier 2 semantic edges on the next
enrichment pass. The contract test in
[`enricher_contract_test.go`](../../../internal/plugins/markedup/enricher_contract_test.go)
fails if the merge happens.

---

## `github.com/Clarit-AI/markedup/index`

The retrieval-side knowledge index. Plexium builds and queries a
`*index.KnowledgeIndex` from a wiki root.

| API                                             | Used at                | Purpose                                                       |
| ----------------------------------------------- | ---------------------- | ------------------------------------------------------------- |
| `index.Load(ctx, root, opts...)`                | `internal/plugins/markedup/retrieval.go:75` | Builds the index from disk. Plexium always passes `WithIgnoreErrors(true)` and `WithAutoEnrich(false)`. |
| `index.WithIgnoreErrors(true)`                  | `internal/plugins/markedup/retrieval.go:76` | Tolerates per-page parse warnings instead of failing the whole load. |
| `index.WithAutoEnrich(false)`                   | `internal/plugins/markedup/retrieval.go:77` | Prevents MarkedUp from auto-enriching pages on load — Plexium owns enrichment via the `EnricherPlugin` pipeline. |
| `index.KnowledgeIndex` (returned by `Load`)     | `internal/plugins/markedup/retrieval.go:29, 82` | Held behind an `atomic.Pointer` for concurrent MCP access. |
| `index.Search(idx, query, opts...)`             | `internal/plugins/markedup/retrieval.go:112` | Keyword + (optional) semantic search. |
| `index.WithContext(ctx)`                        | `internal/plugins/markedup/retrieval.go:94` | Cancellation. |
| `index.WithLimit(n)`                            | `internal/plugins/markedup/retrieval.go:96` | Result cap. |
| `index.WithMinScore(s)`                         | `internal/plugins/markedup/retrieval.go:99` | Threshold. |
| `index.WithEmbedder(p.embedder)`                | `internal/plugins/markedup/retrieval.go:107` | Activates semantic scoring when both embedder and cache are wired. |
| `index.WithVectorCache(p.vectorCache)`          | `internal/plugins/markedup/retrieval.go:108` | Same — required alongside `WithEmbedder`. |
| `index.Traverse(idx, id, opts...)`              | `internal/plugins/markedup/retrieval.go:213` | Graph traversal for the `markedup_traverse` MCP tool. |
| `index.WithDepth(n)`                            | `internal/plugins/markedup/retrieval.go:211` | Traversal depth bound. |
| `(*KnowledgeIndex).CompactGraphSummary(opts...)` | `internal/plugins/markedup/retrieval.go:238` | Compact LLM-friendly summary for the `markedup_graph` MCP tool. |
| `index.SummaryOption` + `index.WithEntityTypeFilter(s)` | `internal/plugins/markedup/retrieval.go:228, 230` | Graph summary filter. |
| `index.WithTagFilter(s)`                        | `internal/plugins/markedup/retrieval.go:233` | Graph summary filter. |
| `index.WithMaxPages(n)`                         | `internal/plugins/markedup/retrieval.go:236` | Graph summary cap. |
| `index.VectorCacheLookup` (interface)           | `internal/plugins/markedup/retrieval.go:31`  | The vector-cache contract Plexium's `.plexium/knowledge/` cache satisfies. |

**Contract guarantee:** `(*KnowledgeIndex).Get(id)` and
`(*KnowledgeIndex).All()` MUST return one page per unique `ID` — no duplicates
and no blank IDs. See
[`retrieval_contract_test.go`](../../../internal/plugins/markedup/retrieval_contract_test.go)
(`TestRetrieval_NoBlankOrDuplicateIDs`).

**Known contract gap:** `index.Load` does NOT support excluding directories
such as `raw/`. The built-in `pageindex` indexer at
`internal/integrations/pageindex/index.go:50` skips `raw/`, but the MarkedUp
retrieval plugin does not. This is the F4 audit finding. The contract test
`TestRetrieval_RawDirectoryExcluded` documents the expected behavior and fails
against the current implementation; see
[`upgrade-procedure.md`](upgrade-procedure.md#known-issues).

---

## `github.com/Clarit-AI/markedup/embed`

Embedder interface contract.

| API               | Used at                | Purpose                                                       |
| ----------------- | ---------------------- | ------------------------------------------------------------- |
| `embed.Embedder`  | `internal/plugins/markedup/retrieval.go:30` | The interface Plexium's assistive cascade satisfies (via `internal/plugins/markedup/retrieval.go` construction). |

The concrete `embed.NewFromProvider(...)` is called from
`internal/plugins/bootstrap/bootstrap.go:227` — not from the markedup plugin
itself. The contract guarantee on this side is that
`Embedder.Embed(ctx, texts) [][]float32` returns one vector per input text and
`Dimensions()` matches the configured `EmbeddingsConfig.Dims`.

---

## `github.com/Clarit-AI/markedup/cache`

Vector cache persistence.

| API                                  | Used at                                    | Purpose                                                       |
| ------------------------------------ | ------------------------------------------ | ------------------------------------------------------------- |
| `cache.NewVectorCache(dir)`          | `internal/plugins/bootstrap/bootstrap.go:195` | Creates a Plexium-side cache rooted at `.plexium/knowledge/`. The returned object satisfies `index.VectorCacheLookup`. |

This is the only MarkedUp-side bootstrap call; everything else goes through the
plugin surface above.

---

## Imports summary

```sh
$ grep -rn "Clarit-AI/markedup" internal/plugins/ cmd/ | grep -v "_test.go"
internal/plugins/markedup/enricher.go:12:    "github.com/Clarit-AI/markedup/enrich"
internal/plugins/markedup/enricher.go:13:    "github.com/Clarit-AI/markedup/markdown"
internal/plugins/markedup/enricher.go:14:    "github.com/Clarit-AI/markedup/schema"
internal/plugins/markedup/llm_adapter.go:11:  "github.com/Clarit-AI/markedup/enrich"
internal/plugins/markedup/retrieval.go:10:    "github.com/Clarit-AI/markedup/embed"
internal/plugins/markedup/retrieval.go:11:    "github.com/Clarit-AI/markedup/index"
internal/plugins/bootstrap/bootstrap.go:24:   mucache "github.com/Clarit-AI/markedup/cache"
internal/plugins/bootstrap/bootstrap.go:25:   muembed "github.com/Clarit-AI/markedup/embed"
```

Six import sites in production code. Test files add
`github.com/Clarit-AI/markedup/{enrich,schema}` for `enricher_modelenrich_test.go`
and use markedup's test fixtures elsewhere.

If a new import site is added in a future PR, this file MUST be updated in
the same commit.
