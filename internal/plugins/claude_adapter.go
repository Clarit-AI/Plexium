package plugins

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/Clarit-AI/Plexium/internal/skills"
)

//go:embed builtins/claude/claude.md.tmpl
var claudeTemplate embed.FS

type claudeTemplateData struct {
	ProjectName  string
	DetectedStack string
	SchemaDigest string
}

// RunClaudeAdapter generates a lean CLAUDE.md, installs skills, and writes
// starter lefthook.yml. It replaces the shell-based plugin.sh for Claude.
func RunClaudeAdapter(repoRoot string) error {
	// 1. Detect project name
	projectName := detectProjectName(repoRoot)

	// 2. Detect tech stack
	stack := DetectTechStack(repoRoot)
	stackLabel := ""
	if stack != TechStackGeneric {
		stackLabel = string(stack)
	}

	// 3. Read schema digest
	schemaDigest := extractSchemaDigest(repoRoot)

	// 4. Render template
	tmplData, err := claudeTemplate.ReadFile("builtins/claude/claude.md.tmpl")
	if err != nil {
		return fmt.Errorf("reading claude template: %w", err)
	}
	tmpl, err := template.New("claude.md").Parse(string(tmplData))
	if err != nil {
		return fmt.Errorf("parsing claude template: %w", err)
	}

	var rendered strings.Builder
	if err := tmpl.Execute(&rendered, claudeTemplateData{
		ProjectName:   projectName,
		DetectedStack: stackLabel,
		SchemaDigest:  schemaDigest,
	}); err != nil {
		return fmt.Errorf("executing claude template: %w", err)
	}

	// 5. Content-aware write (preserve user content outside markers)
	claudePath := filepath.Join(repoRoot, "CLAUDE.md")
	content := rendered.String()
	if existing, err := os.ReadFile(claudePath); err == nil {
		content = mergeWithExisting(string(existing), content)
	}
	if err := os.WriteFile(claudePath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing CLAUDE.md: %w", err)
	}

	// 6. Install skills
	if _, err := skills.EnsureSkills(repoRoot); err != nil {
		return fmt.Errorf("installing skills: %w", err)
	}

	// 7. Write starter lefthook.yml if not present
	if err := writeStarterLefthook(repoRoot); err != nil {
		return fmt.Errorf("writing lefthook.yml: %w", err)
	}

	// 8. Ensure Claude Code settings.json hooks (PostToolUse)
	if _, err := EnsureClaudeHooks(repoRoot); err != nil {
		return fmt.Errorf("configuring Claude Code hooks: %w", err)
	}

	return nil
}

func detectProjectName(repoRoot string) string {
	// Try go.mod
	if data, err := os.ReadFile(filepath.Join(repoRoot, "go.mod")); err == nil {
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "module ") {
				mod := strings.TrimPrefix(line, "module ")
				parts := strings.Split(mod, "/")
				return parts[len(parts)-1]
			}
		}
	}

	// Try package.json
	if data, err := os.ReadFile(filepath.Join(repoRoot, "package.json")); err == nil {
		var pkg struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(data, &pkg) == nil && pkg.Name != "" {
			return pkg.Name
		}
	}

	// Fall back to directory name
	return filepath.Base(repoRoot)
}

func extractSchemaDigest(repoRoot string) string {
	schemaPath := filepath.Join(repoRoot, ".wiki", "_schema.md")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return "Run `plexium init` to generate the wiki schema."
	}

	// Extract first heading and first paragraph
	lines := strings.Split(string(data), "\n")
	var digest []string
	inContent := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" && inContent {
			break
		}
		if strings.HasPrefix(trimmed, "---") {
			continue // skip frontmatter delimiters
		}
		if strings.HasPrefix(trimmed, "#") || (inContent && trimmed != "") {
			digest = append(digest, line)
			inContent = true
		}
	}
	if len(digest) == 0 {
		return "Schema present at `.wiki/_schema.md` — see file for details."
	}
	return strings.Join(digest, "\n")
}

// mergeWithExisting preserves user content outside SCHEMA_INJECT markers.
// If the existing file has markers, only the content between them is replaced.
// If no markers are found, the generated content replaces the entire file.
func mergeWithExisting(existing, generated string) string {
	const startMarker = "<!-- SCHEMA_INJECT_START -->"
	const endMarker = "<!-- SCHEMA_INJECT_END -->"

	startIdx := strings.Index(existing, startMarker)
	endIdx := strings.Index(existing, endMarker)
	if startIdx < 0 || endIdx < 0 || endIdx <= startIdx {
		return generated
	}

	// Extract the new schema section from the generated content
	genStart := strings.Index(generated, startMarker)
	genEnd := strings.Index(generated, endMarker)
	if genStart < 0 || genEnd < 0 {
		return generated
	}

	newSection := generated[genStart : genEnd+len(endMarker)]
	return existing[:startIdx] + newSection + existing[endIdx+len(endMarker):]
}

const starterLefthookYAML = `pre-commit:
  commands:
    plexium:
      run: plexium hook pre-commit
      fail_text: |
        Code files changed but .wiki/ was not updated.
        Ask your coding agent to document the changes, or run:
          plexium sync
        To bypass (with audit trail): git commit --no-verify

post-commit:
  commands:
    plexium:
      run: plexium hook post-commit
`

func writeStarterLefthook(repoRoot string) error {
	path := filepath.Join(repoRoot, "lefthook.yml")
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	return os.WriteFile(path, []byte(starterLefthookYAML), 0o644)
}
