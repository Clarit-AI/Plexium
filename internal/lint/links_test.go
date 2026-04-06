package lint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinkCrawler_Crawl(t *testing.T) {
	// Create temp wiki structure
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	os.MkdirAll(wikiDir, 0755)

	// Create test pages
	pages := map[string]string{
		"modules/auth.md":        "# Auth Module\nSee [[modules/database]] and [[decisions/001]].\n",
		"modules/database.md":    "# Database Module\nRelated to [[modules/auth]].\n",
		"decisions/001.md":       "# ADR 001\nUse [[modules/auth]] here.\n",
		"_Sidebar.md":           "# Sidebar\n- [[modules/auth]]\n- [[modules/database]]\n",
	}

	for path, content := range pages {
		fullPath := filepath.Join(wikiDir, path)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, []byte(content), 0644)
	}

	crawler := NewLinkCrawler(wikiDir)
	links, err := crawler.Crawl()
	if err != nil {
		t.Fatalf("Crawl() error = %v", err)
	}

	// Should find links from modules/auth.md, modules/database.md, decisions/001.md
	if len(links) == 0 {
		t.Fatal("Expected to find links, got none")
	}

	// Check for broken link (decisions/002 doesn't exist)
	var brokenCount int
	for _, l := range links {
		if !l.Resolved {
			brokenCount++
		}
	}
	// We have no broken links in our test data since all targets exist
	if brokenCount != 0 {
		t.Errorf("Expected 0 broken links, got %d", brokenCount)
	}
}

func TestLinkCrawler_ResolveLink(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	os.MkdirAll(wikiDir, 0755)

	// Create a page
	authPath := filepath.Join(wikiDir, "modules/auth.md")
	os.MkdirAll(filepath.Dir(authPath), 0755)
	os.WriteFile(authPath, []byte("# Auth"), 0644)

	crawler := NewLinkCrawler(wikiDir)

	tests := []struct {
		link      string
		sourceDir string
		want      string
		exists    bool
	}{
		{"modules/auth", "", "modules/auth.md", true},
		{"auth", "modules", "modules/auth.md", true},
		{"modules/auth#heading", "", "modules/auth.md#heading", true},
		{"nonexistent", "", "nonexistent.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.link, func(t *testing.T) {
			got, exists := crawler.ResolveLink(tt.link, tt.sourceDir)
			if got != tt.want {
				t.Errorf("ResolveLink() got = %v, want %v", got, tt.want)
			}
			if exists != tt.exists {
				t.Errorf("ResolveLink() exists = %v, want %v", exists, tt.exists)
			}
		})
	}
}

func TestLinkCrawler_GetBrokenLinks(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	os.MkdirAll(wikiDir, 0755)

	// Create a page with broken link
	content := "# Auth\nSee [[modules/nonexistent]].\n"
	authPath := filepath.Join(wikiDir, "modules/auth.md")
	os.MkdirAll(filepath.Dir(authPath), 0755)
	os.WriteFile(authPath, []byte(content), 0644)

	crawler := NewLinkCrawler(wikiDir)
	broken, err := crawler.GetBrokenLinks()
	if err != nil {
		t.Fatalf("GetBrokenLinks() error = %v", err)
	}

	if len(broken) != 1 {
		t.Fatalf("Expected 1 broken link, got %d", len(broken))
	}

	if broken[0].Target != "modules/nonexistent.md" {
		t.Errorf("Expected target 'modules/nonexistent.md', got '%s'", broken[0].Target)
	}
}
