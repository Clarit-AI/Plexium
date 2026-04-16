package regen

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/internal/agent"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/retry"
)

// ---------------------------------------------------------------------------
// Mock provider
// ---------------------------------------------------------------------------

type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) Name() string                        { return "mock" }
func (m *mockProvider) IsAvailable() bool                   { return true }
func (m *mockProvider) HealthCheck() error                  { return nil }
func (m *mockProvider) CostPerToken() float64               { return 0.001 }
func (m *mockProvider) Complete(_ context.Context, _ string) (*agent.CompletionResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &agent.CompletionResult{
		Provider:   "mock",
		Response:   m.response,
		TokensUsed: 100,
		CostUSD:    0.01,
		LatencyMs:  50,
	}, nil
}

func testCascade(response string) *agent.ProviderCascade {
	rp := &retry.RetryPolicy{
		MaxAttempts:       1,
		InitialDelay:      time.Millisecond,
		BackoffMultiplier: 1.0,
		MaxDelay:          time.Millisecond,
	}
	return agent.NewCascade([]agent.Provider{&mockProvider{response: response}}, rp)
}

func testCascadeErr(err error) *agent.ProviderCascade {
	rp := &retry.RetryPolicy{
		MaxAttempts:       1,
		InitialDelay:      time.Millisecond,
		BackoffMultiplier: 1.0,
		MaxDelay:          time.Millisecond,
	}
	return agent.NewCascade([]agent.Provider{&mockProvider{err: err}}, rp)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRegeneratePage_JSONResponse(t *testing.T) {
	tmp := t.TempDir()
	wikiDir := filepath.Join(tmp, ".wiki", "modules")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	jsonResp := `{"content": "# Auth Module\nUpdated content"}`
	result, err := RegeneratePage(context.Background(), PageRegenOptions{
		RepoRoot: tmp,
		WikiRoot: ".wiki",
		Page: manifest.PageEntry{
			WikiPath: "modules/auth.md",
		},
		Cascade: testCascade(jsonResp),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Written {
		t.Fatal("expected Written=true")
	}
	if result.Provider != "mock" {
		t.Errorf("expected provider=mock, got %q", result.Provider)
	}

	data, err := os.ReadFile(filepath.Join(tmp, ".wiki", "modules", "auth.md"))
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	expected := "# Auth Module\nUpdated content"
	if string(data) != expected {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", string(data), expected)
	}
}

func TestRegeneratePage_RawMarkdownFallback(t *testing.T) {
	tmp := t.TempDir()

	rawMD := "# Raw Module\nThis is raw markdown"
	result, err := RegeneratePage(context.Background(), PageRegenOptions{
		RepoRoot: tmp,
		WikiRoot: ".wiki",
		Page: manifest.PageEntry{
			WikiPath: "modules/raw.md",
		},
		Cascade: testCascade(rawMD),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Written {
		t.Fatal("expected Written=true")
	}

	data, err := os.ReadFile(filepath.Join(tmp, ".wiki", "modules", "raw.md"))
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	if string(data) != rawMD {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", string(data), rawMD)
	}
}

func TestRegeneratePage_NilCascade(t *testing.T) {
	_, err := RegeneratePage(context.Background(), PageRegenOptions{
		RepoRoot: t.TempDir(),
		WikiRoot: ".wiki",
		Page: manifest.PageEntry{
			WikiPath: "modules/test.md",
		},
		Cascade: nil,
	})
	if err == nil {
		t.Fatal("expected error for nil cascade")
	}
	if !strings.Contains(err.Error(), "cascade must not be nil") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRegeneratePage_PathSafety(t *testing.T) {
	cases := []struct {
		name     string
		wikiPath string
	}{
		{"dotdot traversal", "../etc/passwd"},
		{"nested dotdot", "modules/../../etc/passwd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RegeneratePage(context.Background(), PageRegenOptions{
				RepoRoot: t.TempDir(),
				WikiRoot: ".wiki",
				Page: manifest.PageEntry{
					WikiPath: tc.wikiPath,
				},
				Cascade: testCascade(`{"content":"hack"}`),
			})
			if err == nil {
				t.Fatal("expected path safety error")
			}
			if !strings.Contains(err.Error(), "escapes wiki root") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestBuildRegenPrompt_IncludesSourceContent(t *testing.T) {
	tmp := t.TempDir()

	// Write a source file.
	srcDir := filepath.Join(tmp, "internal", "auth")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	srcContent := "package auth\n\nfunc Login() {}\n"
	if err := os.WriteFile(filepath.Join(srcDir, "auth.go"), []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}

	page := manifest.PageEntry{
		WikiPath: "modules/auth.md",
		SourceFiles: []manifest.SourceFile{
			{Path: "internal/auth/auth.go"},
		},
	}

	prompt := BuildRegenPrompt(nil, page, tmp, ".wiki", nil)

	if !strings.Contains(prompt, "package auth") {
		t.Error("prompt should contain source file content")
	}
	if !strings.Contains(prompt, "internal/auth/auth.go") {
		t.Error("prompt should contain source file path")
	}
}

func TestBuildRegenPrompt_IncludesExistingPage(t *testing.T) {
	tmp := t.TempDir()

	// Write an existing wiki page.
	wikiDir := filepath.Join(tmp, ".wiki", "modules")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "# Auth\nExisting documentation here."
	if err := os.WriteFile(filepath.Join(wikiDir, "auth.md"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	page := manifest.PageEntry{
		WikiPath: "modules/auth.md",
	}

	prompt := BuildRegenPrompt(nil, page, tmp, ".wiki", nil)

	if !strings.Contains(prompt, "Existing documentation here") {
		t.Error("prompt should contain existing wiki page content")
	}
	if !strings.Contains(prompt, "Existing wiki page content") {
		t.Error("prompt should have existing page heading")
	}
}

func TestTruncateString(t *testing.T) {
	cases := []struct {
		input    string
		max      int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "hel"},
		{"", 5, ""},
		{"abcdef", 0, ""},
		// Unicode: each rune counts as 1.
		{"日本語テスト", 3, "日本語"},
	}
	for _, tc := range cases {
		got := truncateString(tc.input, tc.max)
		if got != tc.expected {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tc.input, tc.max, got, tc.expected)
		}
	}
}

func TestConfiguredWikiRoot(t *testing.T) {
	if got := configuredWikiRoot(nil); got != ".wiki" {
		t.Errorf("nil config: got %q, want .wiki", got)
	}

	cfg := &config.Config{}
	if got := configuredWikiRoot(cfg); got != ".wiki" {
		t.Errorf("empty config: got %q, want .wiki", got)
	}

	cfg.Wiki.Root = "docs/wiki"
	if got := configuredWikiRoot(cfg); got != "docs/wiki" {
		t.Errorf("custom root: got %q, want docs/wiki", got)
	}
}

func TestIsPathWithinRoot(t *testing.T) {
	cases := []struct {
		path, root string
		want       bool
	}{
		{".wiki/modules/auth.md", ".wiki", true},
		{".wiki", ".wiki", true},
		{"../etc/passwd", ".wiki", false},
		{".wiki/../../etc", ".wiki", false},
		{"other/file.md", ".wiki", false},
	}
	for _, tc := range cases {
		got := isPathWithinRoot(tc.path, tc.root)
		if got != tc.want {
			t.Errorf("isPathWithinRoot(%q, %q) = %v, want %v", tc.path, tc.root, got, tc.want)
		}
	}
}
