package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gobwas/glob"
)

// Scanner traverses the repository with include/exclude glob patterns
type Scanner struct {
	include []glob.Glob
	exclude []glob.Glob
}

// File represents a scanned file
type File struct {
	Path      string
	AbsPath   string
	Content   string
	IsDir     bool
	IsSymlink bool
	Mode      os.FileMode
	ModTime   time.Time
}

// New creates a new Scanner with the given include and exclude patterns.
// Patterns like "**/*.md" are augmented with a root-level variant ("*.md")
// so that files at the repo root are also matched.
func New(include, exclude []string) (*Scanner, error) {
	s := &Scanner{
		include: make([]glob.Glob, 0, len(include)*2),
		exclude: make([]glob.Glob, 0, len(exclude)*2),
	}

	for _, pattern := range include {
		g, err := glob.Compile(pattern, '/')
		if err != nil {
			return nil, err
		}
		s.include = append(s.include, g)

		// For recursive patterns like "**/*.md", also compile the root-level
		// variant "*.md" so files at the repo root match.
		if rootPat := rootLevelPattern(pattern); rootPat != "" && rootPat != pattern {
			rg, err := glob.Compile(rootPat, '/')
			if err != nil {
				return nil, err
			}
			s.include = append(s.include, rg)
		}
	}

	for _, pattern := range exclude {
		g, err := glob.Compile(pattern, '/')
		if err != nil {
			return nil, err
		}
		s.exclude = append(s.exclude, g)

		if rootPat := rootLevelPattern(pattern); rootPat != "" && rootPat != pattern {
			rg, err := glob.Compile(rootPat, '/')
			if err != nil {
				return nil, err
			}
			s.exclude = append(s.exclude, rg)
		}
	}

	return s, nil
}

// rootLevelPattern converts a recursive glob like "**/*.md" into its
// root-level equivalent "*.md". Returns "" if the pattern is not recursive.
func rootLevelPattern(pattern string) string {
	if !strings.HasPrefix(pattern, "**/") {
		return ""
	}
	return strings.TrimPrefix(pattern, "**/")
}

// Scan traverses the root directory and returns files matching include patterns
func (s *Scanner) Scan(root string) ([]File, error) {
	var files []File

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		// Normalize path separators for glob matching
		relPath = filepath.ToSlash(relPath)

		// Skip root directory itself
		if relPath == "." {
			return nil
		}

		// Check if this is a symlink
		isSymlink := false
		if info.Mode()&os.ModeSymlink != 0 {
			isSymlink = true
		}

		// Skip special filesystem entries like sockets, devices, and named pipes.
		// They are not readable repository content and can fail with errors such as
		// "operation not supported on socket" when scanned as ordinary files.
		if !info.IsDir() && !isSymlink && !info.Mode().IsRegular() {
			return nil
		}

		// Check exclude patterns first (exclude takes precedence)
		if s.matches(relPath, s.exclude) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// For directories, we still scan them but don't include them
		if info.IsDir() {
			return nil
		}

		// Check include patterns
		if !s.matches(relPath, s.include) {
			return nil
		}

		// Read file content
		content := ""
		if !isSymlink {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			content = string(data)
		}

		files = append(files, File{
			Path:      relPath,
			AbsPath:   path,
			Content:   content,
			IsDir:     info.IsDir(),
			IsSymlink: isSymlink,
			Mode:      info.Mode(),
			ModTime:   info.ModTime(),
		})

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort deterministically by path
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return files, nil
}

// matches checks if the path matches any of the glob patterns
func (s *Scanner) matches(path string, patterns []glob.Glob) bool {
	for _, g := range patterns {
		if g.Match(path) {
			return true
		}
	}
	return false
}

// ExpandHome expands ~ in paths to the user's home directory
func ExpandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
