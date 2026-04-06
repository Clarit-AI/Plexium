package template

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

// Engine renders wiki pages from templates
type Engine struct {
	templateDir string
	templates   *template.Template
}

// New creates a new template engine
func New(templateDir string) (*Engine, error) {
	e := &Engine{
		templateDir: templateDir,
	}

	if templateDir != "" {
		pattern := filepath.Join(templateDir, "*.tpl")
		tmpl, err := template.ParseGlob(pattern)
		if err != nil {
			return nil, fmt.Errorf("parsing templates: %w", err)
		}
		e.templates = tmpl
	}

	return e, nil
}

// Render executes a named template with the given data
func (e *Engine) Render(name string, data interface{}) (string, error) {
	if e.templates == nil {
		return "", fmt.Errorf("no templates loaded")
	}

	var buf bytes.Buffer
	if err := e.templates.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("rendering template %s: %w", name, err)
	}

	return buf.String(), nil
}

// Register adds a named template to the engine
func (e *Engine) Register(name string, tmpl string) error {
	if e.templates == nil {
		e.templates = template.New(name)
	}

	_, err := e.templates.New(name).Parse(tmpl)
	if err != nil {
		return fmt.Errorf("parsing template %s: %w", name, err)
	}

	return nil
}

// LoadDir loads templates from a directory
func (e *Engine) LoadDir(dir string) error {
	pattern := filepath.Join(dir, "*.tpl")
	tmpl, err := template.ParseGlob(pattern)
	if err != nil {
		return fmt.Errorf("parsing templates from %s: %w", dir, err)
	}
	e.templates = tmpl
	e.templateDir = dir
	return nil
}

// DefaultEngine returns an engine with built-in default templates
func DefaultEngine() (*Engine, error) {
	return New("")
}

// ModuleData represents data for module page templates
type ModuleData struct {
	Title         string
	LastUpdated   string
	UpdatedBy     string
	RelatedModules []string
	SourceFiles   []string
	Confidence    string
	ReviewStatus  string
	Tags          []string
	Body          string
}

// DecisionData represents data for ADR templates
type DecisionData struct {
	Title       string
	Date        string
	Deciders    []string
	Context     string
	Decision    string
	Consequences string
	Status      string
}

// ConceptData represents data for concept page templates
type ConceptData struct {
	Title       string
	Description string
	Related     []string
	Examples    []string
	Tags        []string
}

// Ensure template directory exists
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0755)
}
