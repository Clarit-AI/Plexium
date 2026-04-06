package pageindex

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTestWiki creates a temporary wiki directory with test pages.
func setupTestWiki(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")

	pages := map[string]string{
		"modules/auth.md": `---
title: "Authentication Module"
ownership: managed
last-updated: "2025-01-15"
---

# Authentication Module

Handles user authentication and session management.

See also [[modules/database]] for persistence and [[decisions/001-auth-strategy]].
`,
		"modules/database.md": `---
title: "Database Layer"
ownership: managed
---

# Database Layer

Provides database connection pooling and query abstraction.

Related: [[modules/auth]]
`,
		"decisions/001-auth-strategy.md": `---
title: "ADR 001: Auth Strategy"
ownership: human-authored
---

# ADR 001: Auth Strategy

We chose JWT-based authentication for stateless operation.

See [[modules/auth]] for implementation.
`,
		"concepts/wiki-pattern.md": `---
title: "Wiki Pattern"
ownership: managed
---

# Wiki Pattern

The LLM wiki pattern enables continuous documentation.
`,
		"_index.md": `---
title: Index
ownership: managed
---

# Wiki Index

Total pages: 4

## concepts

- [[concepts/wiki-pattern.md|Wiki Pattern]]: The LLM wiki pattern

## decisions

- [[decisions/001-auth-strategy.md|ADR 001: Auth Strategy]]: JWT auth

## modules

- [[modules/auth.md|Authentication Module]]: Handles auth
- [[modules/database.md|Database Layer]]: Database pooling
`,
		"_Sidebar.md": `---
title: Sidebar
---

# Navigation

## Modules

- [[modules/auth.md|Authentication Module]]
- [[modules/database.md|Database Layer]]

## Decisions

- [[decisions/001-auth-strategy.md|ADR 001]]
`,
	}

	for path, content := range pages {
		fullPath := filepath.Join(wikiDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("creating dir for %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}

	return wikiDir
}

func TestNew(t *testing.T) {
	idx := New("/some/path/.wiki")
	if idx.WikiRoot != "/some/path/.wiki" {
		t.Errorf("WikiRoot = %q, want %q", idx.WikiRoot, "/some/path/.wiki")
	}
	if idx.loaded {
		t.Error("new index should not be loaded")
	}
	if idx.pages != nil {
		t.Error("new index should have nil pages")
	}
}

func TestLoad(t *testing.T) {
	wikiDir := setupTestWiki(t)
	idx := New(wikiDir)

	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !idx.loaded {
		t.Error("index should be loaded after Load()")
	}

	pages := idx.ListPages()
	if len(pages) == 0 {
		t.Fatal("expected pages after Load(), got none")
	}

	// Should have all 6 pages (4 content + _index.md + _Sidebar.md)
	if len(pages) != 6 {
		t.Errorf("expected 6 pages, got %d", len(pages))
		for _, p := range pages {
			t.Logf("  page: %s (section=%s, title=%s)", p.Path, p.Section, p.Title)
		}
	}

	// Verify a specific page was parsed correctly
	var authPage *PageInfo
	for i, p := range pages {
		if p.Path == filepath.Join("modules", "auth.md") {
			authPage = &pages[i]
			break
		}
	}

	if authPage == nil {
		t.Fatal("modules/auth.md not found in index")
	}

	if authPage.Title != "Authentication Module" {
		t.Errorf("auth title = %q, want %q", authPage.Title, "Authentication Module")
	}

	if authPage.Section != "modules" {
		t.Errorf("auth section = %q, want %q", authPage.Section, "modules")
	}

	if authPage.Summary == "" {
		t.Error("auth summary should not be empty")
	}

	if len(authPage.Links) != 2 {
		t.Errorf("auth links count = %d, want 2", len(authPage.Links))
	}
}

func TestLoad_NonexistentDir(t *testing.T) {
	idx := New("/nonexistent/path/.wiki")
	err := idx.Load()
	if err == nil {
		t.Error("Load() should error on nonexistent directory")
	}
}

func TestLoad_SkipsRawDir(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")

	// Create a page in raw/ that should be skipped
	os.MkdirAll(filepath.Join(wikiDir, "raw"), 0755)
	os.WriteFile(filepath.Join(wikiDir, "raw", "notes.md"), []byte("# Raw\n"), 0644)

	// Create a normal page
	os.MkdirAll(filepath.Join(wikiDir, "modules"), 0755)
	os.WriteFile(filepath.Join(wikiDir, "modules", "test.md"), []byte("---\ntitle: Test\nownership: managed\n---\n\n# Test\n"), 0644)

	idx := New(wikiDir)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	pages := idx.ListPages()
	for _, p := range pages {
		if p.Section == "raw" {
			t.Error("raw/ directory should be skipped")
		}
	}
}

func TestLoad_SkipsHiddenFiles(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")

	os.MkdirAll(wikiDir, 0755)
	os.WriteFile(filepath.Join(wikiDir, ".hidden.md"), []byte("# Hidden\n"), 0644)
	os.WriteFile(filepath.Join(wikiDir, "visible.md"), []byte("# Visible\n"), 0644)

	idx := New(wikiDir)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	pages := idx.ListPages()
	for _, p := range pages {
		if p.Path == ".hidden.md" {
			t.Error("hidden files should be skipped")
		}
	}

	if len(pages) != 1 {
		t.Errorf("expected 1 page, got %d", len(pages))
	}
}

func TestSearch_TitleMatch(t *testing.T) {
	wikiDir := setupTestWiki(t)
	idx := New(wikiDir)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	results := idx.Search("authentication")
	if len(results) == 0 {
		t.Fatal("expected search results for 'authentication', got none")
	}

	// The auth module should be the top hit (title match)
	top := results[0]
	if top.Page.Title != "Authentication Module" {
		t.Errorf("top result title = %q, want %q", top.Page.Title, "Authentication Module")
	}

	if top.Score != 1.0 {
		t.Errorf("top result score = %f, want 1.0", top.Score)
	}

	if top.MatchType != "title" {
		t.Errorf("top match type = %q, want %q", top.MatchType, "title")
	}
}

func TestSearch_ContentMatch(t *testing.T) {
	wikiDir := setupTestWiki(t)
	idx := New(wikiDir)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	results := idx.Search("JWT")
	if len(results) == 0 {
		t.Fatal("expected search results for 'JWT', got none")
	}

	// Should find the ADR about JWT
	found := false
	for _, r := range results {
		if r.Page.Path == filepath.Join("decisions", "001-auth-strategy.md") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find decisions/001-auth-strategy.md in results for 'JWT'")
	}
}

func TestSearch_SectionMatch(t *testing.T) {
	wikiDir := setupTestWiki(t)
	idx := New(wikiDir)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	results := idx.Search("modules")
	if len(results) == 0 {
		t.Fatal("expected search results for 'modules', got none")
	}

	// Pages in the modules section should appear
	var modulePagesFound int
	for _, r := range results {
		if r.Page.Section == "modules" {
			modulePagesFound++
		}
	}
	if modulePagesFound < 2 {
		t.Errorf("expected at least 2 module pages, got %d", modulePagesFound)
	}
}

func TestSearch_NoMatches(t *testing.T) {
	wikiDir := setupTestWiki(t)
	idx := New(wikiDir)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	results := idx.Search("zzzznonexistentterm")
	if len(results) != 0 {
		t.Errorf("expected 0 results for nonexistent term, got %d", len(results))
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	wikiDir := setupTestWiki(t)
	idx := New(wikiDir)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	results := idx.Search("")
	if results != nil {
		t.Errorf("expected nil for empty query, got %d results", len(results))
	}
}

func TestSearch_UnloadedIndex(t *testing.T) {
	idx := New("/whatever")
	results := idx.Search("test")
	if results != nil {
		t.Error("expected nil from unloaded index")
	}
}

func TestGetPage(t *testing.T) {
	wikiDir := setupTestWiki(t)
	idx := New(wikiDir)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	page, err := idx.GetPage(filepath.Join("modules", "auth.md"))
	if err != nil {
		t.Fatalf("GetPage() error = %v", err)
	}

	if page.Info.Title != "Authentication Module" {
		t.Errorf("page title = %q, want %q", page.Info.Title, "Authentication Module")
	}

	if page.Content == "" {
		t.Error("page content should not be empty")
	}

	if !containsString(page.Content, "user authentication") {
		t.Error("page content should contain 'user authentication'")
	}
}

func TestGetPage_NotFound(t *testing.T) {
	wikiDir := setupTestWiki(t)
	idx := New(wikiDir)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	_, err := idx.GetPage("nonexistent.md")
	if err == nil {
		t.Error("GetPage() should error for nonexistent page")
	}
}

func TestListPages(t *testing.T) {
	wikiDir := setupTestWiki(t)
	idx := New(wikiDir)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	pages := idx.ListPages()
	if len(pages) != 6 {
		t.Errorf("ListPages() returned %d pages, want 6", len(pages))
	}

	// Verify it's a copy (modifying returned slice shouldn't affect index)
	origLen := len(idx.pages)
	pages = append(pages, PageInfo{Path: "extra.md"})
	if len(idx.pages) != origLen {
		t.Error("ListPages should return a copy, not the internal slice")
	}
}

func TestListPages_Unloaded(t *testing.T) {
	idx := New("/whatever")
	pages := idx.ListPages()
	if pages != nil {
		t.Error("ListPages on unloaded index should return nil")
	}
}

func TestListSections(t *testing.T) {
	wikiDir := setupTestWiki(t)
	idx := New(wikiDir)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	sections := idx.ListSections()
	if len(sections) == 0 {
		t.Fatal("expected sections, got none")
	}

	// Should include modules, decisions, concepts
	expected := map[string]bool{"modules": false, "decisions": false, "concepts": false}
	for _, s := range sections {
		if _, ok := expected[s]; ok {
			expected[s] = true
		}
	}

	for section, found := range expected {
		if !found {
			t.Errorf("expected section %q not found in %v", section, sections)
		}
	}

	// Should be sorted
	for i := 1; i < len(sections); i++ {
		if sections[i-1] > sections[i] {
			t.Errorf("sections not sorted: %v", sections)
			break
		}
	}
}

func TestListSections_Unloaded(t *testing.T) {
	idx := New("/whatever")
	sections := idx.ListSections()
	if sections != nil {
		t.Error("ListSections on unloaded index should return nil")
	}
}

func TestExtractSummary(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "paragraph after heading",
			body: "# Title\n\nThis is the summary paragraph.\n\nMore content.",
			want: "This is the summary paragraph.",
		},
		{
			name: "multi-line paragraph",
			body: "# Title\n\nFirst line of summary.\nSecond line of summary.\n\nNext paragraph.",
			want: "First line of summary. Second line of summary.",
		},
		{
			name: "no heading",
			body: "Just content without a heading.\n\nMore stuff.",
			want: "Just content without a heading.",
		},
		{
			name: "empty body",
			body: "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSummary(tt.body)
			if got != tt.want {
				t.Errorf("extractSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractWikiLinks(t *testing.T) {
	body := "See [[modules/auth]] and [[modules/database|Database]]. Also [[modules/auth]] again."
	links := extractWikiLinks(body)

	if len(links) != 2 {
		t.Errorf("expected 2 unique links, got %d: %v", len(links), links)
	}

	if links[0] != "modules/auth" {
		t.Errorf("first link = %q, want %q", links[0], "modules/auth")
	}

	if links[1] != "modules/database" {
		t.Errorf("second link = %q, want %q", links[1], "modules/database")
	}
}

func TestExtractWikiLinks_NoLinks(t *testing.T) {
	links := extractWikiLinks("No links here.")
	if len(links) != 0 {
		t.Errorf("expected 0 links, got %d", len(links))
	}
}

// containsString checks if s contains substr.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
