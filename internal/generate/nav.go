package generate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/internal/markdown"
	"github.com/Clarit-AI/Plexium/internal/template"
)

// Navigation generators

// HomeGenerator creates Home.md
type HomeGenerator struct {
	engine *template.Engine
}

// NewHomeGenerator creates a new HomeGenerator
func NewHomeGenerator(engine *template.Engine) *HomeGenerator {
	return &HomeGenerator{engine: engine}
}

// SectionInfo contains info about a wiki section
type SectionInfo struct {
	Name      string
	Slug      string
	Summary   string
	PageCount int
}

// HomeData is the data for Home.md generation
type HomeData struct {
	RepoName    string
	Description string
	LastUpdated string
	Sections    []SectionInfo
	ReadmeBody  string
}

// Generate creates the Home.md page
func (g *HomeGenerator) Generate(repoName, description string, sections []SectionInfo, readmeBody string) (*markdown.Document, error) {
	data := HomeData{
		RepoName:    repoName,
		Description: description,
		LastUpdated: time.Now().Format("2006-01-02"),
		Sections:    sections,
		ReadmeBody:  readmeBody,
	}

	content := g.defaultHomeTemplate(data)
	doc, err := markdown.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("parsing generated home: %w", err)
	}
	return doc, nil
}

func (g *HomeGenerator) defaultHomeTemplate(data HomeData) string {
	var b strings.Builder

	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %q\n", data.RepoName))
	b.WriteString("ownership: managed\n")
	b.WriteString(fmt.Sprintf("last-updated: %s\n", data.LastUpdated))
	b.WriteString("nav_order: 0\n")
	b.WriteString("---\n\n")

	b.WriteString(fmt.Sprintf("# %s\n\n", data.RepoName))

	if data.Description != "" {
		b.WriteString(data.Description)
		b.WriteString("\n\n")
	}

	if len(data.Sections) > 0 {
		b.WriteString("## Wiki Sections\n\n")
		for _, s := range data.Sections {
			b.WriteString(fmt.Sprintf("- [[%s|%s]]", s.Slug, s.Name))
			if s.Summary != "" {
				b.WriteString(fmt.Sprintf(" — %s", s.Summary))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if data.ReadmeBody != "" {
		b.WriteString("---\n\n")
		b.WriteString(data.ReadmeBody)
	}

	return b.String()
}

// SidebarGenerator creates _Sidebar.md
type SidebarGenerator struct {
	engine *template.Engine
}

// NewSidebarGenerator creates a new SidebarGenerator
func NewSidebarGenerator(engine *template.Engine) *SidebarGenerator {
	return &SidebarGenerator{engine: engine}
}

// PageInfo contains information about a wiki page
type PageInfo struct {
	Path     string
	Title    string
	Section  string
	Summary  string
	NavOrder int
	Tags     []string
	IsIndex  bool
}

// SidebarData is the data for sidebar generation
type SidebarData struct {
	Sections []SidebarSection
}

// SidebarSection represents a section in the sidebar
type SidebarSection struct {
	Name  string
	Pages []PageInfo
}

// Generate creates the _Sidebar.md page
func (g *SidebarGenerator) Generate(pages []PageInfo) (*markdown.Document, error) {
	// Group pages by section
	sections := make(map[string][]PageInfo)
	indices := make(map[string]bool)

	for _, p := range pages {
		if p.IsIndex {
			indices[p.Section] = true
			continue
		}
		sections[p.Section] = append(sections[p.Section], p)
	}

	// Sort pages within each section
	sortedSections := make([]SidebarSection, 0)
	sectionOrder := []string{"Architecture", "Modules", "Decisions", "Patterns", "Concepts", "Guides"}

	for _, sectionName := range sectionOrder {
		if pageList, ok := sections[sectionName]; ok {
			sort.Slice(pageList, func(i, j int) bool {
				if pageList[i].NavOrder != pageList[j].NavOrder {
					return pageList[i].NavOrder < pageList[j].NavOrder
				}
				return pageList[i].Title < pageList[j].Title
			})
			sortedSections = append(sortedSections, SidebarSection{
				Name:  sectionName,
				Pages: pageList,
			})
		}
	}

	// Add remaining sections alphabetically
	for sectionName, pageList := range sections {
		found := false
		for _, s := range sectionOrder {
			if s == sectionName {
				found = true
				break
			}
		}
		if !found {
			sort.Slice(pageList, func(i, j int) bool {
				return pageList[i].Title < pageList[j].Title
			})
			sortedSections = append(sortedSections, SidebarSection{
				Name:  sectionName,
				Pages: pageList,
			})
		}
	}

	data := SidebarData{Sections: sortedSections}
	content := g.defaultSidebarTemplate(data)

	doc, err := markdown.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("parsing generated sidebar: %w", err)
	}
	return doc, nil
}

func (g *SidebarGenerator) defaultSidebarTemplate(data SidebarData) string {
	var b strings.Builder

	b.WriteString("---\n")
	b.WriteString("title: Sidebar\n")
	b.WriteString("---\n\n")

	b.WriteString("# Navigation\n\n")

	for _, section := range data.Sections {
		b.WriteString(fmt.Sprintf("## %s\n\n", section.Name))
		for _, page := range section.Pages {
			link := page.Path
			if !strings.HasSuffix(link, ".md") {
				link += ".md"
			}
			b.WriteString(fmt.Sprintf("- [[%s|%s]]\n", link, page.Title))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// FooterGenerator creates _Footer.md
type FooterGenerator struct{}

// NewFooterGenerator creates a new FooterGenerator
func NewFooterGenerator() *FooterGenerator {
	return &FooterGenerator{}
}

// Generate creates the _Footer.md page
func (g *FooterGenerator) Generate(version string) (*markdown.Document, error) {
	content := g.defaultFooterTemplate(version)
	doc, err := markdown.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("parsing generated footer: %w", err)
	}
	return doc, nil
}

func (g *FooterGenerator) defaultFooterTemplate(version string) string {
	if version == "" {
		version = "0.1.0"
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("---\n\n")
	b.WriteString(fmt.Sprintf("*Last updated: %s*\n\n", time.Now().Format("2006-01-02")))
	b.WriteString(fmt.Sprintf("Powered by [Plexium](https://github.com/Clarit-AI/Plexium) v%s\n", version))
	b.WriteString("\n[[Home.md|Back to Home]]\n")

	return b.String()
}

// IndexGenerator creates _index.md
type IndexGenerator struct{}

// NewIndexGenerator creates a new IndexGenerator
func NewIndexGenerator() *IndexGenerator {
	return &IndexGenerator{}
}

// IndexEntry represents an entry in _index.md
type IndexEntry struct {
	Path        string   `json:"path"`
	Title       string   `json:"title"`
	Section     string   `json:"section"`
	Summary     string   `json:"summary,omitempty"`
	Ownership   string   `json:"ownership"`
	LastUpdated string   `json:"lastUpdated"`
	Tags        []string `json:"tags"`
}

// Generate creates the _index.md page
func (g *IndexGenerator) Generate(entries []IndexEntry) (*markdown.Document, error) {
	// Sort entries: by section, then by title
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Section != entries[j].Section {
			return entries[i].Section < entries[j].Section
		}
		return entries[i].Title < entries[j].Title
	})

	content := g.defaultIndexTemplate(entries)
	doc, err := markdown.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("parsing generated index: %w", err)
	}
	return doc, nil
}

func (g *IndexGenerator) defaultIndexTemplate(entries []IndexEntry) string {
	var b strings.Builder

	b.WriteString("---\n")
	b.WriteString("title: Index\n")
	b.WriteString("ownership: managed\n")
	b.WriteString("---\n\n")

	b.WriteString("# Wiki Index\n\n")
	b.WriteString(fmt.Sprintf("Total pages: %d\n\n", len(entries)))

	currentSection := ""
	for _, entry := range entries {
		if entry.Section != currentSection {
			if currentSection != "" {
				b.WriteString("\n")
			}
			b.WriteString(fmt.Sprintf("## %s\n\n", entry.Section))
			currentSection = entry.Section
		}

		b.WriteString(fmt.Sprintf("- [[%s|%s]]", entry.Path, entry.Title))
		if entry.Summary != "" {
			b.WriteString(fmt.Sprintf(": %s", entry.Summary))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// GenerateJSON generates a machine-readable JSON index using encoding/json
func (g *IndexGenerator) GenerateJSON(entries []IndexEntry) (string, error) {
	// Sort entries
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Section != entries[j].Section {
			return entries[i].Section < entries[j].Section
		}
		return entries[i].Title < entries[j].Title
	})

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling index JSON: %w", err)
	}
	return string(data), nil
}
