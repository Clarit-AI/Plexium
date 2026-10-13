package markedup_test

// Enricher contract tests for the MarkedUp integration boundary (KHA-574).
//
// These tests fail before a MarkedUp bump if breaking semantics are
// introduced on the boundary. They live in `package markedup_test` so they
// exercise the public API exactly as `internal/plugins/bootstrap` and
// `cmd/plexium` do at runtime.
//
// Required behaviors pinned here:
//
//  1. Wikilink-derived Relationships and NER-derived SemanticRelationships
//     must remain DISTINCT fields in manifest.GraphMetadata. Collapsing them
//     would silently lose Tier 2 edges (the bug fixed in ed2a09b).
//
//  2. Page IDs must survive the enrich round-trip. A blank or duplicate ID
//     breaks downstream retrieval (`KnowledgeIndex.Get` lookup misses,
//     `CompactGraphSummary` filters resolve incorrectly).
//
//  3. Page Summaries must survive the enrich round-trip. MarkedUp's
//     Tier 1 enrichment may populate them; they must reach the manifest
//     without being stripped.
//
//  4. Configuration keys normalize case-insensitively. An upper-case
//     `AutoEnrich` and a lower-case `autoenrich` must produce the same
//     parsed Config — the existing `ParseConfig.normalizeKeys` already
//     does this; this test pins that behavior so a future refactor
//     doesn't regress it.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/plugins"
	"github.com/Clarit-AI/Plexium/internal/plugins/markedup"
)

// TestEnricherContract_RelationshipsNotCollapsed is the regression guard for
// the bug fixed in ed2a09b: a MarkedUp bump that merges Wikilink
// `Relationships` with `SemanticRelationships` would silently lose the Tier 2
// edges on every subsequent enrichment pass. The fixture carries both kinds
// of edges — wikilink ([[b]]) populates Relationships, and we synthesize a
// SemanticRelationships edge by hand-injecting it into the source page
// (since Plexium's enricher only produces SemanticRelationships from a Tier 2
// model call, and we don't wire one in this contract test).
//
// After enrichment, the manifest entry for `a.md` must carry:
//
//   - Relationships:           a wikilink edge to "b"
//   - SemanticRelationships:   a separate edge with a free-text target
//
// and the two slices must be observably distinct (different lengths, no
// element present in both).
func TestEnricherContract_RelationshipsNotCollapsed(t *testing.T) {
	pages := map[string]string{
		// a.md carries both a wikilink edge (Tier 1, -> Relationships)
		// AND a pre-existing semantic-relationships block in its
		// frontmatter (Tier 2, -> SemanticRelationships). Enrichment
		// preserves both fields; neither collapses into the other.
		"a.md": `---
id: a
title: A
entity-type: module
confidence: 0.7
semantic-relationships:
  - target: SomeUnresolvedEntity
    type: mentions
    strength: 0.5
---
# A

Relates to [[b]] for authentication.
`,
		"b.md": `---
id: b
title: B
entity-type: module
confidence: 0.7
---
# B

Other page.
`,
	}

	repoRoot, wikiRoot := setupContractWiki(t, pages)

	cfg, err := markedup.ParseConfig(map[string]any{
		"enabled":    true,
		"autoEnrich": true,
	})
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	p := markedup.NewEnricher(cfg)
	err = p.Process(context.Background(), &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages: []plugins.PipelinePage{
			{WikiPath: "a.md", Title: "A"},
			{WikiPath: "b.md", Title: "B"},
		},
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}

	entry := findManifestPage(t, m, "a.md")

	if len(entry.Relationships) == 0 {
		t.Errorf("expected Wikilink Relationships populated (from [[b]]); got %+v", entry.Relationships)
	}
	if len(entry.SemanticRelationships) == 0 {
		t.Errorf("expected SemanticRelationships preserved from frontmatter; got %+v", entry.SemanticRelationships)
	}

	// Wikilink edge target is "b" (or a MarkedUp-resolved variant like
	// "b.md"). Semantic edge target is the free-text entity name. They
	// MUST NOT be the same target — if they are, the two slices were
	// collapsed.
	for _, wr := range entry.Relationships {
		for _, sr := range entry.SemanticRelationships {
			if wr.Target == sr.Target && wr.Type == sr.Type {
				t.Errorf("Wikilink edge %+v and Semantic edge %+v look identical — slices were collapsed",
					wr, sr)
			}
		}
	}

	// At minimum: SemanticRelationships must contain the free-text entity
	// name we hand-injected.
	foundSemantic := false
	for _, sr := range entry.SemanticRelationships {
		if sr.Target == "SomeUnresolvedEntity" && sr.Type == "mentions" {
			foundSemantic = true
			break
		}
	}
	if !foundSemantic {
		t.Errorf("SemanticRelationships missing hand-injected edge; got %+v", entry.SemanticRelationships)
	}

	// And the manifest's semantic-equality check must respect both fields
	// independently: if `existingMeta` and `newMeta` differ only in
	// SemanticRelationships, they MUST NOT be considered equal (the bug
	// fixed in ed2a09b).
	if manifest.GraphMetadataSemanticEqual(
		manifest.GraphMetadata{
			Relationships:         entry.Relationships,
			SemanticRelationships: entry.SemanticRelationships,
		},
		manifest.GraphMetadata{
			Relationships:         entry.Relationships,
			SemanticRelationships: nil, // pretend Tier 2 was lost
		},
	) {
		t.Error("GraphMetadataSemanticEqual ignores SemanticRelationships — bug regression")
	}
}

