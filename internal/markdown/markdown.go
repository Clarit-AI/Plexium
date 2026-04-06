package markdown

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Document represents a parsed markdown document
type Document struct {
	Frontmatter map[string]any // Parsed YAML frontmatter
	Body        string          // Content after frontmatter
	Raw         string          // Original full content
}

// frontmatterRegex matches YAML frontmatter
var frontmatterRegex = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n(.*)$`)

// Parse extracts frontmatter from raw markdown
func Parse(raw string) (*Document, error) {
	matches := frontmatterRegex.FindStringSubmatch(raw)
	if matches == nil {
		return &Document{
			Frontmatter: make(map[string]any),
			Body:        raw,
			Raw:         raw,
		}, nil
	}

	frontmatter := make(map[string]any)
	if err := yaml.Unmarshal([]byte(matches[1]), frontmatter); err != nil {
		return nil, fmt.Errorf("parsing frontmatter: %w", err)
	}

	return &Document{
		Frontmatter: frontmatter,
		Body:        matches[2],
		Raw:         raw,
	}, nil
}

// StripFrontmatter removes frontmatter from document
func StripFrontmatter(doc *Document) string {
	matches := frontmatterRegex.FindStringSubmatch(doc.Raw)
	if matches == nil {
		return doc.Raw
	}
	return matches[2]
}

// InjectFrontmatter injects frontmatter into markdown body
func InjectFrontmatter(doc *Document) (string, error) {
	frontmatter, err := yaml.Marshal(doc.Frontmatter)
	if err != nil {
		return "", fmt.Errorf("marshaling frontmatter: %w", err)
	}

	return fmt.Sprintf("---\n%s---\n%s", string(frontmatter), doc.Body), nil
}

// NormalizeHeadings adjusts heading levels (convert.md may need to shift H1→H2)
func NormalizeHeadings(doc *Document, baseLevel int) string {
	lines := strings.Split(doc.Body, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "#") {
			// Count leading hashes
			hashes := 0
			for _, c := range line {
				if c == '#' {
					hashes++
				} else if c == ' ' {
					break
				} else {
					break
				}
			}
			if hashes > 0 {
				newLevel := hashes + baseLevel
				if newLevel > 6 {
					newLevel = 6
				}
				lines[i] = strings.Repeat("#", newLevel) + line[hashes:]
			}
		}
	}
	return strings.Join(lines, "\n")
}

// ExtractWikiLinks extracts all [[wiki-links]] from body
func ExtractWikiLinks(body string) []string {
	wikiLinkRegex := regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	matches := wikiLinkRegex.FindAllStringSubmatch(body, -1)

	links := make([]string, 0, len(matches))
	seen := make(map[string]bool)
	for _, match := range matches {
		link := match[1]
		if !seen[link] {
			seen[link] = true
			links = append(links, link)
		}
	}
	return links
}

// ValidateFrontmatter checks that required frontmatter fields are present
func ValidateFrontmatter(doc *Document) error {
	required := []string{"title", "ownership"}
	for _, field := range required {
		if _, ok := doc.Frontmatter[field]; !ok {
			return fmt.Errorf("missing required frontmatter field: %s", field)
		}
	}

	// Validate ownership values
	ownership, ok := doc.Frontmatter["ownership"].(string)
	if ok {
		valid := map[string]bool{
			"managed":       true,
			"human-authored": true,
			"co-maintained":  true,
		}
		if !valid[ownership] {
			return fmt.Errorf("invalid ownership value: %s (must be managed, human-authored, or co-maintained)", ownership)
		}
	}

	return nil
}

// HasFrontmatter returns true if the document has frontmatter
func HasFrontmatter(doc *Document) bool {
	return frontmatterRegex.MatchString(doc.Raw)
}
