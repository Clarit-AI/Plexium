package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostCommitHook_AppendDebtEntry(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	os.MkdirAll(wikiDir, 0755)

	h := NewPostCommitHook(tmpDir, ".wiki")

	entry := WikiDebtEntry{
		Date:       "2026-04-06",
		CommitSHA:  "abc1234",
		Files:      []string{"src/main.go", "src/util.go"},
		BypassedBy: "developer (--no-verify)",
		Status:     "pending wiki update",
	}

	err := h.appendDebtEntry(entry)
	if err != nil {
		t.Fatalf("appendDebtEntry failed: %v", err)
	}

	// Verify _log.md was created
	logPath := filepath.Join(wikiDir, "_log.md")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading _log.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "WIKI-DEBT") {
		t.Error("expected WIKI-DEBT in log")
	}
	if !strings.Contains(content, "abc1234") {
		t.Error("expected commit SHA in log")
	}
	if !strings.Contains(content, "src/main.go") {
		t.Error("expected file path in log")
	}
	if !strings.Contains(content, "pending wiki update") {
		t.Error("expected status in log")
	}
}

func TestPostCommitHook_AppendMultipleEntries(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	os.MkdirAll(wikiDir, 0755)

	h := NewPostCommitHook(tmpDir, ".wiki")

	for i := 0; i < 3; i++ {
		entry := WikiDebtEntry{
			Date:       "2026-04-06",
			CommitSHA:  "abc123" + string(rune('0'+i)),
			Files:      []string{"src/file.go"},
			BypassedBy: "developer (--no-verify)",
			Status:     "pending wiki update",
		}
		if err := h.appendDebtEntry(entry); err != nil {
			t.Fatalf("appendDebtEntry %d failed: %v", i, err)
		}
	}

	logPath := filepath.Join(wikiDir, "_log.md")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading _log.md: %v", err)
	}

	content := string(data)
	count := strings.Count(content, "WIKI-DEBT")
	if count != 3 {
		t.Errorf("expected 3 WIKI-DEBT entries, got %d", count)
	}
}

func TestGetDebtEntries(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	os.MkdirAll(wikiDir, 0755)

	// Write a _log.md with known content
	logContent := `# Wiki Log

## [2026-04-05] WIKI-DEBT | Commit def5678 bypassed wiki check
- Files changed: src/old.go
- Bypassed by: developer (--no-verify)
- Status: pending wiki update

## [2026-04-06] WIKI-DEBT | Commit abc1234 bypassed wiki check
- Files changed: src/new.go, lib/util.go
- Bypassed by: developer (--no-verify)
- Status: pending wiki update
`
	logPath := filepath.Join(wikiDir, "_log.md")
	os.WriteFile(logPath, []byte(logContent), 0644)

	entries, err := GetDebtEntries(wikiDir)
	if err != nil {
		t.Fatalf("GetDebtEntries failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].CommitSHA != "def5678" {
		t.Errorf("entry 0 commit = %q, want def5678", entries[0].CommitSHA)
	}
	if entries[0].Date != "2026-04-05" {
		t.Errorf("entry 0 date = %q, want 2026-04-05", entries[0].Date)
	}
	if entries[1].CommitSHA != "abc1234" {
		t.Errorf("entry 1 commit = %q, want abc1234", entries[1].CommitSHA)
	}
	if len(entries[1].Files) != 2 {
		t.Errorf("entry 1 files count = %d, want 2", len(entries[1].Files))
	}
}

func TestGetDebtEntries_NoFile(t *testing.T) {
	entries, err := GetDebtEntries("/nonexistent/path/.wiki")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries for missing file, got %v", entries)
	}
}

func TestShortSHA(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abcdef1234567890", "abcdef1"},
		{"abc", "abc"},
		{"", ""},
	}
	for _, tt := range tests {
		got := shortSHA(tt.input)
		if got != tt.want {
			t.Errorf("shortSHA(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
