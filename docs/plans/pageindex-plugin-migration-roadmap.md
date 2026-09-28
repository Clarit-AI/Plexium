# PageIndex Plugin Migration Assessment

Date: 2026-05-31

Scope: read-only investigation plus this report. No implementation changes were made.

Upstream references inspected:

- `VectifyAI/PageIndex` shallow clone at commit `dd064dc39abc61e8aa741032e0c64e31f248f7d8`
- `VectifyAI/pageindex-mcp` shallow clone at commit `f6e9ac7e8abfcd8fa30e7128632b6192573207d4`

## Executive Summary

Migrating Plexium from its built-in PageIndex-like implementation to a full PageIndex implementation is medium to high complexity, depending on what "full" means.

The low-risk path is not to replace everything at once. Plexium can first add a compiled-in retrieval plugin that provides PageIndex-style tree tools while leaving the existing `pageindex_*` MCP tools and `plexium retrieve` behavior intact. That would prove the plugin seam, expose richer retrieval to agents, and avoid breaking current MCP clients.

The higher-risk path is a full replacement where the PageIndex implementation owns the `pageindex_*` tool names, the CLI retrieve command, and install/enablement through `plexium plugin add`. That requires core refactoring because Plexium's plugin system is currently a hybrid: Go runtime plugins are compiled in, while the CLI plugin installer is adapter/script oriented.

## Current Plexium State

Plexium's built-in implementation is a flat wiki markdown index, not a PageIndex document tree engine.

Important files:

- `internal/integrations/pageindex/index.go`
- `internal/integrations/pageindex/retrieve.go`
- `internal/integrations/pageindex/server.go`
- `cmd/plexium/main.go`
- `cmd/plexium/pageindex_connect.go`
- `internal/plugins/interfaces.go`
- `internal/plugins/bootstrap/bootstrap.go`
- `internal/plugins/markedup/retrieval.go`

### Built-In Index Behavior

`internal/integrations/pageindex/index.go` defines `PageInfo`, `PageContent`, `SearchResult`, and `PageIndex`.

The index loads markdown files from the configured wiki root, skips hidden paths and `raw/`, parses frontmatter, extracts the page title, first paragraph summary, section name from the first path segment, and `[[wiki-links]]`.

Search is weighted lexical matching:

- title match: 1.0
- section match: 0.8
- summary match: 0.6
- link match: 0.2

Results are sorted, capped at 20, and normalized.

This is useful and deterministic, but it is not PageIndex's core model. There is no persisted tree, no PDF or markdown document workspace, no node ids, no node summaries, no page-range navigation, and no LLM-guided tree search.

### CLI Surface

Current CLI retrieval:

- `plexium retrieve "<query>" [--format markdown|json]`
- Implemented in `cmd/plexium/main.go`
- Calls `pageindex.NewRetriever(...)` directly, bypassing the plugin registry

Current MCP management:

- `plexium pageindex serve`
- `plexium pageindex connect claude`
- `plexium pageindex connect codex`

`pageindex serve` builds the runtime plugin registry and initializes retrieval plugins, then starts the built-in Go MCP server.

### Built-In MCP Tools

`internal/integrations/pageindex/server.go` hardcodes these tools:

- `pageindex_search`
- `pageindex_get_page`
- `pageindex_list_pages`

Retrieval plugins can append additional tools, but they cannot currently replace the built-in names.

### Existing Retrieval Plugin Seam

`internal/plugins/interfaces.go` defines `RetrievalPlugin`:

- `Initialize(ctx, wikiRoot)`
- `Search(ctx, query, opts)`
- `MCPTools()`
- `HandleMCPCall(ctx, tool, args)`

`internal/integrations/pageindex/server.go` appends retrieval plugin tools to `tools/list` and delegates unknown `tools/call` names to plugins.

The concrete example is MarkedUp:

- `markedup_search`
- `markedup_traverse`
- `markedup_graph`

This is a good seam for adding PageIndex-style capabilities, but it is currently an extension seam, not a replacement seam.

## Upstream PageIndex State

