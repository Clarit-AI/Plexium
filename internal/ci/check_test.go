package ci

import (
	"testing"

	"github.com/Clarit-AI/Plexium/internal/config"
)

func TestCICheck_IsSourceFile(t *testing.T) {
	c := NewCICheck("/tmp", nil)

	tests := []struct {
		path string
		want bool
	}{
		{"src/main.go", true},
		{"cmd/plexium/main.go", true},
		{"internal/hook/hook.go", true},
		{"pkg/utils/helper.go", true},
		{"lib/processor.py", true},
		{"app/server.ts", true},
		{".wiki/modules/main.md", false},
		{".github/workflows/ci.yml", false},
		{".plexium/config.yml", false},
		{"README.md", false},
		{"docs/guide.md", false},
		{"assets/logo.png", false},
	}

	for _, tt := range tests {
		got := c.isSourceFile(tt.path)
		if got != tt.want {
			t.Errorf("isSourceFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestCICheck_HasWikiChanges(t *testing.T) {
	c := NewCICheck("/tmp", nil)
	files := []string{"src/main.go", ".wiki/modules/main.md", "README.md"}
	if !c.hasWikiChanges(files) {
		t.Error("expected wiki changes detected")
	}

	c2 := NewCICheck("/tmp", nil)
	files2 := []string{"src/main.go", "README.md"}
	if c2.hasWikiChanges(files2) {
		t.Error("expected no wiki changes detected")
	}
}

func TestCICheck_HasWikiChanges_CustomRoot(t *testing.T) {
	cfg := &config.Config{
		Wiki: config.Wiki{Root: "docs/wiki"},
	}
	c := NewCICheck("/tmp", cfg)

	files := []string{"src/main.go", "docs/wiki/modules/main.md"}
	if !c.hasWikiChanges(files) {
		t.Error("expected wiki changes detected with custom root")
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		want    bool
	}{
		{"src/main.go", "src/**", true},
		{"cmd/plexium/main.go", "cmd/**", true},
		{"internal/hook/hook.go", "internal/**", true},
		{"README.md", "README.md", true},
		{"main.go", "*.go", true},
		{"docs/guide.md", "src/**", false},
		{"test.txt", "*.go", false},
	}

	for _, tt := range tests {
		got := matchPattern(tt.path, tt.pattern)
		if got != tt.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.want)
		}
	}
}

func TestCheckResult_ToJSON(t *testing.T) {
	result := &CheckResult{
		Commit:      "abc123",
		BaseSHA:     "base456",
		HeadSHA:     "head789",
		Passes:      true,
		WikiUpdated: false,
		DebtCount:   0,
	}

	data, err := result.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty JSON output")
	}
}

func TestCICheck_FilterSourceFiles(t *testing.T) {
	cfg := &config.Config{
		Sources: config.Sources{
			Include: []string{"internal/**", "cmd/**"},
			Exclude: []string{"*.txt"},
		},
	}
	c := NewCICheck("/tmp", cfg)

	files := []string{
		"internal/hook/hook.go",
		"internal/hook/testdata/fixture.txt",
		"cmd/plexium/main.go",
		"README.md",
		"docs/guide.md",
	}

	filtered := c.filterSourceFiles(files)
	if len(filtered) != 2 {
		t.Errorf("expected 2 filtered source files, got %d: %v", len(filtered), filtered)
	}

	found := map[string]bool{}
	for _, f := range filtered {
		found[f] = true
	}
	if !found["internal/hook/hook.go"] {
		t.Error("expected internal/hook/hook.go in filtered results")
	}
	if !found["cmd/plexium/main.go"] {
		t.Error("expected cmd/plexium/main.go in filtered results")
	}
}
