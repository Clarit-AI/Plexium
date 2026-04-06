package memento

import (
	"fmt"
	"os/exec"
	"strings"
)

// MementoGate implements the CI gate that fails builds without session provenance.
// It checks whether the most recent commit has a memento note attached via git-notes.
type MementoGate struct {
	RepoRoot string
}

// GateResult holds the outcome of a memento gate check.
type GateResult struct {
	Passes            bool
	Reason            string
	LastCommitHasNote bool
}

// NewGate creates a MementoGate for the given repository root.
func NewGate(repoRoot string) *MementoGate {
	return &MementoGate{RepoRoot: repoRoot}
}

// Check verifies the most recent commit has a memento note attached.
// It runs "git notes --ref=memento show HEAD" and inspects the result.
func (g *MementoGate) Check() (*GateResult, error) {
	result := &GateResult{}

	// Verify we're in a git repo by getting HEAD
	headCmd := exec.Command("git", "rev-parse", "HEAD")
	headCmd.Dir = g.RepoRoot
	headOut, err := headCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("not a git repository or no commits: %w", err)
	}
	headSHA := strings.TrimSpace(string(headOut))
	if headSHA == "" {
		return nil, fmt.Errorf("could not determine HEAD commit")
	}

	// Check for memento note on HEAD
	noteCmd := exec.Command("git", "notes", "--ref=memento", "show", "HEAD")
	noteCmd.Dir = g.RepoRoot
	noteOut, err := noteCmd.Output()
	if err != nil {
		// No note found — gate fails
		result.Passes = false
		result.LastCommitHasNote = false
		result.Reason = fmt.Sprintf("commit %s has no memento session note", headSHA[:8])
		return result, nil
	}

	noteContent := strings.TrimSpace(string(noteOut))
	if noteContent == "" {
		result.Passes = false
		result.LastCommitHasNote = false
		result.Reason = fmt.Sprintf("commit %s has an empty memento note", headSHA[:8])
		return result, nil
	}

	result.Passes = true
	result.LastCommitHasNote = true
	result.Reason = fmt.Sprintf("commit %s has memento session provenance", headSHA[:8])
	return result, nil
}