The main `VectifyAI/PageIndex` repo is Python. It generates hierarchical document tree structures and exposes helper APIs for retrieving document metadata, document structure, and page or line content.

Important upstream files:

- `pageindex/page_index.py`
- `pageindex/page_index_md.py`
- `pageindex/retrieve.py`
- `pageindex/client.py`
- `run_pageindex.py`
- `examples/agentic_vectorless_rag_demo.py`

### Upstream Engine Behavior

PageIndex's README describes a two-step model:

1. Generate a table-of-contents tree structure for a document.
2. Perform reasoning-based retrieval through tree search.

For PDFs, `pageindex/page_index.py` uses LLM calls to detect/extract/transform a table of contents, infer physical page indices, validate/fix the generated tree, recursively split large nodes, add node text, add node summaries, and optionally generate a document description.

For Markdown, `pageindex/page_index_md.py` builds a tree from heading levels, stores node ids and line numbers, can thin small nodes, can add node summaries, and can generate a document description.

`pageindex/client.py` wraps this in a document workspace. The key user flow is:

- `client.index(file_path)`
- `client.get_document(doc_id)`
- `client.get_document_structure(doc_id)`
- `client.get_page_content(doc_id, pages)`

The example agent intentionally does not run a keyword search. It asks the model to inspect the document structure, select tight page ranges, then fetch content.

### Upstream MCP Packaging

The official MCP package lives in `VectifyAI/pageindex-mcp`, not the main PageIndex engine repo.

That package is TypeScript and wraps remote PageIndex MCP services. It uses `@modelcontextprotocol/sdk`, OAuth, and dynamic remote tool/resource proxying.

Important upstream MCP files:

- `src/server.ts`
- `src/client/mcp-client.ts`
- `src/tools/index.ts`
- `src/tools/process-document.ts`
- `src/tools/remote-proxy.ts`

The local tool surface includes `process_document`, which uploads a local or remote PDF through remote tools (`get_signed_upload_url`, `submit_document`). Other tools are fetched dynamically from the remote MCP server and proxied locally, excluding internal upload tools.

This means using the official MCP package directly is mostly a cloud/service wrapper, not a local self-hosted PageIndex engine dependency.

## Gap Analysis

### Conceptual Gap

Plexium currently performs lexical search over wiki pages.

PageIndex performs document/tree indexing plus LLM-guided navigation over the tree.

That is the largest migration gap. The MCP wiring is comparatively straightforward.

### Runtime Gap

Plexium is Go.

Self-hosted PageIndex is Python, with dependencies on:

- `litellm`
- `pymupdf`
- `PyPDF2`
- `python-dotenv`
- `pyyaml`

Official PageIndex MCP is TypeScript and primarily proxies to hosted PageIndex services.

There is no clean Go library dependency for the full upstream engine.

### Plugin-System Gap

Plexium has two plugin concepts:

- compiled Go runtime plugins registered by `bootstrap.BuildRegistry`
- adapter/script bundles installed by `plexium plugin add`

The current plugin installer does not dynamically load Go retrieval plugins. A PageIndex plugin can be built as a compiled built-in retrieval plugin today, but not as a true external plugin without core plugin lifecycle work.

### Command-Surface Gap

Plexium-specific features to preserve:

- `.wiki`-rooted markdown search
- wiki frontmatter and `[[wiki-links]]`
- fallback retrieval through `_index.md` and content scan
- Claude/Codex setup commands
- MarkedUp retrieval plugin aggregation
- local credentials/rate-tracker propagation into retrieval plugins

PageIndex-specific features missing today:

- document workspace
- PDF ingestion
- markdown tree generation across heading levels
- node ids
- line/page range retrieval
- node summaries and document descriptions
- tree traversal/reasoning workflow
- possible remote hosted MCP proxy integration

## Migration Options

### Option A: Compiled-In PageIndex Retrieval Plugin

Build a new compiled Go retrieval plugin, likely `internal/plugins/pageindex`, and register it from `bootstrap.BuildRegistry`.

Implementation shape:

