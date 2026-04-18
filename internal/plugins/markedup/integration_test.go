package markedup_test

// Integration test: exercises the MarkedUp plugin end-to-end through the
// plugin Registry. Covers:
//   1. Registering enricher + retrieval against plugins.Registry
//   2. Running the enricher as a StageAfterWrite pipeline hook
//   3. Verifying graph metadata lands in .plexium/manifest.json
//   4. Initializing the retrieval plugin against the same wiki root
//   5. Driving an MCP call through the retrieval plugin's HandleMCPCall
//
// It lives in its own _test package to exercise the public API the way
// real Plexium initialization would wire it.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/plugins"
	"github.com/Clarit-AI/Plexium/internal/plugins/markedup"
)

func TestIntegration_RegistryEnricherRetrieval(t *testing.T) {
	repoRoot := t.TempDir()
	wikiRoot := filepath.Join(repoRoot, ".wiki")
	if err := os.MkdirAll(wikiRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pages carry minimal frontmatter, matching what Plexium's convert
	// pipeline writes to .wiki/. The retrieval plugin uses
	// WithAutoEnrich(false) to avoid disk writes, so pages must have
	// parseable frontmatter already.
	pages := map[string]string{
		"auth.md": `---
id: auth
title: Auth
entity-type: module
confidence: 0.7
tags: [authentication]
---
# Auth

Handles authentication for the app. Depends on [[crypto]].
`,
		"crypto.md": `---
id: crypto
title: Crypto
entity-type: module
confidence: 0.7
tags: [security]
---
# Crypto

Cryptographic primitives.
`,
	}
	for name, body := range pages {
		if err := os.WriteFile(filepath.Join(wikiRoot, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Seed the manifest with entries matching the two pages.
	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m := manifest.NewEmptyManifest()
	m.Pages = []manifest.PageEntry{
		{WikiPath: "auth.md", Title: "Auth", Ownership: "managed"},
		{WikiPath: "crypto.md", Title: "Crypto", Ownership: "managed"},
	}
	if err := mgr.Save(m); err != nil {
		t.Fatal(err)
	}

	// Build a parsed Config as if it came from .plexium/config.yml.
	cfg, err := markedup.ParseConfig(map[string]any{
		"enabled":    true,
		"autoEnrich": true,
	})
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	// Register both plugins through the canonical Registry API.
	reg := plugins.NewRegistry()
	enricher := markedup.NewEnricher(cfg)
	retrieval := markedup.NewRetrieval(cfg)
	if err := reg.Register(enricher); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(retrieval); err != nil {
		t.Fatal(err)
	}

	// Verify the registry routes them to the correct buckets.
	pipelinePlugins := reg.PipelinePlugins(plugins.StageAfterWrite)
	if len(pipelinePlugins) != 1 {
		t.Fatalf("expected 1 pipeline plugin, got %d", len(pipelinePlugins))
	}
	retrievalPlugins := reg.RetrievalPlugins()
	if len(retrievalPlugins) != 1 {
		t.Fatalf("expected 1 retrieval plugin, got %d", len(retrievalPlugins))
	}

	// Simulate the convert pipeline calling the after-write hook.
	ctx := context.Background()
	data := &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages: []plugins.PipelinePage{
			{WikiPath: "auth.md", Title: "Auth"},
			{WikiPath: "crypto.md", Title: "Crypto"},
		},
	}
	for _, pp := range pipelinePlugins {
		if err := pp.Process(ctx, data); err != nil {
			t.Fatalf("pipeline plugin %s: %v", pp.Name(), err)
		}
	}

	// Manifest should now be v2 with graph metadata on both pages.
	reloaded, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Version != 2 {
		t.Errorf("expected Version=2 after enrichment, got %d", reloaded.Version)
	}
	authEntry := findPage(t, reloaded, "auth.md")
	if authEntry.EntityType == "" {
		t.Error("auth.md missing EntityType after enrichment")
	}
	if authEntry.EnrichedBy != markedup.EnricherVersion {
		t.Errorf("auth.md EnrichedBy=%q, want %q", authEntry.EnrichedBy, markedup.EnricherVersion)
	}
	// [[crypto]] wikilink should produce a relationship.
	foundRel := false
	for _, r := range authEntry.Relationships {
		if r.Target == "crypto" {
			foundRel = true
			break
		}
	}
	if !foundRel {
		t.Errorf("auth.md relationships missing crypto target: %+v", authEntry.Relationships)
	}

	// Initialize retrieval plugin over the same wiki root.
	if err := retrieval.Initialize(ctx, wikiRoot); err != nil {
		t.Fatalf("retrieval init: %v", err)
	}

	// Drive an MCP search call through the retrieval plugin.
	args, _ := json.Marshal(map[string]any{"query": "Auth", "limit": 5})
	result, err := retrieval.HandleMCPCall(ctx, "markedup_search", args)
	if err != nil {
		t.Fatalf("markedup_search call: %v", err)
	}
	results, ok := result.([]plugins.PluginSearchResult)
	if !ok {
		t.Fatalf("expected []PluginSearchResult, got %T", result)
	}
	if len(results) == 0 {
		t.Fatal("expected search results for 'Auth'")
	}

	// At least one hit should point at auth.md.
	foundAuth := false
	for _, r := range results {
		if strings.HasSuffix(r.PagePath, "auth.md") {
			foundAuth = true
			break
		}
	}
	if !foundAuth {
		t.Errorf("expected auth.md among search results; got paths %+v", paths(results))
	}
}

func findPage(t *testing.T, m *manifest.Manifest, wikiPath string) manifest.PageEntry {
	t.Helper()
	for _, p := range m.Pages {
		if p.WikiPath == wikiPath {
			return p
		}
	}
	t.Fatalf("manifest missing page %q", wikiPath)
	return manifest.PageEntry{}
}

func paths(rs []plugins.PluginSearchResult) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.PagePath
	}
	return out
}
