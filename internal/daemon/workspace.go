package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// WorkspaceMgr manages git worktree-based workspaces for isolated wiki
// maintenance tasks. Each workspace is a git worktree checked out under
// .plexium/workspaces/.
type WorkspaceMgr struct {
	basePath string // .plexium/workspaces/
	repoRoot string
	gitExec  func(args ...string) ([]byte, error) // injectable for testing
}

// Worktree represents a single worktree workspace and its metadata.
type Worktree struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	IssueID   string    `json:"issueID"`
	Branch    string    `json:"branch"`
	Status    string    `json:"status"` // running | completed | failed
	StartedAt time.Time `json:"startedAt"`
}

// NewWorkspaceMgr creates a WorkspaceMgr rooted at repoRoot. Workspaces are
// stored under <repoRoot>/.plexium/workspaces/.
func NewWorkspaceMgr(repoRoot string) *WorkspaceMgr {
	return &WorkspaceMgr{
		basePath: filepath.Join(repoRoot, ".plexium", "workspaces"),
		repoRoot: repoRoot,
		gitExec:  defaultGitExec,
	}
}

// defaultGitExec shells out to git via os/exec.
func defaultGitExec(args ...string) ([]byte, error) {
	return exec.Command("git", args...).CombinedOutput()
}

// worktreeID returns the canonical ID for an issue.
func worktreeID(issueID string) string {
	return "wt-" + issueID
}

// worktreeBranch returns the branch name for an issue.
func worktreeBranch(issueID string) string {
	return "plexium/wt-" + issueID
}

// metaPath returns the path to the meta.json file for a given worktree ID.
func (m *WorkspaceMgr) metaPath(id string) string {
	return filepath.Join(m.basePath, id, "meta.json")
}

// Create creates a new git worktree workspace for the given issue. It creates
// a new branch and checks it out in a dedicated directory under basePath.
func (m *WorkspaceMgr) Create(issueID string) (*Worktree, error) {
	id := worktreeID(issueID)
	wtPath := filepath.Join(m.basePath, id)
	branch := worktreeBranch(issueID)

	// Ensure base directory exists.
	if err := os.MkdirAll(m.basePath, 0o755); err != nil {
		return nil, fmt.Errorf("workspace: mkdir %s: %w", m.basePath, err)
	}

	// Create the git worktree with a new branch.
	out, err := m.gitExec("worktree", "add", "-b", branch, wtPath)
	if err != nil {
		return nil, fmt.Errorf("workspace: git worktree add: %w: %s", err, string(out))
	}

	wt := &Worktree{
		ID:        id,
		Path:      wtPath,
		IssueID:   issueID,
		Branch:    branch,
		Status:    "running",
		StartedAt: time.Now(),
	}

	if err := m.saveMeta(wt); err != nil {
		return nil, err
	}

	return wt, nil
}

// Get returns the worktree metadata for the given ID. Returns an error if
// the worktree does not exist.
func (m *WorkspaceMgr) Get(id string) (*Worktree, error) {
	return m.loadMeta(id)
}

// List returns all worktrees that have a meta.json file under basePath.
func (m *WorkspaceMgr) List() ([]*Worktree, error) {
	entries, err := os.ReadDir(m.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("workspace: list: %w", err)
	}

	var worktrees []*Worktree
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		wt, err := m.loadMeta(e.Name())
		if err != nil {
			continue // skip entries without valid meta.json
		}
		worktrees = append(worktrees, wt)
	}
	return worktrees, nil
}

// UpdateStatus sets the status field on an existing worktree.
func (m *WorkspaceMgr) UpdateStatus(id, status string) error {
	wt, err := m.loadMeta(id)
	if err != nil {
		return err
	}
	wt.Status = status
	return m.saveMeta(wt)
}

// Cleanup removes the worktree directory and prunes the git worktree entry.
func (m *WorkspaceMgr) Cleanup(id string) error {
	wtPath := filepath.Join(m.basePath, id)

	// Remove the git worktree reference.
	out, err := m.gitExec("worktree", "remove", "--force", wtPath)
	if err != nil {
		// If the worktree is already gone from git's perspective, just remove
		// the directory and prune.
		_ = os.RemoveAll(wtPath)
		_, _ = m.gitExec("worktree", "prune")
		return fmt.Errorf("workspace: git worktree remove: %w: %s", err, string(out))
	}

	// Clean up any leftover directory (git worktree remove should handle it,
	// but be safe).
	_ = os.RemoveAll(wtPath)
	return nil
}

// CleanupAll removes all worktrees managed by this workspace manager.
func (m *WorkspaceMgr) CleanupAll() error {
	worktrees, err := m.List()
	if err != nil {
		return err
	}
	var firstErr error
	for _, wt := range worktrees {
		if err := m.Cleanup(wt.ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ActiveCount returns the number of worktrees with status "running".
func (m *WorkspaceMgr) ActiveCount() (int, error) {
	worktrees, err := m.List()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, wt := range worktrees {
		if wt.Status == "running" {
			count++
		}
	}
	return count, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (m *WorkspaceMgr) saveMeta(wt *Worktree) error {
	data, err := json.MarshalIndent(wt, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: marshal meta: %w", err)
	}

	metaDir := filepath.Join(m.basePath, wt.ID)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return fmt.Errorf("workspace: mkdir %s: %w", metaDir, err)
	}

	metaFile := filepath.Join(metaDir, "meta.json")
	if err := os.WriteFile(metaFile, data, 0o644); err != nil {
		return fmt.Errorf("workspace: write meta: %w", err)
	}
	return nil
}

func (m *WorkspaceMgr) loadMeta(id string) (*Worktree, error) {
	metaFile := m.metaPath(id)
	data, err := os.ReadFile(metaFile)
	if err != nil {
		return nil, fmt.Errorf("workspace: read meta for %s: %w", id, err)
	}

	var wt Worktree
	if err := json.Unmarshal(data, &wt); err != nil {
		return nil, fmt.Errorf("workspace: unmarshal meta for %s: %w", id, err)
	}
	return &wt, nil
}
