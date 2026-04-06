package publish

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/reports"
)

// WikiSyncer handles selective syncing to GitHub Wiki.
type WikiSyncer struct {
	repoRoot    string
	cfg         *config.Config
	manifestMgr *manifest.Manager
	formatter   *reports.ReportFormatter
}

// SyncResult holds the result of a wiki sync operation.
type SyncResult struct {
	PagesIncluded []string
	PagesExcluded []Exclusion
	Commit       string
	Pushed       bool
}

// Exclusion represents a page that was excluded from sync.
type Exclusion struct {
	Path   string
	Reason string // "excluded_by_pattern", "unmanaged_page", "not_matching_publish"
}

// NewWikiSyncer creates a new WikiSyncer.
func NewWikiSyncer(repoRoot string, cfg *config.Config) (*WikiSyncer, error) {
	manifestPath := filepath.Join(repoRoot, ".plexium", "manifest.json")
	mgr, err := manifest.NewManager(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("creating manifest manager: %w", err)
	}

	return &WikiSyncer{
		repoRoot:    repoRoot,
		cfg:         cfg,
		manifestMgr: mgr,
		formatter:   reports.NewReportFormatter(".plexium/reports"),
	}, nil
}

// Sync performs the wiki sync operation.
func (s *WikiSyncer) Sync(dryRun bool, push bool) (*SyncResult, error) {
	wikiRoot := s.wikiRoot()

	// Get all wiki pages
	pages, err := s.getWikiPages(wikiRoot)
	if err != nil {
		return nil, fmt.Errorf("getting wiki pages: %w", err)
	}

	result := &SyncResult{}

	// Filter pages
	for _, page := range pages {
		exclusion := s.shouldExclude(page)
		if exclusion != nil {
			result.PagesExcluded = append(result.PagesExcluded, *exclusion)
			continue
		}
		result.PagesIncluded = append(result.PagesIncluded, page)
	}

	// In dry-run mode, just return the result without writing
	if dryRun {
		s.printDryRunSummary(result)
		return result, nil
	}

	// Perform the sync
	if err := s.performSync(wikiRoot, result); err != nil {
		return nil, fmt.Errorf("performing sync: %w", err)
	}

	// Push if requested
	if push {
		if err := s.push(result); err != nil {
			return nil, fmt.Errorf("pushing to wiki: %w", err)
		}
		result.Pushed = true
	}

	return result, nil
}

func (s *WikiSyncer) getWikiPages(wikiRoot string) ([]string, error) {
	var pages []string

	entries, err := os.ReadDir(wikiRoot)
	if err != nil {
		return nil, fmt.Errorf("reading wiki root: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Skip .obsidian and other hidden directories
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			// Recursively get pages from subdirectories
			subPages, err := s.getWikiPages(filepath.Join(wikiRoot, entry.Name()))
			if err != nil {
				return nil, err
			}
			pages = append(pages, subPages...)
		} else {
			// Only include markdown files
			if strings.HasSuffix(entry.Name(), ".md") {
				relPath, err := filepath.Rel(wikiRoot, filepath.Join(wikiRoot, entry.Name()))
				if err != nil {
					continue
				}
				pages = append(pages, relPath)
			}
		}
	}

	return pages, nil
}

func (s *WikiSyncer) shouldExclude(page string) *Exclusion {
	// Check if page is in unmanaged pages
	manifestData, err := s.manifestMgr.Load()
	if err == nil {
		for _, unmanaged := range manifestData.UnmanagedPages {
			if unmanaged.WikiPath == page {
				return &Exclusion{Path: page, Reason: "unmanaged_page"}
			}
		}
	}

	// Get publish and exclude patterns from config
	publishPatterns := s.getPublishPatterns()
	excludePatterns := s.getExcludePatterns()

	// Check exclude patterns first
	for _, pattern := range excludePatterns {
		if match, err := filepath.Match(pattern, page); err == nil && match {
			return &Exclusion{Path: page, Reason: "excluded_by_pattern"}
		}
		// Also check if page is under a directory that matches
		if strings.HasPrefix(page, strings.TrimSuffix(pattern, "/**")) {
			return &Exclusion{Path: page, Reason: "excluded_by_pattern"}
		}
	}

	// If publish patterns are specified, check if page matches
	if len(publishPatterns) > 0 {
		matched := false
		for _, pattern := range publishPatterns {
			if match, err := filepath.Match(pattern, page); err == nil && match {
				matched = true
				break
			}
			// Check directory patterns like "modules/**"
			if strings.HasSuffix(pattern, "/**") {
				dir := strings.TrimSuffix(pattern, "/**")
				if strings.HasPrefix(page, dir+"/") || page == dir {
					matched = true
					break
				}
			}
		}
		if !matched {
			return &Exclusion{Path: page, Reason: "not_matching_publish"}
		}
	}

	return nil
}