- keep current `pageindex_*` tools as core for compatibility
- add new tools such as `pageindex_documents`, `pageindex_structure`, `pageindex_content`, or `pageindex_tree_search`
- optionally use upstream Python PageIndex as a subprocess for indexing
- store generated structures under `.plexium/pageindex/`
- expose results through `RetrievalPlugin.MCPTools`

Pros:

- smallest safe change
- fits existing plugin extension seam
- avoids dynamic plugin loading design work
- preserves current agent setups

Cons:

- still compiled into Plexium
- does not fully remove long-lived maintenance unless upstream is invoked as a subprocess or package-managed dependency
- leaves built-in `pageindex_*` hardcoded for now

Estimated complexity: medium.

### Option B: Core Replacement Behind Existing Tool Names

Refactor the MCP server so built-in `pageindex_*` tools are provided by a retrieval provider rather than hardcoded in `server.go`.

Implementation shape:

- introduce a default retrieval provider abstraction
- have the current flat wiki index implement that provider
- have PageIndex plugin optionally replace the default provider
- route `plexium retrieve` through the provider registry
- preserve output schemas where possible

Pros:

- moves toward true pluggability
- lets PageIndex own current tool names
- avoids duplicate search surfaces over time

Cons:

- higher regression risk
- requires careful compatibility tests
- needs provider precedence and conflict rules
- needs a decision for projects without PageIndex enabled

Estimated complexity: medium-high.

### Option C: External/Dynamic PageIndex Plugin

Make PageIndex installable through `plexium plugin add`, not compiled into Plexium.

Implementation shape:

- extend plugin manifest schema beyond adapter scripts
- add retrieval plugin lifecycle/install/enablement semantics
- decide how Go loads external retrieval implementations, or define a subprocess protocol
- add versioning and health checks
- add config schema migration support

Pros:

- aligns with a long-term plugin product story
- separates Plexium core from PageIndex updates
- could support other retrieval engines later

Cons:

- much larger architecture project
- Go dynamic loading is not portable enough to assume
- subprocess protocol design becomes part of Plexium's public surface
- requires CLI, MCP, config, docs, tests, and release work

Estimated complexity: high.

### Option D: Official PageIndex MCP Proxy Plugin

Wrap `@pageindex/mcp` or the hosted PageIndex MCP service from Plexium.

Implementation shape:

- install/run Node package as a sidecar, or add a Plexium MCP proxy tool
- delegate PageIndex document processing and retrieval to remote service
- keep Plexium wiki search local

Pros:

- least PageIndex engine maintenance
- tracks official MCP behavior more closely
- useful if hosted PageIndex is acceptable

Cons:

- introduces Node runtime and network/service dependency
- OAuth/API-key flow must fit Plexium credentials model
- does not provide full local self-hosted PageIndex
- less suitable for private/offline repo memory

Estimated complexity: medium, with product-policy risk.

## Recommended Roadmap

### Phase 1: Make Current Retrieval Provider Explicit

Goal: isolate today's behavior behind an internal provider interface without changing behavior.

Tasks:

- define an internal retrieval provider interface for search/get/list
- adapt current `internal/integrations/pageindex.PageIndex` to that interface
- keep current MCP schemas and CLI output unchanged
- add characterization tests for `pageindex_search`, `pageindex_get_page`, `pageindex_list_pages`, and `plexium retrieve`

Exit criteria:

- no behavior changes
- current flat wiki retriever is replaceable inside the server
- plugin replacement rules are documented but not yet enabled

### Phase 2: Add PageIndex Tree Plugin Prototype

Goal: prove PageIndex-style tree retrieval as a plugin-owned MCP extension.

Tasks:

- add compiled-in `internal/plugins/pageindex` retrieval plugin
- generate markdown trees from `.wiki` pages or selected wiki bundles
- store tree JSON under `.plexium/pageindex/`
- expose tree-oriented MCP tools without replacing existing names
- decide whether indexing is eager at `pageindex serve` or explicit through a tool/command

Likely first tool set:

- `pageindex_tree_list_documents`
- `pageindex_tree_get_structure`
- `pageindex_tree_get_content`

Exit criteria:

