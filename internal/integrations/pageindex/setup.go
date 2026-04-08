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

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, false, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return path, false, nil
		}
		return path, false, err
	}

	if _, err := file.Write([]byte(projectReferenceConfig)); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return path, false, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return path, false, err
	}

	return path, true, nil
}