func (s *WikiSyncer) getPublishPatterns() []string {
	if s.cfg != nil && len(s.cfg.GitHubWiki.Publish) > 0 {
		return s.cfg.GitHubWiki.Publish
	}
	// Default publish patterns
	return []string{
		"architecture/**",
		"modules/**",
		"decisions/**",
		"patterns/**",
		"concepts/**",
		"onboarding.md",
		"Home.md",
		"_Sidebar.md",
		"_Footer.md",
	}
}

func (s *WikiSyncer) getExcludePatterns() []string {
	if s.cfg != nil && len(s.cfg.GitHubWiki.Exclude) > 0 {
		return s.cfg.GitHubWiki.Exclude
	}
	// Default exclude patterns
	return []string{
		"raw/**",
		"reports/**",
		".obsidian/**",
	}
}

func (s *WikiSyncer) performSync(wikiRoot string, result *SyncResult) error {
	// Create a temporary directory for the wiki sync
	tempDir, err := os.MkdirTemp("", "plexium-wiki-sync-*")
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Copy included pages to temp directory
	for _, page := range result.PagesIncluded {
		src := filepath.Join(wikiRoot, page)
		dst := filepath.Join(tempDir, page)

		// Ensure destination directory exists
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", page, err)
		}

		// Copy the file
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("reading %s: %w", page, err)
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return fmt.Errorf("writing %s: %w", page, err)
		}
	}

	return nil
}

func (s *WikiSyncer) push(result *SyncResult) error {
	// This is a simplified implementation.
	// In a full implementation, this would use git to push to the wiki submodule.
	commit, err := s.runGitPush()
	if err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	result.Commit = commit
	return nil
}

func (s *WikiSyncer) runGitPush() (string, error) {
	// Run git add .
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = s.repoRoot
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}

	// Run git commit
	cmd = exec.Command("git", "commit", "-m", "docs: sync wiki")
	cmd.Dir = s.repoRoot
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git commit: %w", err)
	}

	// Get commit SHA
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = s.repoRoot
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func (s *WikiSyncer) printDryRunSummary(result *SyncResult) {
	fmt.Println("[dry-run] Wiki Sync Summary")
	fmt.Println("==========================")
	fmt.Printf("Pages included: %d\n", len(result.PagesIncluded))
	fmt.Printf("Pages excluded: %d\n", len(result.PagesExcluded))

	if len(result.PagesIncluded) > 0 {
		fmt.Println("\nIncluded pages:")
		for _, page := range result.PagesIncluded {
			fmt.Printf("  + %s\n", page)
		}
	}

	if len(result.PagesExcluded) > 0 {
		fmt.Println("\nExcluded pages:")
		for _, ex := range result.PagesExcluded {
			fmt.Printf("  - %s (%s)\n", ex.Path, ex.Reason)
		}
	}
}

func (s *WikiSyncer) wikiRoot() string {
	if s.cfg != nil && s.cfg.Wiki.Root != "" {
		return filepath.Join(s.repoRoot, s.cfg.Wiki.Root)
	}
	return filepath.Join(s.repoRoot, ".wiki")
}

// SyncOptions holds options for the gh-wiki-sync command.
type SyncOptions struct {
	RepoRoot string
	DryRun   bool
	Push     bool
}

// GHWikiSync performs a GitHub Wiki sync.
func GHWikiSync(opts SyncOptions) (*SyncResult, error) {
	cfg, err := config.LoadFromDir(opts.RepoRoot)
	if err != nil {
		// Config is optional; use defaults
		cfg = nil
	}

	syncer, err := NewWikiSyncer(opts.RepoRoot, cfg)
	if err != nil {
		return nil, err
	}

	return syncer.Sync(opts.DryRun, opts.Push)
}
