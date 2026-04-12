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
	FilesPushed  []string
	FilesSkipped []string
	Commit       string
	Timestamp    string
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

	enabled, err := p.githubWikiPublishingEnabled()
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, fmt.Errorf("GitHub Wiki publishing is disabled; enable GitHub Wiki in the repository settings or set githubWiki.enabled: true in .plexium/config.yml")
	}

	if err := p.pushToGitHubWiki(filesToPublish); err != nil {
		return nil, fmt.Errorf("pushing to GitHub Wiki: %w", err)
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

func (p *Publisher) githubWikiPublishingEnabled() (bool, error) {
	if p.config != nil && p.config.GitHubWiki.Enabled {
		return true, nil
	}

	enabled, repo, err := p.detectGitHubWikiEnabled()
	if err != nil {
		return false, fmt.Errorf("githubWiki.enabled is false and Plexium could not verify GitHub Wiki settings with gh: %w. Set githubWiki.enabled: true in .plexium/config.yml after confirming the repo wiki is enabled", err)
	}
	if enabled {
		return true, nil
	}
	return false, fmt.Errorf("GitHub Wiki is disabled for %s; enable it in GitHub repository settings before publishing", repo)
}

func (p *Publisher) detectGitHubWikiEnabled() (bool, string, error) {
	remoteURL, err := p.getRemoteURL()
	if err != nil {
		return false, "", err
	}
	repo, ok := githubRepoFromRemoteURL(remoteURL)
	if !ok {
		return false, "", fmt.Errorf("remote is not a GitHub repository: %s", remoteURL)
	}

	cmd := exec.Command("gh", "repo", "view", repo, "--json", "hasWikiEnabled", "--jq", ".hasWikiEnabled")
	cmd.Dir = p.repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, repo, fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}

	switch strings.TrimSpace(string(output)) {
	case "true":
		return true, repo, nil
	case "false":
		return false, repo, nil
	default:
		return false, repo, fmt.Errorf("unexpected gh output %q", strings.TrimSpace(string(output)))
	}
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
			if matched, mErr := doublestar.Match(np, filepath.Base(relPath)); mErr == nil && matched {
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
		return formatWikiCloneError(wikiURL, output, err)
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

func formatWikiCloneError(wikiURL string, output []byte, err error) error {
	cloneOutput := strings.TrimSpace(string(output))
	if strings.Contains(cloneOutput, "Repository not found") {
		return fmt.Errorf("cloning wiki repo %s failed: repository not found. GitHub only creates the backing .wiki.git repository after Wiki is enabled and the first wiki page exists. GitHub can also return this when the active git credentials cannot access the repository. Verify `gh auth status`, enable GitHub Wiki in repository settings, create a first page in the GitHub web UI if needed, then rerun `plexium publish`: %s: %w", wikiURL, cloneOutput, err)
	}
	return fmt.Errorf("cloning wiki repo %s failed: %s: %w", wikiURL, cloneOutput, err)
}

func (p *Publisher) getRemoteURL() (string, error) {
	remote, err := p.resolveRemoteName()
	if err != nil {
		return "", err
	}

	cmd := exec.Command("git", "remote", "get-url", remote)
	cmd.Dir = p.repoRoot
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("get URL for remote %q: %w", remote, err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (p *Publisher) resolveRemoteName() (string, error) {
	if remote, err := p.currentBranchRemote(); err == nil && remote != "" {
		return remote, nil
	}

	if p.remoteExists("origin") {
		return "origin", nil
	}

	remotes, err := p.listRemotes()
	if err != nil {
		return "", err
	}
	if len(remotes) == 1 {
		return remotes[0], nil
	}
	if len(remotes) == 0 {
		return "", fmt.Errorf("no git remotes configured; add a remote or configure an upstream before publishing")
	}
	return "", fmt.Errorf("multiple git remotes configured and no upstream remote is set; set an upstream or add an origin remote before publishing")
}

func (p *Publisher) currentBranchRemote() (string, error) {
	cmd := exec.Command("git", "symbolic-ref", "--quiet", "--short", "HEAD")
	cmd.Dir = p.repoRoot
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(string(output))
	if branch == "" || branch == "HEAD" {
		return "", fmt.Errorf("current branch is detached")
	}

	cmd = exec.Command("git", "config", "--get", "branch."+branch+".remote")
	cmd.Dir = p.repoRoot
	output, err = cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (p *Publisher) remoteExists(remote string) bool {
	cmd := exec.Command("git", "remote", "get-url", remote)
	cmd.Dir = p.repoRoot
	return cmd.Run() == nil
}

func (p *Publisher) listRemotes() ([]string, error) {
	cmd := exec.Command("git", "remote")
	cmd.Dir = p.repoRoot
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list git remotes: %w", err)
	}
	var remotes []string
	for _, line := range strings.Split(string(output), "\n") {
		remote := strings.TrimSpace(line)
		if remote != "" {
			remotes = append(remotes, remote)
		}
	}
	return remotes, nil
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

func githubRepoFromRemoteURL(remoteURL string) (string, bool) {
	remoteURL = strings.TrimSpace(remoteURL)
	remoteURL = strings.TrimSuffix(remoteURL, ".git")
	remoteURL = strings.TrimSuffix(remoteURL, "/")

	for _, prefix := range []string{"https://github.com/", "http://github.com/"} {
		if strings.HasPrefix(remoteURL, prefix) {
			repo := strings.TrimPrefix(remoteURL, prefix)
			return cleanGitHubRepoName(repo)
		}
	}

	if strings.HasPrefix(remoteURL, "git@github.com:") {
		repo := strings.TrimPrefix(remoteURL, "git@github.com:")
		return cleanGitHubRepoName(repo)
	}

	if strings.HasPrefix(remoteURL, "ssh://git@github.com/") {
		repo := strings.TrimPrefix(remoteURL, "ssh://git@github.com/")
		return cleanGitHubRepoName(repo)
	}

	return "", false
}

func cleanGitHubRepoName(repo string) (string, bool) {
	parts := strings.Split(strings.Trim(repo, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return parts[0] + "/" + parts[1], true
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
	repo, ok := githubRepoFromRemoteURL(remoteURL)
	if !ok {
		return ""
	}
	remoteURL = strings.TrimSpace(remoteURL)
	switch {
	case strings.HasPrefix(remoteURL, "http://github.com/"):
		return "http://github.com/" + repo + ".wiki.git"
	case strings.HasPrefix(remoteURL, "https://github.com/"):
		return "https://github.com/" + repo + ".wiki.git"
	case strings.HasPrefix(remoteURL, "git@github.com:"):
		return "git@github.com:" + repo + ".wiki.git"
	case strings.HasPrefix(remoteURL, "ssh://git@github.com/"):
		return "ssh://git@github.com/" + repo + ".wiki.git"
	}
	return ""
}
