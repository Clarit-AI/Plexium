package pageindex

import (
	"errors"
	"os"
	"path/filepath"
)

const projectReferenceConfig = `{
  "server": "plexium-pageindex",
  "command": "plexium",
  "args": ["pageindex", "serve"],
  "transport": "stdio"
}
`

// EnsureProjectReference writes the repo-local PageIndex reference file when it
// does not already exist. Existing files are preserved to keep setup
// idempotent and non-destructive.
func EnsureProjectReference(repoRoot string) (string, bool, error) {
	path := filepath.Join(repoRoot, ".plexium", "pageindex-mcp.json")

	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return path, false, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, false, err
	}
	if err := os.WriteFile(path, []byte(projectReferenceConfig), 0o644); err != nil {
		return path, false, err
	}
	return path, true, nil
}
