package hook

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PostCommitHook tracks WIKI-DEBT when --no-verify was used.
type PostCommitHook struct {
	repoRoot string
	wikiRoot string
}

// NewPostCommitHook creates a new post-commit hook.
func NewPostCommitHook(repoRoot, wikiRoot string) *PostCommitHook {
	if wikiRoot == "" {
		wikiRoot = ".wiki"
	}
	return &PostCommitHook{repoRoot: repoRoot, wikiRoot: wikiRoot}
}

// Run executes the post-commit check.
// It detects if --no-verify was used and logs WIKI-DEBT if so.
func (h *PostCommitHook) Run() error {
	// Check if this commit bypassed hooks (via LEFTHOOK env or skip detection)
	bypassed := h.detectBypass()
	if !bypassed {
		return nil
	}

	// Get the latest commit SHA
	sha, err := h.getLatestCommit()
	if err != nil {
		return fmt.Errorf("getting commit SHA: %w", err)
	}

	// Get files changed in this commit
	files, err := h.getCommitFiles(sha)
	if err != nil {
		return fmt.Errorf("getting commit files: %w", err)
	}

	// Only log if source files were changed
	if len(files) == 0 {
		return nil
	}

	// Append WIKI-DEBT entry to _log.md
	entry := WikiDebtEntry{
		Date:       time.Now().UTC().Format("2006-01-02"),
		CommitSHA:  shortSHA(sha),
		Files:      files,
		BypassedBy: "developer (--no-verify)",
		Status:     "pending wiki update",
	}

	return h.appendDebtEntry(entry)
}

// detectBypass checks if the commit was made with --no-verify.
// Lefthook sets LEFTHOOK=0 when hooks are skipped.
func (h *PostCommitHook) detectBypass() bool {
	// If LEFTHOOK env is set to "0", hooks were bypassed
	if os.Getenv("LEFTHOOK") == "0" {
		return true
	}
	// If GIT_SKIP_HOOKS is set, hooks were skipped
	if os.Getenv("GIT_SKIP_HOOKS") != "" {
		return true
	}
	// Check if the hook was explicitly skipped via LEFTHOOK_QUIET
	// (this is set by lefthook when --no-verify is used)
	if os.Getenv("LEFTHOOK_QUIET") == "1" {
		return true
	}
	return false
}

// getLatestCommit returns the SHA of the most recent commit.
func (h *PostCommitHook) getLatestCommit() (string, error) {
	cmd := exec.Command("git", "-C", h.repoRoot, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// getCommitFiles returns the list of files changed in a commit.
func (h *PostCommitHook) getCommitFiles(sha string) ([]string, error) {
	cmd := exec.Command("git", "-C", h.repoRoot, "diff-tree", "--no-commit-id", "--name-only", "-r", sha)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			result = append(result, line)
		}
	}
	return result, nil
}

// appendDebtEntry appends a WIKI-DEBT entry to .wiki/_log.md.
func (h *PostCommitHook) appendDebtEntry(entry WikiDebtEntry) error {
	logPath := filepath.Join(h.repoRoot, h.wikiRoot, "_log.md")

	// Ensure the wiki directory exists
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating wiki dir: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n## [%s] WIKI-DEBT | Commit %s bypassed wiki check\n", entry.Date, entry.CommitSHA))
	sb.WriteString(fmt.Sprintf("- Files changed: %s\n", strings.Join(entry.Files, ", ")))
	sb.WriteString(fmt.Sprintf("- Bypassed by: %s\n", entry.BypassedBy))
	sb.WriteString(fmt.Sprintf("- Status: %s\n", entry.Status))

	// Read existing content
	existing, err := os.ReadFile(logPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading log: %w", err)
	}

	var content string
	if os.IsNotExist(err) {
		content = "# Wiki Log\n" + sb.String()
	} else {
		content = string(existing) + sb.String()
	}

	return os.WriteFile(logPath, []byte(content), 0644)
}

// shortSHA returns the first 7 chars of a SHA.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// GetDebtEntries reads and returns WIKI-DEBT entries from _log.md.
func GetDebtEntries(wikiRoot string) ([]WikiDebtEntry, error) {
	logPath := filepath.Join(wikiRoot, "_log.md")
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var entries []WikiDebtEntry
	lines := strings.Split(string(data), "\n")
	var current *WikiDebtEntry

	for _, line := range lines {
		if strings.Contains(line, "WIKI-DEBT") {
			if current != nil {
				entries = append(entries, *current)
			}
			current = &WikiDebtEntry{Status: "pending wiki update"}
			// Extract date from "## [YYYY-MM-DD] WIKI-DEBT | ..."
			start := strings.Index(line, "[")
			end := strings.Index(line, "]")
			if start != -1 && end != -1 {
				current.Date = line[start+1 : end]
			}
			// Extract commit SHA from "Commit abc123 bypassed..."
			commitIdx := strings.Index(line, "Commit ")
			if commitIdx != -1 {
				rest := line[commitIdx+7:]
				spaceIdx := strings.Index(rest, " ")
				if spaceIdx != -1 {
					current.CommitSHA = rest[:spaceIdx]
				} else {
					current.CommitSHA = rest
				}
			}
		} else if current != nil && strings.HasPrefix(strings.TrimSpace(line), "- Files changed:") {
			rest := strings.TrimPrefix(strings.TrimSpace(line), "- Files changed: ")
			current.Files = strings.Split(rest, ", ")
		} else if current != nil && strings.HasPrefix(strings.TrimSpace(line), "- Bypassed by:") {
			current.BypassedBy = strings.TrimPrefix(strings.TrimSpace(line), "- Bypassed by: ")
		} else if current != nil && strings.HasPrefix(strings.TrimSpace(line), "- Status:") {
			current.Status = strings.TrimPrefix(strings.TrimSpace(line), "- Status: ")
		}
	}
	if current != nil {
		entries = append(entries, *current)
	}

	return entries, nil
}
