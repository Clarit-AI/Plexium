package generate

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/internal/markdown"
	"github.com/Clarit-AI/Plexium/internal/template"
	"gopkg.in/yaml.v3"
)

// DecisionGenerator creates wiki pages from ADRs
type DecisionGenerator struct {
	engine *template.Engine
}

// ADRData represents parsed ADR content
type ADRData struct {
	Number           int
	Title            string
	Status           string
	Date             string
	Deciders         []string
	Context          string
	Decision         string
	Consequences     string
	RelatedDecisions []string
	RelatedModules   []string
	Tags             []string
}

// NewDecisionGenerator creates a new DecisionGenerator
func NewDecisionGenerator(engine *template.Engine) *DecisionGenerator {
	return &DecisionGenerator{engine: engine}
}

// Generate creates a wiki page from an ADR file
func (g *DecisionGenerator) Generate(adrPath string, content string) (*markdown.Document, error) {
	data, err := g.parseADR(adrPath, content)
	if err != nil {
		return nil, fmt.Errorf("parsing ADR: %w", err)
	}

	// Render template
	output, err := g.engine.Render("decision.md", data)
	if err != nil {
		output = g.defaultDecisionTemplate(data)
	}

	doc, err := markdown.Parse(output)
	if err != nil {
		return nil, fmt.Errorf("parsing generated decision: %w", err)
	}

	return doc, nil
}

// parseADR extracts structured data from an ADR document
func (g *DecisionGenerator) parseADR(path, content string) (*ADRData, error) {
	data := &ADRData{
		Status:   "Proposed",
		Tags:     []string{},
		Deciders: []string{},
	}

	// Extract title from first H1
	titleRegex := regexp.MustCompile(`^#\s+(.+)$`)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if matches := titleRegex.FindStringSubmatch(strings.TrimSpace(line)); matches != nil {
			data.Title = g.cleanTitle(matches[1])
			// Extract ADR number from title if present
			if i == 0 {
				adrNumRegex := regexp.MustCompile(`^(?:ADR[- ]?)?(\d+)[:\s]+`)
				if numMatch := adrNumRegex.FindStringSubmatch(data.Title); numMatch != nil {
					fmt.Sscanf(numMatch[1], "%d", &data.Number)
					// Remove number from title
					data.Title = adrNumRegex.ReplaceAllString(data.Title, "")
				}
			}
			break
		}
	}

	if data.Title == "" {
		return nil, fmt.Errorf("cannot extract title from ADR: %s", path)
	}

	// Extract frontmatter-like fields from markdown body
	// Status: **Status:** Accepted
	statusRegex := regexp.MustCompile(`(?mi)^\*\*Status:\*\*\s*(.+)$`)
	if matches := statusRegex.FindStringSubmatch(content); matches != nil {
		data.Status = strings.TrimSpace(matches[1])
	}

	// Date: **Date:** 2024-01-15
	dateRegex := regexp.MustCompile(`(?mi)^\*\*Date:\*\*\s*(.+)$`)
	if matches := dateRegex.FindStringSubmatch(content); matches != nil {
		data.Date = strings.TrimSpace(matches[1])
	} else {
		data.Date = time.Now().Format("2006-01-02")
	}

	// Deciders: **Deciders:** @alice, @bob
	decidersRegex := regexp.MustCompile(`(?mi)^\*\*Deciders?:\*\*\s*(.+)$`)
	if matches := decidersRegex.FindStringSubmatch(content); matches != nil {
		deciders := matches[1]
		deciders = strings.ReplaceAll(deciders, "@", "")
		parts := strings.Split(deciders, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				data.Deciders = append(data.Deciders, p)
			}
		}
	}

	// Extract sections
	sectionRegex := regexp.MustCompile(`(?m)^## (.+)$`)
	sections := sectionRegex.FindAllStringIndex(content, -1)

	extractSection := func(start, end int) string {
		if start < 0 || end < 0 || start >= len(content) || end > len(content) {
			return ""
		}
		section := content[start:end]
		// Remove the header line
		sectionLines := strings.SplitN(section, "\n", 2)
		if len(sectionLines) < 2 {
			return ""
		}
		return strings.TrimSpace(sectionLines[1])
	}

	for i, match := range sections {
		sectionName := strings.TrimSpace(content[match[0]:match[1]][3:])
		var nextStart int
		if i+1 < len(sections) {
			nextStart = sections[i+1][0]
		} else {
			nextStart = len(content)
		}
		sectionContent := extractSection(match[1], nextStart)

		switch strings.ToLower(sectionName) {
		case "context":
			data.Context = sectionContent
		case "decision":
			data.Decision = sectionContent
		case "consequences":
			data.Consequences = sectionContent
		case "related decisions", "see also":
			data.RelatedDecisions = g.extractLinks(sectionContent)
		case "related modules", "see":
			data.RelatedModules = g.extractLinks(sectionContent)
		}
	}

	// If no sections found, try to parse legacy ADR format
	if data.Context == "" && data.Decision == "" {
		data.Context, data.Decision, data.Consequences = g.parseLegacyFormat(content)
	}

	return data, nil
}

