package publish

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/bmatcuk/doublestar/v2"
)

// PublishOptions holds options for the publish command
type PublishOptions struct {
	RepoRoot string
	DryRun   bool
}

// PublishResult holds the result of a publish operation
type PublishResult struct {
	FilesPushed []string
	FilesSkipped []string
	Commit      string
	Timestamp   string
}

// Publisher handles pushing wiki content to GitHub Wiki
type Publisher struct {
	repoRoot    string
	wikiPath    string
	config      *config.Config
	manifestMgr *manifest.Manager
}

// NewPublisher creates a new Publisher
func NewPublisher(opts PublishOptions) (*Publisher, error) {
	configPath := filepath.Join(opts.RepoRoot, ".plexium", "config.yml")
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	mgr, err := manifest.NewManager(manifest.DefaultPath(opts.RepoRoot))
	if err != nil {
		return nil, fmt.Errorf("creating manifest manager: %w", err)
	}

	wikiPath := filepath.Join(opts.RepoRoot, cfg.Wiki.Root)

	return &Publisher{
		repoRoot:    opts.RepoRoot,
		wikiPath:    wikiPath,
		config:      cfg,
		manifestMgr: mgr,
	}, nil
}

// Publish pushes wiki files to the GitHub Wiki repository.
func Publish(opts PublishOptions) (*PublishResult, error) {
	pub, err := NewPublisher(opts)
	if err != nil {
		return nil, err
	}
	return pub.Publish(opts.DryRun)
}

// Publish executes the publish operation
func (p *Publisher) Publish(dryRun bool) (*PublishResult, error) {
	result := &PublishResult{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// Load manifest
	mf, err := p.manifestMgr.Load()
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}

	// Collect wiki files to publish
	filesToPublish, skipped, err := p.collectFiles(mf)
	if err != nil {
		return nil, fmt.Errorf("collecting files: %w", err)
	}

	if len(filesToPublish) == 0 {
		return result, nil
	}

	result.FilesPushed = filesToPublish
	result.FilesSkipped = skipped

	if dryRun {
		return result, nil
	}

	// Perform the actual publish
	if p.config.GitHubWiki.Enabled {
		if err := p.pushToGitHubWiki(filesToPublish); err != nil {
			return nil, fmt.Errorf("pushing to GitHub Wiki: %w", err)
		}
	}

	// Update manifest timestamp
	if err := p.manifestMgr.UpdatePublishTimestamp(); err != nil {
		return nil, fmt.Errorf("updating publish timestamp: %w", err)
	}

	// Record commit hash
	if hash, err := p.getGitHead(); err == nil {
		result.Commit = hash
	}

	return result, nil
}

// collectFiles gathers wiki files respecting publish/exclude config
func (p *Publisher) collectFiles(mf *manifest.Manifest) ([]string, []string, error) {
	var files []string
	var skipped []string

	err := filepath.Walk(p.wikiPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(p.wikiPath, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		// Skip hidden files and directories (e.g., .obsidian/, .DS_Store)
		parts := strings.Split(relPath, "/")
		for _, p := range parts {
			if strings.HasPrefix(p, ".") && p != "." {
				return nil
			}
		}

		// Check sensitivity neverPublish patterns
		for _, np := range p.config.Sensitivity.NeverPublish {
			if matched, mErr := doublestar.Match(np, relPath); mErr == nil && matched {
				skipped = append(skipped, relPath)
				return nil
			}
			// Also match against just the filename for bare patterns like "credentials.json"
			if matched, mErr := doublestar.Match(np, filepath.Base(path)); mErr == nil && matched {
				skipped = append(skipped, relPath)
				return nil
			}
		}

		// Check exclude patterns (applies to all file types, not just markdown)
		if p.isExcluded(relPath) {
			return nil
		}

		// Check publish patterns (if specified, only publish matching)
		if len(p.config.GitHubWiki.Publish) > 0 && !p.isInPublishList(relPath) {
			skipped = append(skipped, relPath)
			return nil
		}

		// Skip human-authored pages if configured
		if !p.config.Publish.PreserveUnmanagedPages {
			for _, page := range mf.Pages {
				if page.WikiPath == relPath && page.Ownership == "human-authored" {
					skipped = append(skipped, relPath)
					return nil
				}
			}
		}

		files = append(files, relPath)
		return nil
	})

	return files, skipped, err
}

