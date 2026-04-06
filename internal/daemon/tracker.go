// Package daemon provides the autonomous wiki maintenance loop and its supporting
// adapters including issue tracker integration.
package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNotImplemented is returned by tracker stubs that are not yet functional.
var ErrNotImplemented = errors.New("tracker: not implemented")

// TrackerAdapter defines the interface for interacting with an issue tracker.
// Implementations exist for GitHub Issues, Linear (stub), and a no-op fallback.
type TrackerAdapter interface {
	// CreateIssue creates a new issue and returns its ID.
	CreateIssue(title, body string) (string, error)
	// CloseIssue closes an existing issue by ID.
	CloseIssue(id string) error
	// AddLabel adds a label to an existing issue.
	AddLabel(issueID, label string) error
	// Comment adds a comment to an existing issue.
	Comment(issueID, body string) error
}

// ---------------------------------------------------------------------------
// NoOpTracker
// ---------------------------------------------------------------------------

// NoOpTracker is a TrackerAdapter that silently succeeds without performing
// any operations. Used when tracker integration is disabled.
type NoOpTracker struct{}

func (n *NoOpTracker) CreateIssue(_, _ string) (string, error) { return "", nil }
func (n *NoOpTracker) CloseIssue(_ string) error               { return nil }
func (n *NoOpTracker) AddLabel(_, _ string) error               { return nil }
func (n *NoOpTracker) Comment(_, _ string) error                { return nil }

// ---------------------------------------------------------------------------
// GitHubIssuesTracker
// ---------------------------------------------------------------------------

// ghCommandRunner abstracts os/exec so tests can intercept command construction.
type ghCommandRunner interface {
	Run(name string, args ...string) ([]byte, error)
}

// defaultRunner shells out via os/exec.
type defaultRunner struct{}

func (d *defaultRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// GitHubIssuesTracker implements TrackerAdapter using the GitHub CLI (gh).
type GitHubIssuesTracker struct {
	owner string
	repo  string
	token string
	cmd   ghCommandRunner
}

// NewGitHubTracker creates a GitHubIssuesTracker that shells out to the gh CLI.
func NewGitHubTracker(owner, repo, token string) *GitHubIssuesTracker {
	return &GitHubIssuesTracker{
		owner: owner,
		repo:  repo,
		token: token,
		cmd:   &defaultRunner{},
	}
}

// repoFlag returns the --repo flag value.
func (g *GitHubIssuesTracker) repoFlag() string {
	return g.owner + "/" + g.repo
}

// CreateIssue creates a GitHub issue and returns the issue number as a string.
func (g *GitHubIssuesTracker) CreateIssue(title, body string) (string, error) {
	out, err := g.cmd.Run("gh", "issue", "create",
		"--repo", g.repoFlag(),
		"--title", title,
		"--body", body,
		"--json", "number",
	)
	if err != nil {
		return "", fmt.Errorf("gh issue create: %w: %s", err, string(out))
	}

	var result struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		// gh may return the URL instead of JSON in some versions; try to
		// extract a trailing number from the output as a fallback.
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			return trimmed, nil
		}
		return "", fmt.Errorf("gh issue create: failed to parse output: %w", err)
	}
	return fmt.Sprintf("%d", result.Number), nil
}

// CloseIssue closes the given GitHub issue by ID.
func (g *GitHubIssuesTracker) CloseIssue(id string) error {
	out, err := g.cmd.Run("gh", "issue", "close", id,
		"--repo", g.repoFlag(),
	)
	if err != nil {
		return fmt.Errorf("gh issue close: %w: %s", err, string(out))
	}
	return nil
}

// AddLabel adds a label to a GitHub issue.
func (g *GitHubIssuesTracker) AddLabel(issueID, label string) error {
	out, err := g.cmd.Run("gh", "issue", "edit", issueID,
		"--add-label", label,
		"--repo", g.repoFlag(),
	)
	if err != nil {
		return fmt.Errorf("gh issue edit (add-label): %w: %s", err, string(out))
	}
	return nil
}

// Comment adds a comment to a GitHub issue.
func (g *GitHubIssuesTracker) Comment(issueID, body string) error {
	out, err := g.cmd.Run("gh", "issue", "comment", issueID,
		"--body", body,
		"--repo", g.repoFlag(),
	)
	if err != nil {
		return fmt.Errorf("gh issue comment: %w: %s", err, string(out))
	}
	return nil
}

// ---------------------------------------------------------------------------
// LinearTracker (stub)
// ---------------------------------------------------------------------------

// LinearTracker is a placeholder for future Linear API integration.
// All methods return ErrNotImplemented.
type LinearTracker struct{}

func (l *LinearTracker) CreateIssue(_, _ string) (string, error) { return "", ErrNotImplemented }
func (l *LinearTracker) CloseIssue(_ string) error               { return ErrNotImplemented }
func (l *LinearTracker) AddLabel(_, _ string) error               { return ErrNotImplemented }
func (l *LinearTracker) Comment(_, _ string) error                { return ErrNotImplemented }

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// NewTracker returns a TrackerAdapter based on the given type string.
// Supported types: "github", "linear", "none" (or empty string).
func NewTracker(trackerType, owner, repo, token string) TrackerAdapter {
	switch strings.ToLower(strings.TrimSpace(trackerType)) {
	case "github":
		return NewGitHubTracker(owner, repo, token)
	case "linear":
		return &LinearTracker{}
	default:
		return &NoOpTracker{}
	}
}