// cleanTitle removes ADR prefix from title
func (g *DecisionGenerator) cleanTitle(title string) string {
	// Remove ADR-001: prefix if present
	prefixRegex := regexp.MustCompile(`^(?:ADR[- ]?\d+[:\s]+)+`)
	return prefixRegex.ReplaceAllString(title, "")
}

// extractLinks extracts [[wiki-links]] or markdown links from text
func (g *DecisionGenerator) extractLinks(text string) []string {
	seen := make(map[string]bool)
	var links []string

	// Wiki links: [[link]]
	wikiLinkRegex := regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	for _, m := range wikiLinkRegex.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			links = append(links, m[1])
		}
	}

	// Markdown links: [text](url)
	mdLinkRegex := regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	for _, m := range mdLinkRegex.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			links = append(links, m[1])
		}
	}

	return links
}

// parseLegacyFormat handles old-style ADR content without headers
func (g *DecisionGenerator) parseLegacyFormat(content string) (context, decision, consequences string) {
	lines := strings.Split(content, "\n")
	var currentSection string
	var contextLines, decisionLines, consequencesLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			currentSection = strings.ToLower(strings.TrimSpace(trimmed[3:]))
			continue
		}

		switch currentSection {
		case "context":
			contextLines = append(contextLines, line)
		case "decision":
			decisionLines = append(decisionLines, line)
		case "consequences":
			consequencesLines = append(consequencesLines, line)
		}
	}

	return strings.TrimSpace(strings.Join(contextLines, "\n")),
		strings.TrimSpace(strings.Join(decisionLines, "\n")),
		strings.TrimSpace(strings.Join(consequencesLines, "\n"))
}

// decisionFrontmatter holds only the fields that go into YAML frontmatter
type decisionFrontmatter struct {
	Title        string   `yaml:"title"`
	Ownership    string   `yaml:"ownership"`
	Date         string   `yaml:"date"`
	Status       string   `yaml:"status"`
	Deciders     []string `yaml:"deciders"`
	ReviewStatus string   `yaml:"review-status"`
}

// defaultDecisionTemplate provides a fallback template with yaml.Marshal for safe output
func (g *DecisionGenerator) defaultDecisionTemplate(data *ADRData) string {
	fm := decisionFrontmatter{
		Title:        data.Title,
		Ownership:    "managed",
		Date:         data.Date,
		Status:       data.Status,
		Deciders:     data.Deciders,
		ReviewStatus: "unreviewed",
	}

	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		return fmt.Sprintf("# %s", data.Title)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fmBytes)
	b.WriteString("---\n\n")

	b.WriteString(fmt.Sprintf("# %s\n\n", data.Title))

	if data.Context != "" {
		b.WriteString("## Context\n\n")
		b.WriteString(data.Context)
		b.WriteString("\n\n")
	}

	if data.Decision != "" {
		b.WriteString("## Decision\n\n")
		b.WriteString(data.Decision)
		b.WriteString("\n\n")
	}

	if data.Consequences != "" {
		b.WriteString("## Consequences\n\n")
		b.WriteString(data.Consequences)
		b.WriteString("\n\n")
	}

	return b.String()
}

// GenerateFromFile generates a decision page from an ADR file path
func (g *DecisionGenerator) GenerateFromFile(adrPath string) (*markdown.Document, error) {
	data, err := os.ReadFile(adrPath)
	if err != nil {
		return nil, fmt.Errorf("reading ADR file: %w", err)
	}

	return g.Generate(adrPath, string(data))
}