func (p *Publisher) isExcluded(path string) bool {
	for _, pattern := range p.config.GitHubWiki.Exclude {
		if matched, err := doublestar.Match(pattern, path); err == nil && matched {
			return true
		}
	}
	return false
}

func (p *Publisher) isInPublishList(path string) bool {
	for _, pattern := range p.config.GitHubWiki.Publish {
		if matched, err := doublestar.Match(pattern, path); err == nil && matched {
			return true
		}
	}
	return false
}

// pushToGitHubWiki copies wiki files to the GitHub Wiki repo and pushes
func (p *Publisher) pushToGitHubWiki(files []string) error {
	// Get remote URL to construct wiki URL
	remoteURL, err := p.getRemoteURL()
	if err != nil {
		return fmt.Errorf("getting remote URL: %w", err)
	}

	wikiURL := toWikiURL(remoteURL)
	if wikiURL == "" {
		return fmt.Errorf("cannot determine wiki URL from remote: %s", remoteURL)
	}

	// Clone wiki repo to temp dir
	tmpDir, err := os.MkdirTemp("", "plexium-wiki-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	cloneCmd := exec.Command("git", "clone", wikiURL, tmpDir)
	if output, err := cloneCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cloning wiki repo: %s: %w", string(output), err)
	}

	// Step 1: Clear all previously synced wiki content from the clone.
	// This ensures deletions in .wiki/ are reflected in the remote wiki.
	// We preserve .git/ (which contains the wiki repo itself).
	if err := clearWikiContent(tmpDir); err != nil {
		return fmt.Errorf("clearing wiki content: %w", err)
	}

	// Step 2: Copy current publish set onto the clean slate.
	for _, file := range files {
		src := filepath.Join(p.wikiPath, file)
		dst := filepath.Join(tmpDir, file)

		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return fmt.Errorf("creating wiki dir: %w", err)
		}

		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("reading wiki file %s: %w", src, err)
		}

		if err := os.WriteFile(dst, data, 0644); err != nil {
			return fmt.Errorf("writing wiki file %s: %w", dst, err)
		}
	}

	// Stage, commit, push
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = tmpDir
	if output, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("staging wiki files: %s: %w", string(output), err)
	}

	commitMsg := p.config.Publish.Message
	if commitMsg == "" {
		commitMsg = "docs: update wiki"
	}

	commitCmd := exec.Command("git", "commit", "-m", commitMsg)
	commitCmd.Dir = tmpDir
	// Commit may fail if no changes — that's OK
	commitCmd.CombinedOutput()

	pushCmd := exec.Command("git", "push")
	pushCmd.Dir = tmpDir
	if output, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pushing wiki: %s: %w", string(output), err)
	}

	return nil
}

func (p *Publisher) getRemoteURL() (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = p.repoRoot
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (p *Publisher) getGitHead() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = p.repoRoot
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// clearWikiContent removes all wiki-managed content from the cloned wiki dir,
// preserving .git/. This ensures deleted pages are mirrored to the remote wiki.
func clearWikiContent(wikiDir string) error {
	// Collect all non-.git paths to remove, then remove them after the walk.
	var toRemove []string

	err := filepath.Walk(wikiDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(wikiDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		// Always preserve .git
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() {
			toRemove = append(toRemove, path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Remove collected files (directories are emptied by file removal;
	// empty dirs are left for git to handle on add/commit)
	for _, p := range toRemove {
		os.Remove(p)
	}
	return nil
}

// toWikiURL converts a git remote URL to its wiki equivalent
func toWikiURL(remoteURL string) string {
	// HTTPS: https://github.com/owner/repo.git → https://github.com/owner/repo.wiki.git
	if strings.HasPrefix(remoteURL, "https://") {
		return strings.Replace(remoteURL, ".git", ".wiki.git", 1)
	}
	// SSH: git@github.com:owner/repo.git → git@github.com:owner/repo.wiki.git
	if strings.HasPrefix(remoteURL, "git@") {
		return strings.Replace(remoteURL, ".git", ".wiki.git", 1)
	}
	// SSH without .git suffix
	if strings.Contains(remoteURL, "github.com:") {
		return remoteURL + ".wiki.git"
	}
	return ""
}