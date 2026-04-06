package reports

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// BootstrapReport is generated after plexium init or plexium convert.
type BootstrapReport struct {
	Type        string          `json:"type"`
	Timestamp   string          `json:"timestamp"`
	Sources     SourcesSummary  `json:"sources"`
	Pages       PagesSummary    `json:"pages"`
	Navigation  NavSummary      `json:"navigation"`
	Publish     PublishSummary  `json:"publish"`
}

// SourcesSummary summarizes source file scanning results.
type SourcesSummary struct {
	Scanned     int            `json:"scanned"`
	Included    int            `json:"included"`
	Skipped     int            `json:"skipped"`
	SkipReasons map[string]int `json:"skipReasons"`
}

// PagesSummary summarizes generated wiki pages.
type PagesSummary struct {
	Generated          int `json:"generated"`
	Stubs              int `json:"stubs"`
	CollisionsResolved int `json:"collisionsResolved"`
}

// NavSummary summarizes navigation file generation.
type NavSummary struct {
	HomeGenerated    bool `json:"homeGenerated"`
	SidebarGenerated bool `json:"sidebarGenerated"`
	AllPagesReachable bool `json:"allPagesReachable"`
}

// PublishSummary summarizes publishing results.
type PublishSummary struct {
	Status        string `json:"status"`
	PagesPublished int   `json:"pagesPublished"`
	Commit        string `json:"commit,omitempty"`
}

// NewBootstrapReport creates a new BootstrapReport.
func NewBootstrapReport() *BootstrapReport {
	return &BootstrapReport{
		Type:      "bootstrap",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Sources: SourcesSummary{
			SkipReasons: make(map[string]int),
		},
	}
}

// ToJSON formats the bootstrap report as JSON.
func (r *BootstrapReport) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// ToMarkdown formats the bootstrap report as human-readable Markdown.
func (r *BootstrapReport) ToMarkdown() string {
	var sb strings.Builder

	sb.WriteString("# Bootstrap Report\n\n")
	sb.WriteString(fmt.Sprintf("**Generated:** %s\n\n", r.Timestamp))

	sb.WriteString("## Sources\n\n")
	sb.WriteString(fmt.Sprintf("- Scanned: %d\n", r.Sources.Scanned))
	sb.WriteString(fmt.Sprintf("- Included: %d\n", r.Sources.Included))
	sb.WriteString(fmt.Sprintf("- Skipped: %d\n", r.Sources.Skipped))

	if len(r.Sources.SkipReasons) > 0 {
		sb.WriteString("\n**Skip Reasons:**\n")
		for reason, count := range r.Sources.SkipReasons {
			sb.WriteString(fmt.Sprintf("- %s: %d\n", reason, count))
		}
	}

	sb.WriteString("\n## Pages\n\n")
	sb.WriteString(fmt.Sprintf("- Generated: %d\n", r.Pages.Generated))
	sb.WriteString(fmt.Sprintf("- Stubs: %d\n", r.Pages.Stubs))
	sb.WriteString(fmt.Sprintf("- Collisions Resolved: %d\n", r.Pages.CollisionsResolved))

	sb.WriteString("\n## Navigation\n\n")
	sb.WriteString(fmt.Sprintf("- Home Generated: %v\n", boolToEmoji(r.Navigation.HomeGenerated)))
	sb.WriteString(fmt.Sprintf("- Sidebar Generated: %v\n", boolToEmoji(r.Navigation.SidebarGenerated)))
	sb.WriteString(fmt.Sprintf("- All Pages Reachable: %v\n", boolToEmoji(r.Navigation.AllPagesReachable)))

	sb.WriteString("\n## Publish\n\n")
	sb.WriteString(fmt.Sprintf("- Status: %s\n", r.Publish.Status))
	sb.WriteString(fmt.Sprintf("- Pages Published: %d\n", r.Publish.PagesPublished))
	if r.Publish.Commit != "" {
		sb.WriteString(fmt.Sprintf("- Commit: `%s`\n", r.Publish.Commit))
	}

	return sb.String()
}

// BootstrapReportBuilder helps build a BootstrapReport incrementally.
type BootstrapReportBuilder struct {
	report *BootstrapReport
}

// NewBootstrapReportBuilder creates a new BootstrapReportBuilder.
func NewBootstrapReportBuilder() *BootstrapReportBuilder {
	return &BootstrapReportBuilder{
		report: NewBootstrapReport(),
	}
}

// SetSources sets the source summary.
func (b *BootstrapReportBuilder) SetSources(scanned, included, skipped int, skipReasons map[string]int) *BootstrapReportBuilder {
	b.report.Sources = SourcesSummary{
		Scanned:     scanned,
		Included:    included,
		Skipped:     skipped,
		SkipReasons: skipReasons,
	}
	return b
}

// SetPages sets the pages summary.
func (b *BootstrapReportBuilder) SetPages(generated, stubs, collisions int) *BootstrapReportBuilder {
	b.report.Pages = PagesSummary{
		Generated:          generated,
		Stubs:              stubs,
		CollisionsResolved: collisions,
	}
	return b
}

// SetNavigation sets the navigation summary.
func (b *BootstrapReportBuilder) SetNavigation(homeGenerated, sidebarGenerated, allReachable bool) *BootstrapReportBuilder {
	b.report.Navigation = NavSummary{
		HomeGenerated:    homeGenerated,
		SidebarGenerated: sidebarGenerated,
		AllPagesReachable: allReachable,
	}
	return b
}

// SetPublish sets the publish summary.
func (b *BootstrapReportBuilder) SetPublish(status string, pagesPublished int, commit string) *BootstrapReportBuilder {
	b.report.Publish = PublishSummary{
		Status:         status,
		PagesPublished: pagesPublished,
		Commit:         commit,
	}
	return b
}

// Build returns the final BootstrapReport.
func (b *BootstrapReportBuilder) Build() *BootstrapReport {
	return b.report
}

// Emit saves the report to both JSON and Markdown files.
func (b *BootstrapReportBuilder) Emit(outputDir string) (jsonPath, mdPath string, err error) {
	formatter := NewReportFormatter(outputDir)
	return formatter.EmitBoth(b.report, func(r any) string {
		return r.(*BootstrapReport).ToMarkdown()
	}, "bootstrap")
}

func boolToEmoji(b bool) string {
	if b {
		return "✅"
	}
	return "❌"
}
