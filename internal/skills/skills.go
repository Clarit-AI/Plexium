package skills

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed content/plexium-user content/plexium-dev
var embeddedSkills embed.FS

// EnsureSkills copies embedded skill files into .claude/skills/ under the
// given repo root. Files that already exist are left untouched (idempotent).
// Returns the list of files created.
func EnsureSkills(repoRoot string) ([]string, error) {
	var created []string

	err := fs.WalkDir(embeddedSkills, "content", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Strip the "content/" prefix to get the relative path under .claude/skills/
		rel, err := filepath.Rel("content", path)
		if err != nil {
			return err
		}
		dest := filepath.Join(repoRoot, ".claude", "skills", rel)

		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}

		// Skip if the file already exists
		if _, err := os.Stat(dest); err == nil {
			return nil
		}

		data, err := embeddedSkills.ReadFile(path)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return err
		}
		created = append(created, filepath.Join(".claude", "skills", rel))
		return nil
	})

	return created, err
}
