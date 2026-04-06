package hook

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Clarit-AI/Plexium/internal/config"
)

// PreCommitHook checks if wiki was updated when source files changed.
type PreCommitHook struct {
	repoRoot string
	cfg      *config.Config
}

// NewPreCommitHook creates a new pre-commit hook checker.
func NewPreCommitHook(repoRoot string, cfg *config.Config) *PreCommitHook {
	return &PreCommitHook{repoRoot: repoRoot, cfg: cfg}
}

// Run executes the pre-commit check.
// If stagedFiles is nil, reads from git. If empty slice, treats as no staged files.
func (h *PreCommitHook) Run(stagedFiles []string) (*HookResult, error) {
	result := &HookResult{}

	// Check for explicit bypass
	if os.Getenv("PLEXIUM_BYPASS_HOOK") == "1" {
		result.Allowed = true
		result.Skipped = true
		result.SkipReason = "PLEXIUM_BYPASS_HOOK=1"
		return result, nil
	}

	// Get staged files if not provided (nil means auto-detect)
	if stagedFiles == nil {
		var err error
		stagedFiles, err = h.getStagedFiles()
		if err != nil {
			return nil, fmt.Errorf("getting staged files: %w", err)
		}
	}

	// No staged files — nothing to check
	if len(stagedFiles) == 0 {
		result.Allowed = true
		result.Skipped = true
		result.SkipReason = "no staged files"
		return result, nil
	}

	// Filter to source files
	sourceFiles := h.filterSourceFiles(stagedFiles)
	if len(sourceFiles) == 0 {
		result.Allowed = true
		result.Skipped = true
		result.SkipReason = "no source files in staged set"
		return result, nil
	}

	result.FilesChanged = sourceFiles

	// Check if any .wiki/ files are staged
	result.WikiUpdated = h.hasWikiChanges(stagedFiles)

	if result.WikiUpdated {
		result.Allowed = true
		result.Strictness = h.strictness()
		return result, nil
	}

	// Wiki NOT updated — apply strictness
	strictness := h.strictness()
	result.Strictness = strictness
	result.Reason = fmt.Sprintf("%d source file(s) changed but .wiki/ not updated", len(sourceFiles))

	switch strictness {
	case "strict":
		result.Allowed = false
	case "moderate":
		// In moderate mode, still block but explain bypass options
		result.Allowed = false
	case "advisory":
		result.Allowed = true
	default:
		result.Allowed = false
	}

	return result, nil
}

// getStagedFiles runs git diff --cached --name-only.
func (h *PreCommitHook) getStagedFiles() ([]string, error) {
	cmd := exec.Command("git", "-C", h.repoRoot, "diff", "--cached", "--name-only")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --cached: %w", err)
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

// filterSourceFiles filters files matching sources.include but not sources.exclude.
func (h *PreCommitHook) filterSourceFiles(files []string) []string {
	if h.cfg == nil || len(h.cfg.Sources.Include) == 0 {
		// Default: consider common source dirs
		defaultIncludes := []string{"src/**", "cmd/**", "internal/**", "pkg/**", "lib/**", "app/**"}
		return filterByGlobs(files, defaultIncludes, nil, h.repoRoot)
	}
	return filterByGlobs(files, h.cfg.Sources.Include, h.cfg.Sources.Exclude, h.repoRoot)
}

// hasWikiChanges checks if any .wiki/ files are in the staged set.
func (h *PreCommitHook) hasWikiChanges(files []string) bool {
	wikiRoot := ".wiki"
	if h.cfg != nil && h.cfg.Wiki.Root != "" {
		wikiRoot = h.cfg.Wiki.Root
	}
	for _, f := range files {
		if strings.HasPrefix(f, wikiRoot+"/") || f == wikiRoot {
			return true
		}
	}
	return false
}

// strictness returns the enforcement strictness level.
func (h *PreCommitHook) strictness() string {
	if h.cfg != nil && h.cfg.Enforcement.Strictness != "" {
		return h.cfg.Enforcement.Strictness
	}
	return "moderate" // default
}

// filterByGlobs matches files against include/exclude glob patterns.
func filterByGlobs(files, includes, excludes []string, repoRoot string) []string {
	var result []string
	for _, f := range files {
		matched := false
		for _, inc := range includes {
			if matchGlob(f, inc) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		excluded := false
		for _, exc := range excludes {
			if matchGlob(f, exc) {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, f)
		}
	}
	return result
}

// matchGlob does simple glob matching.
func matchGlob(path, pattern string) bool {
	// Extension match: "*.go" matches any .go file (only when pattern has no /)
	if strings.HasPrefix(pattern, "*.") && !strings.Contains(pattern, "/") {
		ext := pattern[1:] // ".go"
		return strings.HasSuffix(path, ext)
	}
	// Directory prefix match: "src/**" matches "src/foo/bar.go"
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return strings.HasPrefix(path, prefix+"/")
	}
	// Exact match
	if matched, _ := filepath.Match(pattern, path); matched {
		return true
	}
	// ** prefix matching: "**/*.go" matches any .go at any depth
	if strings.HasPrefix(pattern, "**/") {
		suffix := pattern[3:]
		if matched, _ := filepath.Match(suffix, filepath.Base(path)); matched {
			return true
		}
	}
	return false
}
