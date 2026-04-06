package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockLLMClient implements LLMClient for testing.
type mockLLMClient struct {
	responses map[string]string // prompt substring -> response
	calls     []string          // record of prompts sent
}

func newMockLLMClient() *mockLLMClient {
	return &mockLLMClient{
		responses: make(map[string]string),
	}
}

func (m *mockLLMClient) Complete(prompt string) (string, error) {
	m.calls = append(m.calls, prompt)

	for substr, response := range m.responses {
		if strings.Contains(prompt, substr) {
			return response, nil
		}
	}
	return "NONE", nil
}

// createTestWiki sets up a temporary wiki directory with test pages.
func createTestWiki(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")

	pages := map[string]string{
		"modules/auth.md": `---
title: Auth Module
ownership: managed
---
# Auth Module

This module handles authentication using JWT tokens.
See [[modules/database]] for session storage.
`,
		"modules/database.md": `---
title: Database Module
ownership: managed
---
# Database Module

Manages PostgreSQL connections and queries.
See [[modules/auth]] for auth integration.
Uses connection pooling with a max of 10 connections.
`,
		"decisions/001.md": `---
title: ADR 001 - Use JWT
ownership: human-authored
---
# ADR 001: Use JWT for Authentication

We decided to use JWT tokens for stateless authentication.
Related: [[modules/auth]]
`,
	}

	for path, content := range pages {
		fullPath := filepath.Join(wikiDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	return wikiDir
}

func TestNewLLMAnalyzer(t *testing.T) {
	client := newMockLLMClient()
	analyzer := NewLLMAnalyzer(client, "/tmp/wiki")

	if analyzer.Client != client {
		t.Error("Expected client to be set")
	}
	if analyzer.WikiRoot != "/tmp/wiki" {
		t.Errorf("Expected WikiRoot '/tmp/wiki', got '%s'", analyzer.WikiRoot)
	}
	if analyzer.RateLimit != DefaultRateLimit {
		t.Errorf("Expected default rate limit %d, got %d", DefaultRateLimit, analyzer.RateLimit)
	}
}

func TestDetectContradictions_ParsesResponse(t *testing.T) {
	wikiDir := createTestWiki(t)

	client := newMockLLMClient()
	client.responses["contradictions"] = `CONTRADICTION: Auth module says JWT is stateless but database module implies session storage is required.
CONTRADICTION: Max connections stated as 10 but auth assumes unlimited connections.`

	analyzer := NewLLMAnalyzer(client, wikiDir)
	pages, err := analyzer.loadAllPages()
	if err != nil {
		t.Fatalf("loadAllPages() error: %v", err)
	}

	results, err := analyzer.detectContradictions(pages)
	if err != nil {
		t.Fatalf("detectContradictions() error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Expected contradictions but got none")
	}

	// Verify at least one contradiction was parsed
	found := false
	for _, r := range results {
		if strings.Contains(r.Description, "JWT is stateless") {
			found = true
			if len(r.Pages) != 2 {
				t.Errorf("Expected 2 pages in contradiction, got %d", len(r.Pages))
			}
		}
	}
	if !found {
		t.Error("Expected to find JWT contradiction in results")
	}
}

func TestDetectContradictions_HandlesNone(t *testing.T) {
	wikiDir := createTestWiki(t)

	client := newMockLLMClient()
	// Default mock response is "NONE"

	analyzer := NewLLMAnalyzer(client, wikiDir)
	pages, err := analyzer.loadAllPages()
	if err != nil {
		t.Fatalf("loadAllPages() error: %v", err)
	}

	results, err := analyzer.detectContradictions(pages)
	if err != nil {
		t.Fatalf("detectContradictions() error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 contradictions for NONE response, got %d", len(results))
	}
}

func TestSuggestMissingPages_ParsesConcepts(t *testing.T) {
	wikiDir := createTestWiki(t)

	client := newMockLLMClient()
	client.responses["concepts"] = `CONCEPT: Connection Pooling
CONCEPT: JWT Token Management
CONCEPT: Session Storage`

	analyzer := NewLLMAnalyzer(client, wikiDir)
	pages, err := analyzer.loadAllPages()
	if err != nil {
		t.Fatalf("loadAllPages() error: %v", err)
	}

	results, err := analyzer.suggestMissingPages(pages)
	if err != nil {
		t.Fatalf("suggestMissingPages() error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Expected 3 concepts, got %d", len(results))
	}

	expected := []string{"Connection Pooling", "JWT Token Management", "Session Storage"}
	for i, want := range expected {
		if results[i] != want {
			t.Errorf("Concept %d: got '%s', want '%s'", i, results[i], want)
		}
	}
}

func TestSuggestMissingPages_HandlesNone(t *testing.T) {
	wikiDir := createTestWiki(t)

	client := newMockLLMClient()
	// Default response is "NONE"

	analyzer := NewLLMAnalyzer(client, wikiDir)
	pages, err := analyzer.loadAllPages()
	if err != nil {
		t.Fatalf("loadAllPages() error: %v", err)
	}

	results, err := analyzer.suggestMissingPages(pages)
	if err != nil {
		t.Fatalf("suggestMissingPages() error: %v", err)
	}

	if results != nil {
		t.Errorf("Expected nil for NONE response, got %v", results)
	}
}

func TestSuggestCrossRefs_ParsesFormat(t *testing.T) {
	wikiDir := createTestWiki(t)

	client := newMockLLMClient()
	client.responses["cross-reference"] = `CROSSREF: [modules/auth.md] -> [decisions/001.md]: Both discuss JWT authentication
CROSSREF: [modules/database.md] -> [decisions/001.md]: Database stores JWT sessions`

	analyzer := NewLLMAnalyzer(client, wikiDir)
	pages, err := analyzer.loadAllPages()
	if err != nil {
		t.Fatalf("loadAllPages() error: %v", err)
	}

	results, err := analyzer.suggestCrossRefs(pages)
	if err != nil {
		t.Fatalf("suggestCrossRefs() error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 cross-refs, got %d", len(results))
	}

	if results[0].From != "modules/auth.md" {
		t.Errorf("Expected From 'modules/auth.md', got '%s'", results[0].From)
	}
	if results[0].ShouldLinkTo != "decisions/001.md" {
		t.Errorf("Expected ShouldLinkTo 'decisions/001.md', got '%s'", results[0].ShouldLinkTo)
	}
	if results[0].Reason != "Both discuss JWT authentication" {
		t.Errorf("Expected reason about JWT, got '%s'", results[0].Reason)
	}
}

func TestDetectSemanticStaleness_ParsesStaleResponse(t *testing.T) {
	wikiDir := createTestWiki(t)

	client := newMockLLMClient()
	client.responses["semantically outdated"] = `STALE: References deprecated JWT library v1 which has known vulnerabilities | CONFIDENCE: high`

	analyzer := NewLLMAnalyzer(client, wikiDir)
	pages, err := analyzer.loadAllPages()
	if err != nil {
		t.Fatalf("loadAllPages() error: %v", err)
	}

	results, err := analyzer.detectSemanticStaleness(pages)
	if err != nil {
		t.Fatalf("detectSemanticStaleness() error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Expected stale pages but got none")
	}

	found := false
	for _, r := range results {
		if strings.Contains(r.Description, "deprecated JWT library") {
			found = true
			if r.Confidence != "high" {
				t.Errorf("Expected confidence 'high', got '%s'", r.Confidence)
			}
		}
	}
	if !found {
		t.Error("Expected to find JWT library staleness in results")
	}
}

func TestDetectSemanticStaleness_HandlesCurrent(t *testing.T) {
	wikiDir := createTestWiki(t)

	client := newMockLLMClient()
	client.responses["semantically outdated"] = "CURRENT"

	analyzer := NewLLMAnalyzer(client, wikiDir)
	pages, err := analyzer.loadAllPages()
	if err != nil {
		t.Fatalf("loadAllPages() error: %v", err)
	}

	results, err := analyzer.detectSemanticStaleness(pages)
	if err != nil {
		t.Fatalf("detectSemanticStaleness() error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 stale pages for CURRENT response, got %d", len(results))
	}
}

func TestAnalyze_RespectsRateLimit(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	os.MkdirAll(filepath.Join(wikiDir, "modules"), 0755)

	// Create 10 pages
	for i := 0; i < 10; i++ {
		content := "---\ntitle: Page " + string(rune('A'+i)) + "\nownership: managed\n---\n# Page content\n"
		path := filepath.Join(wikiDir, "modules", "page"+string(rune('a'+i))+".md")
		os.WriteFile(path, []byte(content), 0644)
	}

	client := newMockLLMClient()
	analyzer := NewLLMAnalyzer(client, wikiDir)
	analyzer.RateLimit = 3

	result, err := analyzer.Analyze(nil)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}

	if result.PagesAnalyzed != 3 {
		t.Errorf("Expected 3 pages analyzed (rate limit), got %d", result.PagesAnalyzed)
	}
}

func TestAnalyze_CombinesResults(t *testing.T) {
	wikiDir := createTestWiki(t)

	client := newMockLLMClient()
	// Set up responses for each analysis type
	client.responses["contradictions"] = "CONTRADICTION: JWT claim conflicts between auth and decisions"
	client.responses["concepts"] = "CONCEPT: Connection Pooling"
	client.responses["cross-reference"] = "CROSSREF: [modules/auth.md] -> [decisions/001.md]: Related topic"
	client.responses["semantically outdated"] = "STALE: Uses deprecated pattern | CONFIDENCE: low"

	analyzer := NewLLMAnalyzer(client, wikiDir)
	analyzer.RateLimit = 0 // unlimited

	result, err := analyzer.Analyze(nil)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}

	if result.PagesAnalyzed != 3 {
		t.Errorf("Expected 3 pages analyzed, got %d", result.PagesAnalyzed)
	}

	// Verify all result sections are populated
	if len(result.SuggestedPages) == 0 {
		t.Error("Expected suggested pages")
	}
	if len(result.MissingCrossRefs) == 0 {
		t.Error("Expected missing cross-refs")
	}
	if len(result.SemanticStaleness) == 0 {
		t.Error("Expected semantic staleness results")
	}
}