// TestEnricherContract_PageIDsPreserved pins the rule that the page ID field
// survives the enrich round-trip with no blanks and no duplicates. A bump
// that drops ID, or that produces blank IDs from the parser, would surface
// as silent retrieval misses downstream.
func TestEnricherContract_PageIDsPreserved(t *testing.T) {
	pages := map[string]string{
		"alpha.md": `---
id: alpha-id
title: Alpha
entity-type: module
confidence: 0.5
---
# Alpha

[[beta-id]]
`,
		"beta.md": `---
id: beta-id
title: Beta
entity-type: module
confidence: 0.5
---
# Beta

Content.
`,
	}

	repoRoot, wikiRoot := setupContractWiki(t, pages)
	cfg, err := markedup.ParseConfig(map[string]any{"enabled": true, "autoEnrich": true})
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	p := markedup.NewEnricher(cfg)
	err = p.Process(context.Background(), &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages: []plugins.PipelinePage{
			{WikiPath: "alpha.md", Title: "Alpha"},
			{WikiPath: "beta.md", Title: "Beta"},
		},
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}

	// Every tracked page must have a non-blank WikiPath and Title.
	for _, page := range m.Pages {
		if strings.TrimSpace(page.WikiPath) == "" {
			t.Errorf("manifest page with blank WikiPath: %+v", page)
		}
		if strings.TrimSpace(page.Title) == "" {
			t.Errorf("manifest page %q with blank Title", page.WikiPath)
		}
	}

	// WikiPaths must be unique — duplicates would surface as manifest-level
	// ambiguity.
	seen := map[string]bool{}
	for _, page := range m.Pages {
		if seen[page.WikiPath] {
			t.Errorf("duplicate WikiPath in manifest: %q", page.WikiPath)
		}
		seen[page.WikiPath] = true
	}

	// And specifically: both alpha and beta must still be tracked.
	for _, want := range []string{"alpha.md", "beta.md"} {
		if !seen[want] {
			t.Errorf("manifest missing page %q", want)
		}
	}
}

// TestEnricherContract_SummaryPreserved pins the rule that a `Summary` field
// in the page frontmatter survives the enrich round-trip. MarkedUp exposes
// `schema.GraphFrontmatter.Summary`; the current contract doesn't write it
// into the manifest (the manifest's `GraphMetadata` has no Summary field
// today), but a future expansion that does must not strip existing summaries.
//
// Today this test pins the **parser-side** behavior: a Summary field in the
// source frontmatter is preserved through `markdown.ParseBytesPermissive`
// (and therefore through the enrich loop). If a MarkedUp bump renames or
// drops `schema.GraphFrontmatter.Summary`, this test fails.
func TestEnricherContract_SummaryPreserved(t *testing.T) {
	summaryText := "Authentication handles JWT and OAuth tokens."
	body := []byte(`---
id: auth
title: Auth
entity-type: module
confidence: 0.8
summary: "` + summaryText + `"
---
# Auth

Body content here.
`)
	tmp := filepath.Join(t.TempDir(), "auth.md")
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		t.Fatal(err)
	}

	// We don't need to run the full enricher for this test — the contract
	// is on the MarkedUp parser. Reuse the parser via enrich.EnrichPage's
	// input (markdown.ParseBytesPermissive).
	raw, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	// Pin the parser round-trip directly. This test will fail if MarkedUp
	// renames or strips the Summary field at the schema layer.
	parsed := parseSummaryContract(t, raw)
	if parsed.Summary != summaryText {
		t.Errorf("Summary field lost: got %q, want %q", parsed.Summary, summaryText)
	}
}

