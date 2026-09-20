package hook

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
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

	// KHA-287 / F7: "wiki updated" used to mean "any wiki file is staged".
	// That let an unrelated wiki edit satisfy a source change. Now we
	// distinguish "wiki file is staged" (WikiUpdated) from "staged wiki
	// file is mapped to a staged source file" (WikiRelevant). Only when
	// WikiRelevant is true does the hook allow the commit when strictness
	// would otherwise block.
	wikiFiles := h.collectWikiFiles(stagedFiles)
	result.WikiFiles = wikiFiles
	result.WikiUpdated = len(wikiFiles) > 0
	result.WikiRelevant = h.isWikiRelevant(wikiFiles, sourceFiles)

	// Explicit debt mark bypasses the relevance check entirely. The
	// operator is asserting they will document the change manually.
	if h.hasExplicitDebtMark() {
		result.Allowed = true
		result.Strictness = h.strictness()
		result.Reason = "explicit debt mark present (PLEXIUM_WIKI_DEBT or PLEXIUM_BYPASS_HOOK)"
		return result, nil
	}

	if result.WikiUpdated && result.WikiRelevant {
		result.Allowed = true
		result.Strictness = h.strictness()
		result.Reason = "staged wiki change is mapped to a staged source change"
		return result, nil
	}

	// Wiki NOT updated (or unrelated) — apply strictness
	strictness := h.strictness()
	result.Strictness = strictness
	switch {
	case result.WikiUpdated && !result.WikiRelevant:
		result.Reason = fmt.Sprintf(
			"%d source file(s) changed and %d wiki file(s) are staged, but no staged wiki file is mapped (via the manifest's SourceFiles) to any staged source file",
			len(sourceFiles), len(wikiFiles),
		)
	default:
		result.Reason = fmt.Sprintf("%d source file(s) changed but .wiki/ not updated", len(sourceFiles))
	}

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
	return len(h.collectWikiFiles(files)) > 0
}

// collectWikiFiles returns the subset of the staged files that live under
// the configured wiki root.
func (h *PreCommitHook) collectWikiFiles(files []string) []string {
	wikiRoot := ".wiki"
	if h.cfg != nil && h.cfg.Wiki.Root != "" {
		wikiRoot = h.cfg.Wiki.Root
	}
	var out []string
	for _, f := range files {
		if strings.HasPrefix(f, wikiRoot+"/") || f == wikiRoot {
			out = append(out, f)
		}
	}
	return out
}

// isWikiRelevant reports whether at least one staged wiki file is mapped
// (via the manifest's SourceFiles) to a staged source file. New wiki
// files not yet in the manifest are considered relevant only if their
// path matches the wiki-path convention for a staged source file
// (e.g., src/foo.go ↔ wiki/modules/foo.md / wiki/foo.md / wiki/src/foo.md).
//
// If the manifest cannot be loaded we conservatively return false: an
// unrelated wiki edit must not silently satisfy a source change.
func (h *PreCommitHook) isWikiRelevant(wikiFiles, sourceFiles []string) bool {
	if len(wikiFiles) == 0 || len(sourceFiles) == 0 {
		return false
	}
	mgr, err := manifest.NewManager(manifest.DefaultPath(h.repoRoot))
	if err != nil {
		return false
	}
	m, err := mgr.Load()
	if err != nil {
		return false
	}

	wikiRoot := ".wiki"
	if h.cfg != nil && h.cfg.Wiki.Root != "" {
		wikiRoot = h.cfg.Wiki.Root
	}

	// Build the set of source-file paths each staged wiki page maps to.
	wikiToSources := make(map[string]map[string]bool)
	for _, page := range m.Pages {
		if page.WikiPath == "" {
			continue
		}
		// Normalize the wiki path the same way staged paths look.
		key := filepath.ToSlash(filepath.Join(wikiRoot, page.WikiPath))
		set := wikiToSources[key]
		if set == nil {
			set = make(map[string]bool)
		}
		for _, sf := range page.SourceFiles {
			set[sf.Path] = true
		}
		wikiToSources[key] = set
	}

	// Staged source files as a set.
	sourceSet := make(map[string]bool)
	for _, s := range sourceFiles {
		sourceSet[s] = true
	}

	for _, wf := range wikiFiles {
		normalized := filepath.ToSlash(wf)
		// Direct manifest mapping.
		if sources, ok := wikiToSources[normalized]; ok {
			for s := range sources {
				if sourceSet[s] {
					return true
				}
			}
		}
		// Heuristic: a brand-new wiki page (not in manifest) is relevant
		// to a staged source file when its basename matches the source
		// file's basename. This catches `src/auth.go` + `wiki/modules/
		// auth-module.md` style conventions without requiring an explicit
		// manifest entry yet. Conservative: only when a candidate match
		// exists by stem.
		base := filepath.Base(normalized)
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		for s := range sourceSet {
			sourceBase := filepath.Base(s)
			sourceStem := strings.TrimSuffix(sourceBase, filepath.Ext(sourceBase))
			if stem != "" && stem == sourceStem {
				return true
			}
		}
	}
	return false
}

// hasExplicitDebtMark reports whether the operator has explicitly marked
// this commit as a wiki-debt acknowledgement. Two env vars are honoured:
// PLEXIUM_BYPASS_HOOK=1 (legacy bypass) and PLEXIUM_WIKI_DEBT=1 (the
// debt-mark that KHA-287 introduces). The CI checker treats both the
// same way.
func (h *PreCommitHook) hasExplicitDebtMark() bool {
	return os.Getenv("PLEXIUM_BYPASS_HOOK") == "1" ||
		os.Getenv("PLEXIUM_WIKI_DEBT") == "1"
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
