package wiki

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	GitignoreStart = "# BEGIN PLEXIUM GITIGNORE"
	GitignoreEnd   = "# END PLEXIUM GITIGNORE"
)

// EnsureGitignore writes (or updates) the Plexium secret/runtime ignore block
// in the repo's .gitignore. It is idempotent: a second call on an already-
// protected repo is a no-op. Returns true if the file was written.
func EnsureGitignore(repoRoot string) (bool, error) {
	path := filepath.Join(repoRoot, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}

	next, changed := applyGitignoreBlock(string(data))
	if !changed {
		return false, nil
	}

	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// GitignoreBlock returns the canonical Plexium .gitignore block.
func GitignoreBlock() string {
	return strings.Join([]string{
		GitignoreStart,
		"# Plexium local secrets and runtime artifacts",
		".mcp.json",
		".plexium/.env",
		".plexium/credentials.json",
		".plexium/daemon-status.json",
		".plexium/daemon.err.log",
		".plexium/daemon.out.log",
		".plexium/daemon.pid",
		".plexium/output/",
		".plexium/workspaces/",
		".plexium/recovery/",
		".plexium/reports/conversion-*.json",
		".plexium/reports/conversion-*.md",
		".DS_Store",
		GitignoreEnd,
		"",
	}, "\n")
}

// applyGitignoreBlock inserts or replaces the Plexium block in existing
// .gitignore content. Returns the updated content and whether a change was
// made.
func applyGitignoreBlock(existing string) (string, bool) {
	block := GitignoreBlock()
	if existing == "" {
		return block, true
	}
	if strings.Contains(existing, block) {
		return existing, false
	}

	start := strings.Index(existing, GitignoreStart)
	if start >= 0 {
		end := strings.Index(existing[start:], GitignoreEnd)
		if end >= 0 {
			endAbs := start + end + len(GitignoreEnd)
			for endAbs < len(existing) && existing[endAbs] == '\n' {
				endAbs++
			}
			return existing[:start] + block + existing[endAbs:], true
		}
	}

	trimmed := strings.TrimRight(existing, "\n")
	if trimmed == "" {
		return block, true
	}
	return trimmed + "\n\n" + block, true
}
