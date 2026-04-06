package ci

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
)

// WikiDebtEntry represents a wiki debt item found during CI check.
type WikiDebtEntry struct {
	Commit  string   `json:"commit"`
	Message string   `json:"message"`
	Files   []string `json:"files"`
}

// CheckResult is the output of a CI check.
type CheckResult struct {
	Commit           string          `json:"commit"`
	BaseSHA          string          `json:"baseSha"`
	HeadSHA          string          `json:"headSha"`
	ChangedFiles     []string        `json:"changedFiles"`
	SourceFiles      []string        `json:"sourceFiles"`
	WikiUpdated      bool            `json:"wikiUpdated"`
	WikiDebt         []WikiDebtEntry `json:"wikiDebt"`
	UntrackedChanges []string        `json:"untrackedChanges"`
	Passes           bool            `json:"passes"`
	DebtCount        int             `json:"debtCount"`
}

// CICheck performs diff-aware wiki checks for CI pipelines.
type CICheck struct {
	repoRoot string
	cfg      *config.Config
}

// NewCICheck creates a new CI checker.
func NewCICheck(repoRoot string, cfg *config.Config) *CICheck {
	return &CICheck{repoRoot: repoRoot, cfg: cfg}
}

// Run executes the diff-aware wiki check between base and head commits.
func (c *CICheck) Run(baseSHA, headSHA string) (*CheckResult, error) {
	result := &CheckResult{
		BaseSHA: baseSHA,
		HeadSHA: headSHA,
	}

	// 1. Get files changed between base and head
	changedFiles, err := c.getDiffFiles(baseSHA, headSHA)
	if err != nil {
		return nil, fmt.Errorf("getting diff files: %w", err)
	}
	result.ChangedFiles = changedFiles

	// 2. Filter to source files
	result.SourceFiles = c.filterSourceFiles(changedFiles)

	if len(result.SourceFiles) == 0 {
		result.Passes = true
		result.WikiUpdated = true
		return result, nil
	}

	// 3. Check if wiki was updated in the same range
	result.WikiUpdated = c.hasWikiChanges(changedFiles)

	// 4. For each source file, check if it has a wiki mapping
	manifestPath := manifest.DefaultPath(c.repoRoot)
	mgr, err := manifest.NewManager(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("creating manifest manager: %w", err)
	}

	m, err := mgr.Load()
	if err != nil {
		// No manifest — flag all source files as untracked
		result.UntrackedChanges = result.SourceFiles
		result.Passes = false
		return result, nil
	}

	// Check each source file for wiki mapping
	var untracked []string
	for _, src := range result.SourceFiles {
		mapped := false
		for _, page := range m.Pages {
			for _, sf := range page.SourceFiles {
				if sf.Path == src {
					mapped = true
					break
				}
			}
			if mapped {
				break
			}
		}
		if !mapped {
			untracked = append(untracked, src)
		}
	}
	result.UntrackedChanges = untracked

	// 5. Check wiki debt from _log.md
	wikiDebt, err := c.getWikiDebt()
	if err == nil {
		result.WikiDebt = wikiDebt
	}
	result.DebtCount = len(result.WikiDebt)

	// 6. Determine pass/fail
	if result.WikiUpdated {
		result.Passes = true
	} else {
		// Check debt threshold from config
		threshold := 0 // default: no debt allowed
		if c.cfg != nil && c.cfg.Enforcement.DebtThreshold > 0 {
			threshold = c.cfg.Enforcement.DebtThreshold
		}
		result.Passes = result.DebtCount <= threshold
	}

	return result, nil
}

