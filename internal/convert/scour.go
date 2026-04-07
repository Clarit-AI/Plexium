package convert

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Clarit-AI/Plexium/internal/scanner"
)

// Scourer traverses the repository and extracts documentation from multiple sources.
type Scourer struct {
	scanner *scanner.Scanner
	root    string
}

// ScourFindings holds everything extracted from the repository.
type ScourFindings struct {
	Readmes      []ReadmeDoc
	SourceFiles  []SourceDoc
	Configs      []ConfigDoc
	ADRs         []ADRDoc
	ExistingDocs []ExistingDoc
}

// ReadmeDoc represents a README file found in the repo.
type ReadmeDoc struct {
	Path      string
	Title     string
	Content   string
	Hierarchy int // nesting depth (0 = root)
}

// SourceDoc represents a source file with extracted documentation.
type SourceDoc struct {
	Path          string
	PackageName   string
	DocComments   []string
	FunctionNames []string
	TypeNames     []string
}

// ConfigDoc represents a parsed config file (package.json, go.mod, etc.).
type ConfigDoc struct {
	Path    string
	Type    string         // e.g. "go.mod", "package.json", "pyproject.toml"
	Content map[string]any // parsed key-value data
}

// ADRDoc represents an Architecture Decision Record.
type ADRDoc struct {
	Path    string
	Number  int
	Title   string
	Status  string
	Content string
}

// ExistingDoc represents an existing documentation file (CLAUDE.md, AGENTS.md, etc.).
type ExistingDoc struct {
	Path    string
	Type    string // "claude", "agents", "cursorrules", "readme", "doc"
	Content string
}

// ScourOptions controls how deep scouring goes.
type ScourOptions struct {
	Depth string // "shallow" or "deep"
}

// NewScourer creates a new Scourer for the given repo root.
func NewScourer(root string) (*Scourer, error) {
	// Create a broad scanner that picks up everything we care about
	s, err := scanner.New(
		[]string{"**/*.md", "**/*.go", "**/*.ts", "**/*.js", "**/*.py", "**/*.rs",
			"**/*.java", "**/*.toml", "**/*.json", "**/*.yml", "**/*.yaml",
			"go.mod", "Cargo.toml", "pyproject.toml", ".env.example"},
		[]string{"**/node_modules/**", "**/.next/**", "**/dist/**", "**/vendor/**",
			"**/.git/**", "**/.wiki/**", "**/.plexium/**", "**/target/**",
			"**/__pycache__/**", "**/.venv/**"},
	)
	if err != nil {
		return nil, fmt.Errorf("creating scourer scanner: %w", err)
	}
	return &Scourer{scanner: s, root: root}, nil
}

// Scour traverses the repository and extracts findings.
func (s *Scourer) Scour(opts ScourOptions) (*ScourFindings, error) {
	files, err := s.scanner.Scan(s.root)
	if err != nil {
		return nil, fmt.Errorf("scanning repository: %w", err)
	}

	findings := &ScourFindings{}

	for _, f := range files {
		// Skip files larger than 1MB
		if len(f.Content) > 1024*1024 {
			continue
		}
		// Skip non-UTF8
		if !utf8.ValidString(f.Content) {
			continue
		}

		switch {
		case isReadme(f.Path):
			findings.Readmes = append(findings.Readmes, s.extractReadme(f))
		case isADRPath(f.Path):
			findings.ADRs = append(findings.ADRs, s.extractADR(f))
		case isAgentInstruction(f.Path):
			findings.ExistingDocs = append(findings.ExistingDocs, s.extractExistingDoc(f))
		case isConfigFile(f.Path):
			if doc := s.extractConfig(f); doc != nil {
				findings.Configs = append(findings.Configs, *doc)
			}
		case isDocFile(f.Path):
			findings.ExistingDocs = append(findings.ExistingDocs, s.extractExistingDoc(f))
		case isSourceFile(f.Path) && opts.Depth == "deep":
			findings.SourceFiles = append(findings.SourceFiles, s.extractSource(f))
		}
	}

	return findings, nil
}

// --- README extraction ---

func isReadme(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return base == "readme.md" || base == "readme"
}

func (s *Scourer) extractReadme(f scanner.File) ReadmeDoc {
	depth := strings.Count(filepath.Dir(f.Path), string(filepath.Separator))
	if filepath.Dir(f.Path) == "." {
		depth = 0
	}

	title := extractFirstHeading(f.Content)
	if title == "" {
		title = filepath.Dir(f.Path)
		if title == "." {
			title = "Home"
		}
	}

	return ReadmeDoc{
		Path:      f.Path,
		Title:     title,
		Content:   f.Content,
		Hierarchy: depth,
	}
}

// --- ADR extraction ---

var adrDirNames = map[string]bool{
	"adr": true, "decisions": true,
}

func isADRPath(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, p := range parts[:len(parts)-1] {
		if adrDirNames[strings.ToLower(p)] {
			return true
		}
	}
	// Also check docs/decisions
	if len(parts) >= 3 && strings.ToLower(parts[0]) == "docs" && strings.ToLower(parts[1]) == "decisions" {
		return true
	}
	return false
}

var adrNumberRegex = regexp.MustCompile(`^(\d+)-(.+)\.md$`)