func TestParseContradictions(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     int
	}{
		{"none", "NONE", 0},
		{"single", "CONTRADICTION: Conflicting statement found", 1},
		{"multiple", "CONTRADICTION: First conflict\nCONTRADICTION: Second conflict", 2},
		{"mixed lines", "Some preamble\nCONTRADICTION: Actual finding\nMore text", 1},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := parseContradictions(tt.response, "page1.md", "page2.md")
			if len(results) != tt.want {
				t.Errorf("parseContradictions() got %d results, want %d", len(results), tt.want)
			}
		})
	}
}

func TestParseConcepts(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     int
	}{
		{"none", "NONE", 0},
		{"single", "CONCEPT: Authentication", 1},
		{"multiple", "CONCEPT: Auth\nCONCEPT: Database\nCONCEPT: Config", 3},
		{"empty concept skipped", "CONCEPT: \nCONCEPT: Valid", 1},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := parseConcepts(tt.response)
			if len(results) != tt.want {
				t.Errorf("parseConcepts() got %d results, want %d", len(results), tt.want)
			}
		})
	}
}

func TestParseCrossRefs(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     int
	}{
		{"none", "NONE", 0},
		{"single", "CROSSREF: [auth.md] -> [db.md]: Related", 1},
		{"multiple", "CROSSREF: [a.md] -> [b.md]: Reason1\nCROSSREF: [c.md] -> [d.md]: Reason2", 2},
		{"bad format", "CROSSREF: no arrow here", 0},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := parseCrossRefs(tt.response)
			if len(results) != tt.want {
				t.Errorf("parseCrossRefs() got %d results, want %d", len(results), tt.want)
			}
		})
	}
}