// ToJSON formats the result as JSON.
func (r *CheckResult) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// getDiffFiles returns files changed between two commits.
func (c *CICheck) getDiffFiles(baseSHA, headSHA string) ([]string, error) {
	cmd := exec.Command("git", "-C", c.repoRoot, "diff", "--name-only", baseSHA, headSHA)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s %s: %w", baseSHA, headSHA, err)
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

// filterSourceFiles returns files matching source include globs.
func (c *CICheck) filterSourceFiles(files []string) []string {
	var result []string
	for _, f := range files {
		if c.isSourceFile(f) {
			result = append(result, f)
		}
	}
	return result
}

// isSourceFile checks if a file path matches source patterns.
func (c *CICheck) isSourceFile(path string) bool {
	// Skip wiki files, config files, and hidden dirs
	if strings.HasPrefix(path, ".wiki/") || strings.HasPrefix(path, ".plexium/") {
		return false
	}
	if strings.HasPrefix(path, ".github/") {
		return false
	}

	if c.cfg != nil && len(c.cfg.Sources.Include) > 0 {
		for _, inc := range c.cfg.Sources.Include {
			if matchPattern(path, inc) {
				excluded := false
				for _, exc := range c.cfg.Sources.Exclude {
					if matchPattern(path, exc) {
						excluded = true
						break
					}
				}
				return !excluded
			}
		}
		return false
	}

	// Default: common source extensions
	sourceExts := map[string]bool{
		".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
		".py": true, ".rs": true, ".java": true, ".rb": true, ".swift": true,
		".kt": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
	}
	for ext := range sourceExts {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}

	// Also match common source dirs
	sourceDirs := []string{"src/", "cmd/", "internal/", "pkg/", "lib/", "app/"}
	for _, dir := range sourceDirs {
		if strings.HasPrefix(path, dir) {
			return true
		}
	}

	return false
}

// hasWikiChanges checks if any wiki files were changed.
func (c *CICheck) hasWikiChanges(files []string) bool {
	wikiRoot := ".wiki"
	if c.cfg != nil && c.cfg.Wiki.Root != "" {
		wikiRoot = c.cfg.Wiki.Root
	}
	for _, f := range files {
		if strings.HasPrefix(f, wikiRoot+"/") {
			return true
		}
	}
	return false
}

// getWikiDebt reads WIKI-DEBT entries from _log.md using the hook package's parser.
func (c *CICheck) getWikiDebt() ([]WikiDebtEntry, error) {
	// Read _log.md directly
	logPath := ".wiki/_log.md"
	if c.cfg != nil && c.cfg.Wiki.Root != "" {
		logPath = c.cfg.Wiki.Root + "/_log.md"
	}

	cmd := exec.Command("git", "-C", c.repoRoot, "show", "HEAD:"+logPath)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil // no log file is fine
	}

	var entries []WikiDebtEntry
	lines := strings.Split(string(out), "\n")
	var current *WikiDebtEntry

	for _, line := range lines {
		if strings.Contains(line, "WIKI-DEBT") {
			if current != nil {
				entries = append(entries, *current)
			}
			current = &WikiDebtEntry{}
			commitIdx := strings.Index(line, "Commit ")
			if commitIdx != -1 {
				rest := line[commitIdx+7:]
				spaceIdx := strings.Index(rest, " ")
				if spaceIdx != -1 {
					current.Commit = rest[:spaceIdx]
				}
			}
		} else if current != nil && strings.HasPrefix(strings.TrimSpace(line), "- Files changed:") {
			rest := strings.TrimPrefix(strings.TrimSpace(line), "- Files changed: ")
			current.Files = strings.Split(rest, ", ")
		}
	}
	if current != nil {
		entries = append(entries, *current)
	}

	return entries, nil
}

// matchPattern does simple glob matching.
func matchPattern(path, pattern string) bool {
	// Directory prefix match: "src/**" matches "src/foo/bar.go"
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return strings.HasPrefix(path, prefix+"/")
	}
	// Extension match: "*.go" matches any .go file
	if strings.HasPrefix(pattern, "*.") {
		ext := pattern[1:] // ".go"
		return strings.HasSuffix(path, ext)
	}
	// Exact match
	return path == pattern
}