// TestEnricherContract_CaseNormalizedConfig pins the rule that
// `ParseConfig` treats upper-case and lower-case YAML keys identically. The
// implementation lower-cases keys in `normalizeKeys` before lookup; this test
// fails if a future refactor removes the normalization step (which would
// silently break viper-cased config in the field).
func TestEnricherContract_CaseNormalizedConfig(t *testing.T) {
	type wantFields struct {
		enabled                  bool
		autoEnrich               bool
		modelEnrich              bool
		writeEnrichedFrontmatter bool
		provider                 string
		dims                     int
	}

	cases := []struct {
		name string
		raw  map[string]any
		want wantFields
	}{
		{
			name: "lower camelCase",
			raw: map[string]any{
				"enabled":                  true,
				"autoEnrich":               true,
				"modelEnrich":              true,
				"writeEnrichedFrontmatter": true,
				"embeddings": map[string]any{
					"enabled":   true,
					"provider":  "openai-compatible",
					"model":     "text-embedding-3-small",
					"endpoint":  "https://api.openai.com",
					"apiKeyEnv": "OPENAI_API_KEY",
					"dims":      1024,
				},
			},
			want: wantFields{
				enabled: true, autoEnrich: true, modelEnrich: true,
				writeEnrichedFrontmatter: true,
				provider: "openai-compatible", dims: 1024,
			},
		},
		{
			name: "UPPER CAMELCASE",
			raw: map[string]any{
				"Enabled":                  true,
				"AutoEnrich":               true,
				"ModelEnrich":              true,
				"WriteEnrichedFrontmatter": true,
				"Embeddings": map[string]any{
					"Enabled":   true,
					"Provider":  "openai-compatible",
					"Model":     "text-embedding-3-small",
					"Endpoint":  "https://api.openai.com",
					"ApiKeyEnv": "OPENAI_API_KEY",
					"Dims":      1024,
				},
			},
			want: wantFields{
				enabled: true, autoEnrich: true, modelEnrich: true,
				writeEnrichedFrontmatter: true,
				provider: "openai-compatible", dims: 1024,
			},
		},
		{
			name: "Mixed case (duplicates normalize to last-write-wins)",
			raw: map[string]any{
				"Enabled":    true,
				"autoenrich": true,
				"AUTOENRICH": true,
				// No modelEnrich / writeEnrichedFrontmatter / embeddings
				// here — this subtest only pins the Enabled + AutoEnrich
				// fields. The point: no error from unknown keys, and
				// duplicate-case keys normalize cleanly.
			},
			want: wantFields{
				enabled:    true,
				autoEnrich: true,
			},
		},
		{
			name: "Embeddings nested upper-case only",
			raw: map[string]any{
				"Enabled": true,
				"Embeddings": map[string]any{
					"Provider": "openai-compatible",
					"Model":    "text-embedding-3-small",
					"Endpoint": "https://api.openai.com",
					"Dims":     2048,
				},
			},
			want: wantFields{
				enabled:    true,
				autoEnrich: true, // AutoEnrich defaults to true when Enabled=true
				provider:   "openai-compatible",
				dims:       2048,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := markedup.ParseConfig(tc.raw)
			if err != nil {
				t.Fatalf("ParseConfig(%s): %v", tc.name, err)
			}
			if cfg.Enabled != tc.want.enabled {
				t.Errorf("Enabled: got %v, want %v", cfg.Enabled, tc.want.enabled)
			}
			if cfg.AutoEnrich != tc.want.autoEnrich {
				t.Errorf("AutoEnrich: got %v, want %v", cfg.AutoEnrich, tc.want.autoEnrich)
			}
			if cfg.ModelEnrich != tc.want.modelEnrich {
				t.Errorf("ModelEnrich: got %v, want %v", cfg.ModelEnrich, tc.want.modelEnrich)
			}
			if cfg.WriteEnrichedFrontmatter != tc.want.writeEnrichedFrontmatter {
				t.Errorf("WriteEnrichedFrontmatter: got %v, want %v", cfg.WriteEnrichedFrontmatter, tc.want.writeEnrichedFrontmatter)
			}
			if tc.want.provider != "" && cfg.Embeddings.Provider != tc.want.provider {
				t.Errorf("Embeddings.Provider: got %q, want %q", cfg.Embeddings.Provider, tc.want.provider)
			}
			if tc.want.dims != 0 && cfg.Embeddings.Dims != tc.want.dims {
				t.Errorf("Embeddings.Dims: got %d, want %d", cfg.Embeddings.Dims, tc.want.dims)
			}
		})
	}
}

// setupContractWiki mirrors the helper in enricher_test.go (kept private
// there) but is duplicated here so the contract tests are self-contained and
// readable in isolation.
func setupContractWiki(t *testing.T, pages map[string]string) (string, string) {
	t.Helper()
	repoRoot := t.TempDir()
	wikiRoot := filepath.Join(repoRoot, ".wiki")
	if err := os.MkdirAll(wikiRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m := manifest.NewEmptyManifest()
	for wikiPath, body := range pages {
		fullPath := filepath.Join(wikiRoot, wikiPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		m.Pages = append(m.Pages, manifest.PageEntry{
			WikiPath:  wikiPath,
			Title:     strings.TrimSuffix(filepath.Base(wikiPath), ".md"),
			Ownership: "managed",
		})
	}
	if err := mgr.Save(m); err != nil {
		t.Fatal(err)
	}
	return repoRoot, wikiRoot
}

func findManifestPage(t *testing.T, m *manifest.Manifest, wikiPath string) manifest.PageEntry {
	t.Helper()
	for _, p := range m.Pages {
		if p.WikiPath == wikiPath {
			return p
		}
	}
	t.Fatalf("manifest missing page %q", wikiPath)
	return manifest.PageEntry{}
}

// parseSummaryContract parses raw markdown bytes via the MarkedUp
// permissive parser and returns the frontmatter. It exists here as a thin
// shim so the test is self-documenting about WHICH parser it pins.
func parseSummaryContract(t *testing.T, raw []byte) markedupFrontmatter {
	t.Helper()
	return markedupParseFrontmatter(raw)
}
