package hook

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
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
		Wiki:        config.Wiki{Root: ".wiki"},
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

// TestPreCommitHook_UnrelatedWikiEditDoesNotSatisfySourceChange covers
// the F7 audit finding: a source change staged alongside an unrelated
// wiki edit must not satisfy the freshness check.
func TestPreCommitHook_UnrelatedWikiEditDoesNotSatisfySourceChange(t *testing.T) {
	cfg := &config.Config{
		Sources: config.Sources{
			Include: []string{"src/**"},
		},
		Wiki:        config.Wiki{Root: ".wiki"},
		Enforcement: config.Enforcement{Strictness: "strict"},
	}
	root := t.TempDir()
	h := NewPreCommitHook(root, cfg)

	result, err := h.Run([]string{"src/auth.go", ".wiki/onboarding.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected allowed=false: onboarding.md is not mapped to src/auth.go")
	}
	if !result.WikiUpdated {
		t.Error("expected wikiUpdated=true (.wiki/ was staged)")
	}
	if result.WikiRelevant {
		t.Error("expected wikiRelevant=false (unrelated wiki page)")
	}
}

// TestPreCommitHook_RelevantWikiSatisfiesSourceChange covers the happy
// path: a staged wiki file whose basename matches a staged source file
// counts as relevant even without a manifest entry.
func TestPreCommitHook_RelevantWikiSatisfiesSourceChange(t *testing.T) {
	cfg := &config.Config{
		Sources: config.Sources{
			Include: []string{"src/**"},
		},
		Wiki: config.Wiki{Root: ".wiki"},
	}
	root := t.TempDir()
	h := NewPreCommitHook(root, cfg)

	result, err := h.Run([]string{"src/auth.go", ".wiki/modules/auth.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Errorf("expected allowed=true (basename match), got reason=%q", result.Reason)
	}
	if !result.WikiRelevant {
		t.Error("expected wikiRelevant=true via basename heuristic")
	}
}

// TestPreCommitHook_ManifestMappedWikiSatisfiesSourceChange covers the
// direct manifest mapping path.
func TestPreCommitHook_ManifestMappedWikiSatisfiesSourceChange(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{
		Sources: config.Sources{
			Include: []string{"src/**"},
		},
		Wiki: config.Wiki{Root: ".wiki"},
	}
	mgr, err := manifest.NewManager(manifest.DefaultPath(root))
	if err != nil {
		t.Fatalf("manifest manager: %v", err)
	}
	require := struct{ NoError func(cond bool, msg string) }{NoError: func(cond bool, msg string) {
		if !cond {
			t.Fatalf("%s", msg)
		}
	}}
	require.NoError(true, "")
	hash := "deadbeef"
	if err := mgr.Save(&manifest.Manifest{
		Version: 2,
		Pages: []manifest.PageEntry{
			{
				WikiPath:  "modules/auth.md",
				Ownership: "managed",
				SourceFiles: []manifest.SourceFile{
					{Path: "src/auth.go", Hash: hash, ValidatedHash: hash, LastValidatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	h := NewPreCommitHook(root, cfg)
	result, err := h.Run([]string{"src/auth.go", ".wiki/modules/auth.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Errorf("expected allowed=true via manifest mapping, got reason=%q", result.Reason)
	}
	if !result.WikiRelevant {
		t.Error("expected wikiRelevant=true via manifest mapping")
	}
}

// TestPreCommitHook_ExplicitDebtMarkAllowsCommit covers the explicit
// debt mechanism: PLEXIUM_WIKI_DEBT=1 lets an operator bypass the
// relevance check when wiki content will be updated manually.
func TestPreCommitHook_ExplicitDebtMarkAllowsCommit(t *testing.T) {
	t.Setenv("PLEXIUM_WIKI_DEBT", "1")
	cfg := &config.Config{
		Sources: config.Sources{
			Include: []string{"src/**"},
		},
		Wiki:        config.Wiki{Root: ".wiki"},
		Enforcement: config.Enforcement{Strictness: "strict"},
	}
	root := t.TempDir()
	h := NewPreCommitHook(root, cfg)

	// Source change staged, no wiki files staged, debt mark set.
	result, err := h.Run([]string{"src/auth.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed=true with PLEXIUM_WIKI_DEBT=1")
	}
	if result.WikiUpdated {
		t.Error("expected wikiUpdated=false (no wiki files staged)")
	}
	if !strings.Contains(result.Reason, "debt mark") {
		t.Errorf("expected reason to mention debt mark, got %q", result.Reason)
	}
}

// silence unused import in case the helper above is the only one.
var _ = filepath.Join
