package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/internal/markdown"
	"github.com/Clarit-AI/Plexium/internal/scanner"
	"github.com/Clarit-AI/Plexium/internal/template"
	"gopkg.in/yaml.v3"
)

// ModuleGenerator creates wiki pages for source modules
type ModuleGenerator struct {
	scanner *scanner.Scanner
	engine  *template.Engine
}

// ModuleData contains data for generating a module page
type ModuleData struct {
	Title          string   `yaml:"title"`
	LastUpdated    string   `yaml:"last-updated"`
	UpdatedBy      string   `yaml:"updated-by"`
	RelatedModules []string `yaml:"related-modules"`
	SourceFiles    []string `yaml:"source-files"`
	Confidence     string   `yaml:"confidence"`
	ReviewStatus   string   `yaml:"review-status"`
	Tags           []string `yaml:"tags"`
	Body           string   `yaml:"-"`
	Description    string   `yaml:"-"`
	Exports        []string `yaml:"-"`
	PackageName    string   `yaml:"-"`
}

// NewModuleGenerator creates a new ModuleGenerator
func NewModuleGenerator(scanner *scanner.Scanner, engine *template.Engine) *ModuleGenerator {
	return &ModuleGenerator{
		scanner: scanner,
		engine:  engine,
	}
}

// Generate creates a wiki page for a module
func (g *ModuleGenerator) Generate(modulePath string) (*markdown.Document, error) {
	// Read module directory
	files, err := g.scanner.Scan(modulePath)
	if err != nil {
		return nil, fmt.Errorf("scanning module %s: %w", modulePath, err)
	}

	// Extract module name from path
	moduleName := filepath.Base(modulePath)

	// Collect source files and analyze
	var sourceFiles []string
	var exports []string
	var packageName string

	for _, f := range files {
		if f.IsDir {
			continue
		}
		sourceFiles = append(sourceFiles, f.Path)

		ext := filepath.Ext(f.Path)
		if ext == ".go" || ext == ".ts" || ext == ".js" || ext == ".py" {
			exports = append(exports, g.extractExports(f)...)
		}

		if packageName == "" {
			packageName = g.extractPackageName(f)
		}
	}

	description := fmt.Sprintf("Module containing %d source files", len(sourceFiles))

	data := &ModuleData{
		Title:          formatTitle(moduleName),
		LastUpdated:    time.Now().Format("2006-01-02"),
		UpdatedBy:      "plexium",
		RelatedModules: []string{},
		SourceFiles:    sourceFiles,
		Confidence:     "medium",
		ReviewStatus:   "unreviewed",
		Tags:           []string{moduleName},
		Description:    description,
		Exports:        exports,
		PackageName:    packageName,
	}

	// Render template
	content, err := g.engine.Render("module.md", data)
	if err != nil {
		// Fall back to default template
		content = g.defaultModuleTemplate(data)
	}

	doc, err := markdown.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("parsing generated module: %w", err)
	}

	return doc, nil
}

// extractExports extracts exported functions/types from source files
func (g *ModuleGenerator) extractExports(f scanner.File) []string {
	var exports []string

	inBlockComment := false
	lines := strings.Split(f.Content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track block comment state
		if strings.HasPrefix(trimmed, "/*") {
			inBlockComment = true
		}
		if inBlockComment {
			if strings.Contains(trimmed, "*/") {
				inBlockComment = false
			}
			continue
		}

		// Skip line comments
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		// Go exports: func Foo, type Bar, const Foo, var Bar
		if strings.HasPrefix(trimmed, "func ") || strings.HasPrefix(trimmed, "type ") ||
			strings.HasPrefix(trimmed, "const ") || strings.HasPrefix(trimmed, "var ") {
			// Extract name (first word after keyword)
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				name := parts[1]
				// Remove receiver if present: (r *Receiver) FuncName
				if idx := strings.Index(name, "("); idx != -1 {
					name = name[idx+1:]
					if idx := strings.Index(name, ")"); idx != -1 {
						name = name[idx+1:]
					}
				}
				if len(name) > 0 {
					exports = append(exports, name)
				}
			}
		}

		// TypeScript/JavaScript exports: export function Foo, export class Bar, export const Foo
		if strings.HasPrefix(trimmed, "export ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 3 {
				name := parts[2]
				// Remove brace or parenthesis
				name = strings.TrimSuffix(name, "()")
				name = strings.TrimSuffix(name, "{}")
				if name != "" {
					exports = append(exports, name)
				}
			}
		}
	}

	return exports
}

// extractPackageName extracts the package name from a Go file
func (g *ModuleGenerator) extractPackageName(f scanner.File) string {
	if filepath.Ext(f.Path) != ".go" {
		return ""
	}

	packageRegex := regexp.MustCompile(`^package\s+(\w+)`)
	lines := strings.Split(f.Content, "\n")
	for _, line := range lines {
		matches := packageRegex.FindStringSubmatch(strings.TrimSpace(line))
		if matches != nil {
			return matches[1]
		}
	}
	return ""
}

// moduleFrontmatter holds only the fields that go into YAML frontmatter
type moduleFrontmatter struct {
	Title          string   `yaml:"title"`
	Ownership      string   `yaml:"ownership"`
	LastUpdated    string   `yaml:"last-updated"`
	UpdatedBy      string   `yaml:"updated-by"`
	RelatedModules []string `yaml:"related-modules"`
	SourceFiles    []string `yaml:"source-files"`
	Confidence     string   `yaml:"confidence"`
	ReviewStatus   string   `yaml:"review-status"`
	Tags           []string `yaml:"tags"`
}

// defaultModuleTemplate provides a fallback template with yaml.Marshal for safe output
func (g *ModuleGenerator) defaultModuleTemplate(data *ModuleData) string {
	fm := moduleFrontmatter{
		Title:          data.Title,
		Ownership:      "managed",
		LastUpdated:    data.LastUpdated,
		UpdatedBy:      data.UpdatedBy,
		RelatedModules: data.RelatedModules,
		SourceFiles:    data.SourceFiles,
		Confidence:     data.Confidence,
		ReviewStatus:   data.ReviewStatus,
		Tags:           data.Tags,
	}

	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		// Fallback to simple format if yaml.Marshal fails
		return fmt.Sprintf("# %s\n\n%s", data.Title, data.Description)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fmBytes)
	b.WriteString("---\n\n")

	b.WriteString(fmt.Sprintf("# %s\n\n", data.Title))
	b.WriteString(fmt.Sprintf("%s\n\n", data.Description))

	if len(data.Exports) > 0 {
		b.WriteString("## Exports\n\n")
		for _, exp := range data.Exports {
			b.WriteString(fmt.Sprintf("- `%s`\n", exp))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// GenerateFromPath generates a module page from a src/{module} path
func (g *ModuleGenerator) GenerateFromPath(modulePath string) (*markdown.Document, error) {
	// Verify this is a module path
	if !strings.HasPrefix(modulePath, "src/") {
		return nil, fmt.Errorf("not a module path: %s", modulePath)
	}

	// Check if directory exists
	if _, err := os.Stat(modulePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("module path does not exist: %s", modulePath)
	}

	return g.Generate(modulePath)
}
