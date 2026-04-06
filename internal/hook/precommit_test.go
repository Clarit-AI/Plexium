package hook

import (
	"testing"

	"github.com/Clarit-AI/Plexium/internal/config"
)

func TestPreCommitHook_ExplicitlyNoFiles(t *testing.T) {
	h := NewPreCommitHook(t.TempDir(), nil)
	result, err := h.Run([]string{}) // explicit empty = no staged files
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed=true with explicitly empty files")
	}
	if !result.Skipped {
		t.Error("expected skipped=true with no files")
	}
}

func TestPreCommitHook_BypassEnv(t *testing.T) {
	t.Setenv("PLEXIUM_BYPASS_HOOK", "1")
	h := NewPreCommitHook(t.TempDir(), nil)
	result, err := h.Run(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed=true with bypass env")
	}
	if !result.Skipped {
		t.Error("expected skipped=true with bypass env")
	}
}

func TestPreCommitHook_SourceFilesWithWikiUpdate(t *testing.T) {
	cfg := &config.Config{
		Sources: config.Sources{
			Include: []string{"src/**"},
		},
		Wiki: config.Wiki{Root: ".wiki"},
	}
	h := NewPreCommitHook(t.TempDir(), cfg)
	result, err := h.Run([]string{"src/main.go", ".wiki/modules/main.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed=true when wiki updated")
	}
	if !result.WikiUpdated {
		t.Error("expected wikiUpdated=true")
	}
}

func TestPreCommitHook_SourceFilesWithoutWiki_Strict(t *testing.T) {
	cfg := &config.Config{
		Sources: config.Sources{
			Include: []string{"src/**"},
		},
		Wiki: config.Wiki{Root: ".wiki"},
		Enforcement: config.Enforcement{
			Strictness: "strict",
		},
	}
	h := NewPreCommitHook(t.TempDir(), cfg)
	result, err := h.Run([]string{"src/main.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected allowed=false in strict mode without wiki update")
	}
	if result.Strictness != "strict" {
		t.Errorf("expected strictness=strict, got %s", result.Strictness)
	}
}

func TestPreCommitHook_SourceFilesWithoutWiki_Moderate(t *testing.T) {
	cfg := &config.Config{
		Sources: config.Sources{
			Include: []string{"src/**"},
		},
		Wiki: config.Wiki{Root: ".wiki"},
		Enforcement: config.Enforcement{
			Strictness: "moderate",
		},
	}
	h := NewPreCommitHook(t.TempDir(), cfg)
	result, err := h.Run([]string{"src/main.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected allowed=false in moderate mode without wiki update")
	}
}

func TestPreCommitHook_SourceFilesWithoutWiki_Advisory(t *testing.T) {
	cfg := &config.Config{
		Sources: config.Sources{
			Include: []string{"src/**"},
		},
		Wiki: config.Wiki{Root: ".wiki"},
		Enforcement: config.Enforcement{
			Strictness: "advisory",
		},
	}
	h := NewPreCommitHook(t.TempDir(), cfg)
	result, err := h.Run([]string{"src/main.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed=true in advisory mode (warn only)")
	}
	if result.Reason == "" {
		t.Error("expected reason to be set even in advisory mode")
	}
}

func TestPreCommitHook_NonSourceFiles(t *testing.T) {
	cfg := &config.Config{
		Sources: config.Sources{
			Include: []string{"src/**"},
		},
		Wiki: config.Wiki{Root: ".wiki"},
	}
	h := NewPreCommitHook(t.TempDir(), cfg)
	result, err := h.Run([]string{"README.md", "docs/guide.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed=true with non-source files")
	}
	if !result.Skipped {
		t.Error("expected skipped=true with non-source files")
	}
}

func TestPreCommitHook_ExcludePatterns(t *testing.T) {
	cfg := &config.Config{
		Sources: config.Sources{
			Include: []string{"**/*.go"},
			Exclude: []string{"**/*_test.go"},
		},
		Wiki: config.Wiki{Root: ".wiki"},
		Enforcement: config.Enforcement{Strictness: "strict"},
	}
	h := NewPreCommitHook(t.TempDir(), cfg)

	// Test file should be excluded
	result, err := h.Run([]string{"internal/hook/hook_test.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed=true for excluded test file")
	}
}

func TestPreCommitHook_DefaultSourceDirs(t *testing.T) {
	h := NewPreCommitHook(t.TempDir(), nil)

	// Without config, should still detect common source dirs
	result, err := h.Run([]string{"cmd/plexium/main.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.FilesChanged) == 0 {
		t.Error("expected files changed for cmd/ file")
	}
}

func TestPreCommitHook_HasWikiChanges_CustomRoot(t *testing.T) {
	cfg := &config.Config{
		Wiki: config.Wiki{Root: "docs/wiki"},
	}
	h := NewPreCommitHook(t.TempDir(), cfg)

	result, err := h.Run([]string{"src/main.go", "docs/wiki/modules/main.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.WikiUpdated {
		t.Error("expected wikiUpdated=true with custom wiki root")
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		want    bool
	}{
		{"src/main.go", "src/**", true},
		{"cmd/plexium/main.go", "cmd/**", true},
		{"internal/hook/hook.go", "internal/**", true},
		{"src/main.go", "*.go", true},
		{"src/main_test.go", "*.go", true},
		{"docs/guide.md", "src/**", false},
		{"docs/guide.md", "*.go", false},
	}

	for _, tt := range tests {
		got := matchGlob(tt.path, tt.pattern)
		if got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.want)
		}
	}
}
