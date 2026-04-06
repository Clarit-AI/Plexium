package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/manifest"
)

func TestOrphanDetector_Detect(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	os.MkdirAll(wikiDir, 0755)

	// Create manifest
	manifestDir := filepath.Join(tmpDir, ".plexium")
	os.MkdirAll(manifestDir, 0755)
	manifestPath := filepath.Join(manifestDir, "manifest.json")

	// Create a manifest with pages
	m := manifest.NewEmptyManifest()
	m.Pages = []manifest.PageEntry{
		{WikiPath: "modules/auth.md", Title: "Auth", Ownership: "managed"},
		{WikiPath: "modules/database.md", Title: "Database", Ownership: "managed"},
		{WikiPath: "orphan.md", Title: "Orphan", Ownership: "managed"},
	}

	mgr, err := manifest.NewManager(manifestPath)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if err := mgr.Save(m); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Create wiki pages
	os.WriteFile(filepath.Join(wikiDir, "modules/auth.md"), []byte("# Auth\nSee [[modules/database]].\n"), 0644)
	os.WriteFile(filepath.Join(wikiDir, "modules/database.md"), []byte("# Database\nSee [[modules/auth]].\n"), 0644)
	os.WriteFile(filepath.Join(wikiDir, "orphan.md"), []byte("# Orphan\nThis page has no links to it.\n"), 0644)
	os.WriteFile(filepath.Join(wikiDir, "_Sidebar.md"), []byte("# Sidebar\n- [[Home]]\n"), 0644)
	os.WriteFile(filepath.Join(wikiDir, "Home.md"), []byte("# Home\n"), 0644)

	detector := NewOrphanDetector(wikiDir, mgr)
	result, err := detector.Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	// orphan.md should be an orphan (error severity)
	// _Sidebar.md, Home.md are navigation hubs and should be excluded
	foundOrphan := false
	for _, o := range result.Orphans {
		if o.WikiPath == "orphan.md" {
			foundOrphan = true
			if o.Severity != "error" {
				t.Errorf("Expected orphan.md severity 'error', got '%s'", o.Severity)
			}
		}
	}

	if !foundOrphan {
		t.Error("Expected orphan.md to be detected as orphan")
	}
}

func TestOrphanDetector_SidebarReachable(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	os.MkdirAll(wikiDir, 0755)

	// Create manifest
	manifestDir := filepath.Join(tmpDir, ".plexium")
	os.MkdirAll(manifestDir, 0755)
	manifestPath := filepath.Join(manifestDir, "manifest.json")

	m := manifest.NewEmptyManifest()
	m.Pages = []manifest.PageEntry{
		{WikiPath: "reachable.md", Title: "Reachable", Ownership: "managed"},
	}

	mgr, _ := manifest.NewManager(manifestPath)
	mgr.Save(m)

	// Create pages - reachable.md has no inbound links but IS in sidebar
	os.WriteFile(filepath.Join(wikiDir, "reachable.md"), []byte("# Reachable\nSidebar page.\n"), 0644)
	os.WriteFile(filepath.Join(wikiDir, "_Sidebar.md"), []byte("# Sidebar\n- [[reachable]]\n"), 0644)
	os.WriteFile(filepath.Join(wikiDir, "Home.md"), []byte("# Home\n"), 0644)

	detector := NewOrphanDetector(wikiDir, mgr)
	result, err := detector.Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	// reachable.md should be a warning (has sidebar link but no inbound)
	for _, o := range result.Orphans {
		if o.WikiPath == "reachable.md" {
			if o.Severity != "warning" {
				t.Errorf("Expected severity 'warning', got '%s'", o.Severity)
			}
			if o.Reason != "no inbound links but reachable from sidebar" {
				t.Errorf("Expected reason 'no inbound links but reachable from sidebar', got '%s'", o.Reason)
			}
		}
	}
}
