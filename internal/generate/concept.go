package generate

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/internal/markdown"
	"github.com/Clarit-AI/Plexium/internal/template"
)

// ConceptGenerator creates wiki pages for domain concepts
type ConceptGenerator struct {
	engine *template.Engine
}

// NewConceptGenerator creates a new ConceptGenerator
func NewConceptGenerator(engine *template.Engine) *ConceptGenerator {
	return &ConceptGenerator{
		engine: engine,
	}
}

// ConceptData represents data for a concept page
type ConceptData struct {
	Title           string
	Description     string
	RelatedConcepts []string
	RelatedModules  []string
	Examples        []string
	Tags            []string
	SourceFiles     []string
	LastUpdated     string
	UpdatedBy       string
}

// Generate creates a wiki page for a concept
func (g *ConceptGenerator) Generate(title string, sourcePaths []string, content string) (*markdown.Document, error) {
	data := &ConceptData{
		Title:           formatTitle(title),
		Description:     g.extractDescription(content),
		RelatedConcepts: g.extractRelatedConcepts(content),
		RelatedModules:  g.extractRelatedModules(content),
		Examples:        g.extractExamples(content),
		Tags:            g.extractTags(content, title),
		SourceFiles:     sourcePaths,
		LastUpdated:     time.Now().Format("2006-01-02"),
		UpdatedBy:       "plexium",
	}

	// Render template
	output, err := g.engine.Render("concept.md", data)
	if err != nil {
		output = g.defaultConceptTemplate(data)
	}

	doc, err := markdown.Parse(output)
	if err != nil {
		return nil, fmt.Errorf("parsing generated concept: %w", err)
	}

	return doc, nil
}

// extractDescription extracts a summary from content
func (g *ConceptGenerator) extractDescription(content string) string {
	lines := strings.Split(content, "\n")
	var descLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip empty lines, headers, and lists at the start
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			continue
		}

		descLines = append(descLines, line)
		if len(descLines) >= 3 {
			break
		}
	}

	description := strings.Join(descLines, " ")
	// Truncate if too long
	if len(description) > 500 {
		// Find a good break point
		if idx := strings.LastIndex(description[:500], ". "); idx != -1 {
			description = description[:idx+1]
		} else {
			description = description[:500] + "..."
		}
	}

	return strings.TrimSpace(description)
}

// extractRelatedConcepts extracts [[wiki-links]] that look like concepts
func (g *ConceptGenerator) extractRelatedConcepts(content string) []string {
	var concepts []string
	wikiLinkRegex := regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	for _, m := range wikiLinkRegex.FindAllStringSubmatch(content, -1) {
		link := m[1]
		// Links to concepts often don't have path separators
		if !strings.Contains(link, "/") {
			concepts = append(concepts, link)
		}
	}
	return concepts
}

// extractRelatedModules extracts module-style links
func (g *ConceptGenerator) extractRelatedModules(content string) []string {
	var modules []string
	wikiLinkRegex := regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	for _, m := range wikiLinkRegex.FindAllStringSubmatch(content, -1) {
		link := m[1]
		// Links to modules typically have "modules/" prefix or look like module names
		if strings.HasPrefix(link, "modules/") {
			modules = append(modules, strings.TrimPrefix(link, "modules/"))
		}
	}
	return modules
}

// extractExamples extracts example sections
func (g *ConceptGenerator) extractExamples(content string) []string {
	var examples []string
	lines := strings.Split(content, "\n")
	inExample := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## Example") || strings.HasPrefix(trimmed, "### Example") {
			inExample = true
			continue
		}
		if inExample && strings.HasPrefix(trimmed, "##") {
			break
		}
		if inExample && trimmed != "" {
			examples = append(examples, trimmed)
		}
	}

	return examples
}

// tagRegex matches hashtag-style tags: a word preceded by a '#' that is NOT
// at the start of a line (to avoid matching markdown ATX headers like "# Title").
// Valid tags appear inline: "this is about #authentication and #rbac"
var tagRegex = regexp.MustCompile(`(?:^|\s)#(\w{2,})`)

// extractTags extracts tags from content and title.
// It only matches inline hashtag patterns (e.g., "uses #auth") and avoids
// matching markdown ATX headers (e.g., "# Title").
func (g *ConceptGenerator) extractTags(content, title string) []string {
	tags := []string{}
	seen := make(map[string]bool)

	// Extract inline #tags from content — skip lines that look like headers
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		// Skip lines that ARE markdown headers (start with 1-6 # followed by space)
		if isMarkdownHeader(trimmed) {
			continue
		}
		for _, m := range tagRegex.FindAllStringSubmatch(trimmed, -1) {
			tag := strings.ToLower(m[1])
			// Filter common non-tag words
			if tag == "example" || tag == "examples" || tag == "note" || tag == "notes" {
				continue
			}
			if !seen[tag] {
				seen[tag] = true
				tags = append(tags, tag)
			}
		}
	}

	// Add title words as tags if not already present
	titleWords := strings.Fields(strings.ToLower(title))
	for _, word := range titleWords {
		if len(word) <= 2 {
			continue
		}
		if !seen[word] {
			seen[word] = true
			tags = append(tags, word)
		}
	}

	return tags
}

// isMarkdownHeader returns true if the line is a markdown ATX header (## Title)
func isMarkdownHeader(line string) bool {
	if len(line) == 0 {
		return false
	}
	// Count leading # characters (max 6)
	hashes := 0
	for _, c := range line {
		if c == '#' && hashes < 6 {
			hashes++
		} else {
			break
		}
	}
	// A header has 1-6 # followed by a space
	return hashes >= 1 && hashes <= 6 && len(line) > hashes && line[hashes] == ' '
}

// defaultConceptTemplate provides a fallback template using yaml-safe output
func (g *ConceptGenerator) defaultConceptTemplate(data *ConceptData) string {
	var b strings.Builder

	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %q\n", data.Title))
	b.WriteString("ownership: managed\n")
	b.WriteString(fmt.Sprintf("last-updated: %s\n", data.LastUpdated))
	b.WriteString(fmt.Sprintf("updated-by: %s\n", data.UpdatedBy))
	b.WriteString(fmt.Sprintf("related-modules: [%s]\n", yamlJoin(data.RelatedModules)))
	b.WriteString(fmt.Sprintf("tags: [%s]\n", yamlJoin(data.Tags)))
	b.WriteString("---\n\n")

	b.WriteString(fmt.Sprintf("# %s\n\n", data.Title))
	b.WriteString(data.Description)
	b.WriteString("\n")

	if len(data.RelatedConcepts) > 0 {
		b.WriteString("\n## Related Concepts\n\n")
		for _, c := range data.RelatedConcepts {
			b.WriteString(fmt.Sprintf("- [[%s]]\n", c))
		}
	}

	if len(data.Examples) > 0 {
		b.WriteString("\n## Examples\n\n")
		for _, e := range data.Examples {
			b.WriteString(fmt.Sprintf("%s\n", e))
		}
	}

	return b.String()
}

// yamlJoin joins strings into a YAML-safe list
func yamlJoin(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		if strings.ContainsAny(item, ":\"'{}[],&*?|>!%@`") {
			quoted[i] = fmt.Sprintf("%q", item)
		} else {
			quoted[i] = item
		}
	}
	return strings.Join(quoted, ", ")
}
