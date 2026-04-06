package generate

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Clarit-AI/Plexium/internal/scanner"
)

// Taxonomy classifies source files into wiki sections
type Taxonomy struct {
	Sections []string
}

// Classification represents a source file's classification result
type Classification struct {
	Section    string
	PageType   string
	Slug       string
	Title      string
	SourcePath string
}

// NewTaxonomy creates a new Taxonomy with default sections
func NewTaxonomy() *Taxonomy {
	return &Taxonomy{
		Sections: []string{"Architecture", "Modules", "Decisions", "Patterns", "Concepts", "Guides"},
	}
}

// adrRegex matches ADR filenames like 001-foo.md, 2024-01-15-foo.md
var adrRegex = regexp.MustCompile(`^(?:(\d+)-|[\d-]+-)(.+)\.md$`)

// decisionPaths are known ADR directories
var decisionPaths = map[string]bool{
	"adr":         true,
	"docs/decisions": true,
	"decisions":   true,
}

// Classify determines the wiki output for a source file
func (t *Taxonomy) Classify(file scanner.File) (*Classification, error) {
	relPath := file.Path

	// README.md → Home.md
	if relPath == "README.md" || relPath == "readme.md" {
		return &Classification{
			Section:  "Root",
			PageType: "home",
			Slug:     "Home",
			Title:    "Home",
			SourcePath: relPath,
		}, nil
	}

	// src/{module}/ → modules/{module}.md
	if strings.HasPrefix(relPath, "src/") {
		parts := strings.SplitN(relPath, "/", 3)
		if len(parts) >= 2 {
			moduleName := parts[1]
			return &Classification{
				Section:    "Modules",
				PageType:   "module",
				Slug:       moduleName,
				Title:      formatTitle(moduleName),
				SourcePath: relPath,
			}, nil
		}
	}

	// Check for ADR files by path or pattern
	if t.isADRFile(relPath) {
		return t.classifyADR(relPath)
	}

	// docs/{folder}/ or docs/*.md → varies by content/location
	if strings.HasPrefix(relPath, "docs/") {
		return t.classifyDocs(relPath)
	}

	// Default: treat as a concept or guide
	return t.classifyGeneric(relPath)
}

// isADRFile checks if the path indicates an ADR file
func (t *Taxonomy) isADRFile(path string) bool {
	// Check directory
	dir := filepath.Dir(path)
	if decisionPaths[dir] {
		return true
	}

	// Check filename pattern
	filename := filepath.Base(path)
	if adrRegex.MatchString(filename) {
		return true
	}

	return false
}

// classifyADR handles ADR files
func (t *Taxonomy) classifyADR(path string) (*Classification, error) {
	filename := filepath.Base(path)
	matches := adrRegex.FindStringSubmatch(filename)
	if matches == nil {
		return nil, fmt.Errorf("invalid ADR filename format: %s", path)
	}

	var titleStr string
	if matches[1] != "" {
		titleStr = matches[2]
	} else {
		titleStr = matches[2]
		// Extract number from date pattern if present
		dateRegex := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-(.+)$`)
		dateMatch := dateRegex.FindStringSubmatch(titleStr)
		if dateMatch != nil {
			titleStr = dateMatch[2]
		}
	}

	slug := filename[:len(filename)-3] // remove .md
	return &Classification{
		Section:    "Decisions",
		PageType:   "decision",
		Slug:       slug,
		Title:      formatTitle(titleStr),
		SourcePath: path,
	}, nil
}

// classifyDocs handles documentation files
func (t *Taxonomy) classifyDocs(path string) (*Classification, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")

	// docs/concepts/*.md → concepts/
	if len(parts) >= 2 && parts[1] == "concepts" {
		if len(parts) >= 3 {
			conceptName := strings.TrimSuffix(parts[len(parts)-1], ".md")
			return &Classification{
				Section:    "Concepts",
				PageType:   "concept",
				Slug:       conceptName,
				Title:      formatTitle(conceptName),
				SourcePath: path,
			}, nil
		}
	}

	// docs/patterns/*.md → patterns/
	if len(parts) >= 2 && parts[1] == "patterns" {
		if len(parts) >= 3 {
			patternName := strings.TrimSuffix(parts[len(parts)-1], ".md")
			return &Classification{
				Section:    "Patterns",
				PageType:   "pattern",
				Slug:       patternName,
				Title:      formatTitle(patternName),
				SourcePath: path,
			}, nil
		}
	}

	// docs/architecture/*.md → architecture/
	if len(parts) >= 2 && parts[1] == "architecture" {
		filename := strings.TrimSuffix(parts[len(parts)-1], ".md")
		return &Classification{
			Section:    "Architecture",
			PageType:   "architecture",
			Slug:       filename,
			Title:      formatTitle(filename),
			SourcePath: path,
		}, nil
	}

	// docs/{folder}/index.md → {folder} section index
	if len(parts) >= 3 && parts[len(parts)-1] == "index.md" {
		sectionName := parts[1]
		return &Classification{
			Section:    formatTitle(sectionName),
			PageType:   "index",
			Slug:       sectionName,
			Title:      formatTitle(sectionName),
			SourcePath: path,
		}, nil
	}

	// docs/*.md at root level → Guides
	if len(parts) == 2 {
		filename := strings.TrimSuffix(parts[1], ".md")
		return &Classification{
			Section:    "Guides",
			PageType:   "guide",
			Slug:       filename,
			Title:      formatTitle(filename),
			SourcePath: path,
		}, nil
	}

	// docs/{folder}/file.md → {folder} section
	if len(parts) >= 3 {
		sectionName := parts[1]
		filename := strings.TrimSuffix(parts[len(parts)-1], ".md")
		return &Classification{
			Section:    formatTitle(sectionName),
			PageType:   "guide",
			Slug:       filepath.Join(sectionName, filename),
			Title:      formatTitle(filename),
			SourcePath: path,
		}, nil
	}

	return nil, fmt.Errorf("cannot classify docs path: %s", path)
}

// classifyGeneric handles generic source files
func (t *Taxonomy) classifyGeneric(path string) (*Classification, error) {
	filename := filepath.Base(path)
	name := strings.TrimSuffix(filename, ".md")

	// Check if it looks like a concept file
	if strings.Contains(path, "concept") || strings.Contains(path, "domain") {
		return &Classification{
			Section:    "Concepts",
			PageType:   "concept",
			Slug:       name,
			Title:      formatTitle(name),
			SourcePath: path,
		}, nil
	}

	return &Classification{
		Section:    "Guides",
		PageType:   "guide",
		Slug:       name,
		Title:      formatTitle(name),
		SourcePath: path,
	}, nil
}

// formatTitle converts a slug or filename to a human-readable title
func formatTitle(name string) string {
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.TrimSpace(name)

	// Capitalize words
	words := strings.Fields(name)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(string(word[0])) + word[1:]
		}
	}

	return strings.Join(words, " ")
}

// ClassifyBatch classifies multiple files
func (t *Taxonomy) ClassifyBatch(files []scanner.File) ([]*Classification, error) {
	results := make([]*Classification, 0, len(files))
	for _, f := range files {
		c, err := t.Classify(f)
		if err != nil {
			continue // Skip unclassifiable files
		}
		results = append(results, c)
	}
	return results, nil
}