func (s *Scourer) extractADR(f scanner.File) ADRDoc {
	doc := ADRDoc{
		Path:    f.Path,
		Content: f.Content,
	}

	base := filepath.Base(f.Path)
	if matches := adrNumberRegex.FindStringSubmatch(base); matches != nil {
		fmt.Sscanf(matches[1], "%d", &doc.Number)
		doc.Title = formatADRTitle(matches[2])
	} else {
		doc.Title = extractFirstHeading(f.Content)
		if doc.Title == "" {
			doc.Title = strings.TrimSuffix(base, ".md")
		}
	}

	// Extract status
	statusRegex := regexp.MustCompile(`(?mi)^\*\*Status:\*\*\s*(.+)$`)
	if m := statusRegex.FindStringSubmatch(f.Content); m != nil {
		doc.Status = strings.TrimSpace(m[1])
	} else {
		doc.Status = "Proposed"
	}

	return doc
}

func formatADRTitle(slug string) string {
	s := strings.ReplaceAll(slug, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// --- Source file extraction ---

func isSourceFile(path string) bool {
	ext := filepath.Ext(path)
	switch ext {
	case ".go", ".ts", ".js", ".py", ".rs", ".java":
		return true
	}
	return false
}

var (
	goFuncRegex     = regexp.MustCompile(`^func\s+(?:\([^)]+\)\s+)?(\w+)`)
	goTypeRegex     = regexp.MustCompile(`^type\s+(\w+)`)
	goPackageRegex  = regexp.MustCompile(`^package\s+(\w+)`)
	goDocRegex      = regexp.MustCompile(`^//\s*(.+)`)
)

func (s *Scourer) extractSource(f scanner.File) SourceDoc {
	doc := SourceDoc{Path: f.Path}

	lines := strings.Split(f.Content, "\n")
	var pendingComments []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Go package
		if m := goPackageRegex.FindStringSubmatch(trimmed); m != nil {
			doc.PackageName = m[1]
			continue
		}

		// Doc comments
		if m := goDocRegex.FindStringSubmatch(trimmed); m != nil {
			pendingComments = append(pendingComments, m[1])
			continue
		}

		// Function declarations
		if m := goFuncRegex.FindStringSubmatch(trimmed); m != nil {
			doc.FunctionNames = append(doc.FunctionNames, m[1])
			if len(pendingComments) > 0 {
				doc.DocComments = append(doc.DocComments, strings.Join(pendingComments, " "))
			}
			pendingComments = nil
			continue
		}

		// Type declarations
		if m := goTypeRegex.FindStringSubmatch(trimmed); m != nil {
			doc.TypeNames = append(doc.TypeNames, m[1])
			if len(pendingComments) > 0 {
				doc.DocComments = append(doc.DocComments, strings.Join(pendingComments, " "))
			}
			pendingComments = nil
			continue
		}

		// Reset pending comments on non-comment, non-declaration lines
		if trimmed != "" {
			pendingComments = nil
		}
	}

	return doc
}

// --- Config file extraction ---

func isConfigFile(path string) bool {
	base := filepath.Base(path)
	switch base {
	case "package.json", "go.mod", "Cargo.toml", "pyproject.toml",
		"requirements.txt", "pom.xml", ".env.example":
		return true
	}
	return false
}

func (s *Scourer) extractConfig(f scanner.File) *ConfigDoc {
	base := filepath.Base(f.Path)
	doc := &ConfigDoc{
		Path:    f.Path,
		Type:    base,
		Content: make(map[string]any),
	}

	switch base {
	case "package.json":
		var pkg map[string]any
		if err := json.Unmarshal([]byte(f.Content), &pkg); err != nil {
			return nil
		}
		if name, ok := pkg["name"].(string); ok {
			doc.Content["name"] = name
		}
		if desc, ok := pkg["description"].(string); ok {
			doc.Content["description"] = desc
		}
		if deps, ok := pkg["dependencies"].(map[string]any); ok {
			keys := make([]string, 0, len(deps))
			for k := range deps {
				keys = append(keys, k)
			}
			doc.Content["dependencies"] = keys
		}
		if scripts, ok := pkg["scripts"].(map[string]any); ok {
			doc.Content["scripts"] = scripts
		}
	case "go.mod":
		// Extract module name from first line
		lines := strings.Split(f.Content, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "module ") {
				doc.Content["module"] = strings.TrimPrefix(line, "module ")
				break
			}
		}
	default:
		// Store raw content for other config types
		doc.Content["raw"] = f.Content
	}

	return doc
}

// --- Agent instruction files ---

func isAgentInstruction(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "claude.md", "agents.md", ".cursorrules":
		return true
	}
	// .gemini/config.md
	if strings.Contains(filepath.ToSlash(path), ".gemini/") {
		return true
	}
	return false
}

// --- Doc files ---

func isDocFile(path string) bool {
	ext := filepath.Ext(path)
	if ext != ".md" {
		return false
	}
	// Not a README (handled separately) and not an ADR
	if isReadme(path) || isADRPath(path) || isAgentInstruction(path) {
		return false
	}
	// Must be in docs/ or similar
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) >= 2 {
		dir := strings.ToLower(parts[0])
		return dir == "docs" || dir == "doc" || dir == "documentation"
	}
	return false
}

func (s *Scourer) extractExistingDoc(f scanner.File) ExistingDoc {
	docType := "doc"
	base := strings.ToLower(filepath.Base(f.Path))
	switch {
	case base == "claude.md":
		docType = "claude"
	case base == "agents.md":
		docType = "agents"
	case base == ".cursorrules":
		docType = "cursorrules"
	case isReadme(f.Path):
		docType = "readme"
	}

	return ExistingDoc{
		Path:    f.Path,
		Type:    docType,
		Content: f.Content,
	}
}

// --- Helpers ---

func extractFirstHeading(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(trimmed[2:])
		}
	}
	return ""
}