func TestParseStaleness(t *testing.T) {
	tests := []struct {
		name       string
		response   string
		wantNil    bool
		wantConf   string
	}{
		{"current", "CURRENT", true, ""},
		{"stale high", "STALE: Outdated API | CONFIDENCE: high", false, "high"},
		{"stale medium", "STALE: Old pattern | CONFIDENCE: medium", false, "medium"},
		{"stale low", "STALE: Minor issue | CONFIDENCE: low", false, "low"},
		{"stale no confidence", "STALE: Missing confidence field", false, "medium"},
		{"stale bad confidence", "STALE: Something | CONFIDENCE: extreme", false, "medium"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseStaleness(tt.response, "test.md")
			if tt.wantNil {
				if result != nil {
					t.Error("Expected nil result for CURRENT")
				}
				return
			}
			if result == nil {
				t.Fatal("Expected non-nil result")
			}
			if result.Confidence != tt.wantConf {
				t.Errorf("Expected confidence '%s', got '%s'", tt.wantConf, result.Confidence)
			}
			if result.WikiPath != "test.md" {
				t.Errorf("Expected WikiPath 'test.md', got '%s'", result.WikiPath)
			}
		})
	}
}

func TestLoadPages_SkipsUnderscoreAndRaw(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")

	// Create various pages
	files := map[string]string{
		"modules/auth.md":  "---\ntitle: Auth\nownership: managed\n---\n# Auth\n",
		"_schema.md":       "---\ntitle: Schema\nownership: managed\n---\n# Schema\n",
		"_Sidebar.md":      "# Sidebar\n",
		"raw/imported.md":  "---\ntitle: Raw\nownership: managed\n---\n# Raw\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(wikiDir, path)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, []byte(content), 0644)
	}

	client := newMockLLMClient()
	analyzer := NewLLMAnalyzer(client, wikiDir)
	pages, err := analyzer.loadAllPages()
	if err != nil {
		t.Fatalf("loadAllPages() error: %v", err)
	}

	// Should only load modules/auth.md (skip _-prefixed and raw/)
	if len(pages) != 1 {
		t.Errorf("Expected 1 page (auth only), got %d", len(pages))
		for _, p := range pages {
			t.Logf("  loaded: %s", p.path)
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	// Simple sanity check
	text := "Hello world" // 11 chars
	tokens := estimateTokens(text)
	if tokens != 2 { // 11/4 = 2
		t.Errorf("Expected 2 tokens, got %d", tokens)
	}
}
