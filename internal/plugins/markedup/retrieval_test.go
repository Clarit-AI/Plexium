package markedup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/plugins"
)

// seedEnrichedWiki creates a temp wiki with real YAML-frontmatter pages
// so index.Load can build a graph over them.
func seedEnrichedWiki(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	pages := map[string]string{
		"auth.md": `---
id: auth
title: Auth
entity-type: module
confidence: 0.9
tags: [authentication, security]
relationships:
  - target: crypto
    type: depends-on
    strength: 0.8
---
# Auth

Authentication module handles JWT and OAuth.
`,
		"crypto.md": `---
id: crypto
title: Crypto
entity-type: module
confidence: 0.9
tags: [security]
---
# Crypto

Cryptographic primitives.
`,
	}

	for name, body := range pages {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRetrievalPlugin_InitializeLoadsIndex(t *testing.T) {
	wikiRoot := seedEnrichedWiki(t)

	p := NewRetrieval(Config{Enabled: true})
	if err := p.Initialize(context.Background(), wikiRoot); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	if p.idx.Load() == nil {
		t.Fatal("expected index populated after Initialize")
	}
}

func TestRetrievalPlugin_DisabledInitializeIsNoOp(t *testing.T) {
	wikiRoot := seedEnrichedWiki(t)
	p := NewRetrieval(Config{Enabled: false})
	if err := p.Initialize(context.Background(), wikiRoot); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if p.idx.Load() != nil {
		t.Error("index should not be loaded when Enabled=false")
	}
}

func TestRetrievalPlugin_SearchReturnsResults(t *testing.T) {
	wikiRoot := seedEnrichedWiki(t)
	p := NewRetrieval(Config{Enabled: true})
	if err := p.Initialize(context.Background(), wikiRoot); err != nil {
		t.Fatal(err)
	}

	results, err := p.Search(context.Background(), "Auth", plugins.SearchOpts{Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one search result")
	}
	if results[0].Score <= 0 {
		t.Errorf("expected positive score, got %v", results[0].Score)
	}
}

func TestRetrievalPlugin_SearchWithoutInitReturnsError(t *testing.T) {
	p := NewRetrieval(Config{Enabled: true})
	_, err := p.Search(context.Background(), "x", plugins.SearchOpts{})
	if err == nil {
		t.Fatal("expected error when searching before Initialize")
	}
}

func TestRetrievalPlugin_MCPTools(t *testing.T) {
	p := NewRetrieval(Config{Enabled: true})
	tools := p.MCPTools()
	if len(tools) < 3 {
		t.Fatalf("expected at least 3 MCP tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, td := range tools {
		names[td.Name] = true
	}
	for _, want := range []string{"markedup_search", "markedup_traverse", "markedup_graph"} {
		if !names[want] {
			t.Errorf("expected MCP tool %q", want)
		}
	}
}

func TestRetrievalPlugin_HandleMCPCall_Search(t *testing.T) {
	wikiRoot := seedEnrichedWiki(t)
	p := NewRetrieval(Config{Enabled: true})
	if err := p.Initialize(context.Background(), wikiRoot); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]any{"query": "Auth", "limit": 3})
	result, err := p.HandleMCPCall(context.Background(), "markedup_search", args)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	results, ok := result.([]plugins.PluginSearchResult)
	if !ok {
		t.Fatalf("expected []PluginSearchResult, got %T", result)
	}
	if len(results) == 0 {
		t.Error("expected at least one result")
	}
}

func TestRetrievalPlugin_HandleMCPCall_Traverse(t *testing.T) {
	wikiRoot := seedEnrichedWiki(t)
	p := NewRetrieval(Config{Enabled: true})
	if err := p.Initialize(context.Background(), wikiRoot); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]any{"id": "auth", "depth": 2})
	result, err := p.HandleMCPCall(context.Background(), "markedup_traverse", args)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil traverse result")
	}
}

func TestRetrievalPlugin_HandleMCPCall_Graph(t *testing.T) {
	wikiRoot := seedEnrichedWiki(t)
	p := NewRetrieval(Config{Enabled: true})
	if err := p.Initialize(context.Background(), wikiRoot); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]any{"maxPages": 10})
	result, err := p.HandleMCPCall(context.Background(), "markedup_graph", args)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil graph summary")
	}
}

func TestRetrievalPlugin_HandleMCPCall_UnknownTool(t *testing.T) {
	wikiRoot := seedEnrichedWiki(t)
	p := NewRetrieval(Config{Enabled: true})
	_ = p.Initialize(context.Background(), wikiRoot)

	_, err := p.HandleMCPCall(context.Background(), "nonexistent", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}
