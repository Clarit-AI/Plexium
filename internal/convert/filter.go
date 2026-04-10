package convert

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Clarit-AI/Plexium/internal/scanner"
	"github.com/gobwas/glob"
)

// Filter classifies scanned files as eligible or skipped.
type Filter struct {
	include []glob.Glob
	exclude []glob.Glob
}

// FilterResult holds the outcome of filtering.
type FilterResult struct {
	Eligible    []scanner.File
	Skipped     []scanner.File
	SkipReasons map[string]string // path → reason
}

// DefaultInclude patterns for conversion.
var DefaultInclude = []string{
	"README.md", "**/README.md",
	"**/*.md",
	"docs/*.md", "docs/**/*.md", "doc/*.md", "doc/**/*.md",
	"adr/*.md", "adr/**/*.md", "decisions/*.md", "decisions/**/*.md",
	"docs/decisions/*.md", "docs/decisions/**/*.md",
	"src/**", "lib/**", "pkg/**", "internal/**", "cmd/**",
	"package.json", "go.mod", "Cargo.toml", "pyproject.toml",
	".env.example",
	"CLAUDE.md", "AGENTS.md",
}

// DefaultExclude patterns for conversion.
var DefaultExclude = []string{
	"node_modules/**", "**/node_modules/**",
	".next/**", "**/.next/**",
	"dist/**", "**/dist/**",
	"vendor/**", "**/vendor/**",
	".git/**", "**/.git/**",
	".wiki/**", "**/.wiki/**",
	".plexium/**", "**/.plexium/**",
	"target/**", "**/target/**",
	"__pycache__/**", "**/__pycache__/**",
	".venv/**", "**/.venv/**",
	"build/**", "**/build/**",
	"out/**", "**/out/**",
	".cache/**", "**/.cache/**",
}

// BinaryExtensions are always excluded.
var BinaryExtensions = map[string]bool{
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".o": true, ".a": true, ".lib": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true, ".svg": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true,
	".pdf": true, ".wasm": true, ".pyc": true,
}

// NewFilter creates a Filter from include/exclude patterns.
// If include is nil, DefaultInclude is used. If exclude is nil, DefaultExclude is used.
// Recursive patterns (e.g. "**/*.md") are augmented with root-level variants ("*.md")
// so root-level files are also matched.
func NewFilter(include, exclude []string) (*Filter, error) {
	if include == nil {
		include = DefaultInclude
	}
	if exclude == nil {
		exclude = DefaultExclude
	}

	f := &Filter{}

	for _, pattern := range include {
		f.include = append(f.include, mustCompileGlob(pattern)...)
	}

	for _, pattern := range exclude {
		f.exclude = append(f.exclude, mustCompileGlob(pattern)...)
	}

	return f, nil
}

// mustCompileGlob compiles a glob pattern and its root-level variant.
func mustCompileGlob(pattern string) []glob.Glob {
	var globs []glob.Glob

	g, err := glob.Compile(pattern, '/')
	if err != nil {
		return nil
	}
	globs = append(globs, g)

	// For recursive patterns like "**/*.md", also compile "*.md"
	if strings.HasPrefix(pattern, "**/") {
		rootPat := strings.TrimPrefix(pattern, "**/")
		if rootPat != pattern {
			if rg, err := glob.Compile(rootPat, '/'); err == nil {
				globs = append(globs, rg)
			}
		}
	}

	return globs
}

// Apply runs the filter against a list of scanned files.
func (f *Filter) Apply(files []scanner.File) *FilterResult {
	result := &FilterResult{
		SkipReasons: make(map[string]string),
	}

	for _, file := range files {
		if file.IsDir {
			continue
		}

		reason := f.skipReason(file)
		if reason != "" {
			result.Skipped = append(result.Skipped, file)
			result.SkipReasons[file.Path] = reason
		} else {
			result.Eligible = append(result.Eligible, file)
		}
	}

	return result
}

func (f *Filter) skipReason(file scanner.File) string {
	// Binary extension check
	ext := strings.ToLower(filepath.Ext(file.Path))
	if BinaryExtensions[ext] {
		return "binary file"
	}

	// Size check (>1MB)
	if len(file.Content) > 1024*1024 {
		return "file too large (>1MB)"
	}

	// Empty file
	if len(strings.TrimSpace(file.Content)) == 0 {
		return "empty file"
	}

	// Non-UTF8
	if !utf8.ValidString(file.Content) {
		return "non-UTF8 encoding"
	}

	// Exclude patterns take precedence
	for _, g := range f.exclude {
		if g.Match(file.Path) {
			return "excluded by pattern"
		}
	}

	// Must match at least one include pattern
	matched := false
	for _, g := range f.include {
		if g.Match(file.Path) {
			matched = true
			break
		}
	}
	if !matched {
		return "no matching include pattern"
	}

	return ""
}