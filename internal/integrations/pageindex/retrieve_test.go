package pageindex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewRetriever(t *testing.T) {
	wikiDir := setupTestWiki(t)
	r := NewRetriever(wikiDir)

	if r.WikiRoot != wikiDir {
		t.Errorf("WikiRoot = %q, want %q", r.WikiRoot, wikiDir)
	}

	if r.PageIdx == nil {
		t.Error("PageIdx should not be nil")
	}

	if r.UseFallback {
		t.Error("UseFallback should be false when wiki exists")
	}
}

func TestNewRetriever_FallbackOnMissingDir(t *testing.T) {
	r := NewRetriever("/nonexistent/wiki")

	if !r.UseFallback {
		t.Error("UseFallback should be true when wiki directory doesn't exist")
	}
}

func TestRetrieve_PageIndex(t *testing.T) {
	wikiDir := setupTestWiki(t)
	r := NewRetriever(wikiDir)

	result, err := r.Retrieve("authentication")
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}

	if result.Method != "pageindex" {
		t.Errorf("Method = %q, want %q", result.Method, "pageindex")
	}

	if result.Query != "authentication" {
		t.Errorf("Query = %q, want %q", result.Query, "authentication")
	}

	if len(result.Pages) == 0 {
		t.Fatal("expected pages in result, got none")
	}

	// Top hit should be the auth module
	top := result.Pages[0]
	if top.Title != "Authentication Module" {
		t.Errorf("top result title = %q, want %q", top.Title, "Authentication Module")
	}

	if top.Relevance <= 0 {
		t.Error("top result relevance should be positive")
	}
}

func TestRetrieve_SortedByRelevance(t *testing.T) {
	wikiDir := setupTestWiki(t)
	r := NewRetriever(wikiDir)

	result, err := r.Retrieve("auth")
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}

	if len(result.Pages) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(result.Pages))
	}

	for i := 1; i < len(result.Pages); i++ {
		if result.Pages[i].Relevance > result.Pages[i-1].Relevance {
			t.Errorf("results not sorted by relevance: [%d]=%f > [%d]=%f",
				i, result.Pages[i].Relevance, i-1, result.Pages[i-1].Relevance)
		}
	}
}

func TestRetrieve_NoResults(t *testing.T) {
	wikiDir := setupTestWiki(t)
	r := NewRetriever(wikiDir)

	result, err := r.Retrieve("zzzznonexistentterm")
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}

	// Should fall back when PageIndex returns nothing
	if result.Method != "fallback" {
		t.Errorf("Method = %q, want %q for no-match query", result.Method, "fallback")
	}
}

func TestFallbackRetrieve_WithIndex(t *testing.T) {
	wikiDir := setupTestWiki(t)
	r := &Retriever{
		WikiRoot:    wikiDir,
		PageIdx:     New(wikiDir),
		UseFallback: true,
	}

	result, err := r.fallbackRetrieve("authentication")
	if err != nil {
		t.Fatalf("fallbackRetrieve() error = %v", err)
	}

	if result.Method != "fallback" {
		t.Errorf("Method = %q, want %q", result.Method, "fallback")
	}

	if len(result.Pages) == 0 {
		t.Fatal("expected pages from fallback, got none")
	}

	// Should find the auth module by scanning _index.md and content
	foundAuth := false
	for _, p := range result.Pages {
		if p.Title == "Authentication Module" || p.Path == filepath.Join("modules", "auth.md") {
			foundAuth = true
			break
		}
	}
	if !foundAuth {
		t.Error("fallback should find auth module")
	}
}

func TestFallbackRetrieve_EmptyQuery(t *testing.T) {
	wikiDir := setupTestWiki(t)
	r := &Retriever{
		WikiRoot:    wikiDir,
		PageIdx:     New(wikiDir),
		UseFallback: true,
	}

	result, err := r.fallbackRetrieve("")
	if err != nil {
		t.Fatalf("fallbackRetrieve() error = %v", err)
	}

	if len(result.Pages) != 0 {
		t.Errorf("expected 0 pages for empty query, got %d", len(result.Pages))
	}
}

func TestFallbackRetrieve_NoIndexFile(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")

	// Create wiki without _index.md
	os.MkdirAll(filepath.Join(wikiDir, "modules"), 0755)
	os.WriteFile(
		filepath.Join(wikiDir, "modules", "auth.md"),
		[]byte("---\ntitle: Auth\nownership: managed\n---\n\n# Auth\n\nHandles authentication.\n"),
		0644,
	)

	r := &Retriever{
		WikiRoot:    wikiDir,
		PageIdx:     New(wikiDir),
		UseFallback: true,
	}

	result, err := r.fallbackRetrieve("auth")
	if err != nil {
		t.Fatalf("fallbackRetrieve() error = %v", err)
	}

	// Should still find via content scanning
	if len(result.Pages) == 0 {
		t.Fatal("expected pages from content scan, got none")
	}
}

func TestFallbackRetrieve_SortedByRelevance(t *testing.T) {
	wikiDir := setupTestWiki(t)
	r := &Retriever{
		WikiRoot:    wikiDir,
		PageIdx:     New(wikiDir),
		UseFallback: true,
	}

	result, err := r.fallbackRetrieve("auth")
	if err != nil {
		t.Fatalf("fallbackRetrieve() error = %v", err)
	}

	for i := 1; i < len(result.Pages); i++ {
		if result.Pages[i].Relevance > result.Pages[i-1].Relevance {
			t.Errorf("fallback results not sorted: [%d]=%f > [%d]=%f",
				i, result.Pages[i].Relevance, i-1, result.Pages[i-1].Relevance)
		}
	}
}

func TestSearchIndex(t *testing.T) {
	indexContent := `# Wiki Index

## modules

- [[modules/auth.md|Authentication Module]]: Handles user authentication
- [[modules/database.md|Database Layer]]: Database connection pooling

## decisions

- [[decisions/001.md|ADR 001: Auth Strategy]]: JWT-based auth
`

	r := &Retriever{WikiRoot: t.TempDir()}
	hits := r.searchIndex(indexContent, []string{"auth"})

	if len(hits) == 0 {
		t.Fatal("expected hits from index search, got none")
	}

	// Should find both auth-related entries
	if len(hits) < 2 {
		t.Errorf("expected at least 2 hits for 'auth', got %d", len(hits))
	}
}