- agents can inspect a tree and retrieve tight content ranges
- existing flat search still works
- no Python dependency is required yet unless explicitly chosen

### Phase 3: Choose Engine Strategy

Goal: decide whether Plexium reimplements enough PageIndex locally or wraps upstream Python.

Decision points:

- Is PageIndex tree generation needed for PDFs, or only Plexium wiki markdown?
- Is LLM-based summary/tree repair acceptable during indexing?
- Should indexing work offline/deterministically?
- Are Python dependencies acceptable for a Plexium plugin?
- Should hosted PageIndex be supported as a separate provider?

Recommendation:

- use a local Go markdown-tree implementation for `.wiki` first
- defer Python subprocess support until PDF ingestion is a confirmed requirement
- treat hosted PageIndex MCP as a separate optional connector, not the default

### Phase 4: Route CLI Retrieval Through Registry

Goal: make `plexium retrieve` plugin-aware.

Tasks:

- build/init retrieval registry in the retrieve command
- define provider precedence
- add a `--provider` flag only if multiple providers are enabled
- preserve default output for existing users
- add JSON schema tests for output stability

Exit criteria:

- MarkedUp and PageIndex-style plugins can participate in CLI retrieval
- existing `plexium retrieve` still behaves the same by default

### Phase 5: Decide Replacement vs Coexistence

Goal: determine whether PageIndex should own `pageindex_*` names.

Options:

- keep current flat `pageindex_*` names and expose tree tools separately
- make `pageindex_search` provider-backed
- deprecate flat behavior behind compatibility aliases

Recommendation:

- do not replace the names until tree retrieval is stable in production
- add explicit docs distinguishing "wiki search" from "tree retrieval"

### Phase 6: External Plugin Lifecycle

Goal: only if required, make PageIndex a true installable plugin.

Tasks:

- extend plugin manifests for retrieval plugins
- define subprocess plugin protocol, since portable Go dynamic loading is not a good default
- implement install/enable/disable/status
- add health checks and version reporting
- add migration docs

Exit criteria:

- `plexium plugin add pageindex` can install and enable a retrieval provider
- MCP and CLI both discover it through the same registry

## Risks

### Tool Name Compatibility

Existing MCP clients may depend on `pageindex_search`, `pageindex_get_page`, and `pageindex_list_pages`. Replacing these too early risks breaking agents and tests.

Mitigation: add new tree tools first; replace only after compatibility tests exist.

### Dependency Boundary

Wrapping upstream Python reduces reimplementation drift but introduces Python dependency management, model/API key handling, subprocess failures, and index cache invalidation.

Mitigation: start with Go markdown tree indexing for `.wiki`, then add Python as an optional advanced backend.

### Hosted Service Coupling

The official MCP package is heavily oriented around hosted PageIndex services. It is useful as an integration option, but not a drop-in replacement for local Plexium retrieval.

Mitigation: model hosted PageIndex as an optional remote provider.

### Plugin Architecture Expansion

True dynamic retrieval plugins require new product semantics. Doing that only for PageIndex may overfit.

Mitigation: design the external plugin protocol around generic retrieval providers.

## Complexity Estimate

Recommended path, Phases 1-3:

- 1-2 weeks for careful provider extraction, tests, and a markdown tree prototype
- more if PDF ingestion or Python subprocess support is included immediately

Full replacement, Phases 1-5:

- 3-5 weeks, mostly due to compatibility, provider precedence, CLI/MCP behavior, and tests

True external plugin lifecycle, Phase 6:

- 4+ additional weeks as a separate architecture project

## Bottom Line

The migration should be staged. Plexium already has enough plugin infrastructure to add PageIndex-style retrieval as a compiled-in plugin extension. It does not yet have enough infrastructure to make PageIndex a fully external, dynamically installed retrieval plugin or to let a plugin replace the core `pageindex_*` tools cleanly.

The highest-value first implementation is a PageIndex-style tree retrieval plugin for `.wiki` markdown, exposed through new MCP tools. That proves the model and gives agents a better retrieval workflow without destabilizing the existing CLI and MCP contracts.
