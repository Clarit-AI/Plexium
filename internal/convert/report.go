package convert

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ConversionReport is the structured output of a conversion run.
type ConversionReport struct {
	Type       string         `json:"type"`
	Timestamp  string         `json:"timestamp"`
	Sources    SourcesSummary `json:"sources"`
	Pages      PagesSummary   `json:"pages"`
	Gaps       []GapEntry     `json:"gaps"`
	Stubs      []StubEntry    `json:"stubs"`
	CrossRefs  int            `json:"crossRefsGenerated"`
	GapScore   float64        `json:"gapScore"`
}

// SourcesSummary summarizes source file processing.
type SourcesSummary struct {
	Scanned     int            `json:"scanned"`
	Included    int            `json:"included"`
	Skipped     int            `json:"skipped"`
	SkipReasons map[string]int `json:"skipReasons"`
}

// PagesSummary summarizes generated pages.
type PagesSummary struct {
	Generated int `json:"generated"`
	Stubs     int `json:"stubs"`
}

// GapEntry represents a detected gap in documentation.
type GapEntry struct {
	Type   string `json:"type"` // "undocumented_module", "orphan", "missing_crossref"
	Target string `json:"target"`
	Detail string `json:"detail"`
}

// StubEntry represents a stub page created during conversion.
type StubEntry struct {
	WikiPath string `json:"wikiPath"`
	Title    string `json:"title"`
	Reason   string `json:"reason"`
}

// ReportGenerator creates conversion reports.
type ReportGenerator struct{}

// NewReportGenerator creates a new ReportGenerator.
func NewReportGenerator() *ReportGenerator {
	return &ReportGenerator{}
}

// Generate creates a ConversionReport from pipeline results.
func (g *ReportGenerator) Generate(
	pages []PageData,
	filter *FilterResult,
	lint *LintResult,
	inbound map[string][]string,
) *ConversionReport {
	report := &ConversionReport{
		Type:      "conversion",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// Sources summary
	skipReasonCounts := make(map[string]int)
	for _, reason := range filter.SkipReasons {
		skipReasonCounts[reason]++
	}
	report.Sources = SourcesSummary{
		Scanned:     len(filter.Eligible) + len(filter.Skipped),
		Included:    len(filter.Eligible),
		Skipped:     len(filter.Skipped),
		SkipReasons: skipReasonCounts,
	}

	// Pages summary
	stubCount := 0
	for _, p := range pages {
		if p.IsStub {
			stubCount++
		}
	}
	report.Pages = PagesSummary{
		Generated: len(pages),
		Stubs:     stubCount,
	}

	// Gaps
	for _, mod := range lint.UndocumentedModules {
		report.Gaps = append(report.Gaps, GapEntry{
			Type:   "undocumented_module",
			Target: mod,
			Detail: fmt.Sprintf("Source directory '%s' has no wiki page", mod),
		})
	}
	for _, orphan := range lint.Orphans {
		report.Gaps = append(report.Gaps, GapEntry{
			Type:   "orphan",
			Target: orphan,
			Detail: fmt.Sprintf("Page '%s' has no inbound links", orphan),
		})
	}
	for _, ref := range lint.MissingCrossRefs {
		report.Gaps = append(report.Gaps, GapEntry{
			Type:   "missing_crossref",
			Target: ref.ToPage,
			Detail: fmt.Sprintf("Suggested link from '%s' to '%s': %s", ref.FromPage, ref.ToPage, ref.Reason),
		})
	}

	// Stubs
	for _, p := range pages {
		if p.IsStub {
			report.Stubs = append(report.Stubs, StubEntry{
				WikiPath: p.WikiPath,
				Title:    p.Title,
				Reason:   "detected during conversion",
			})
		}
	}
	for _, sp := range lint.StubPages {
		report.Stubs = append(report.Stubs, StubEntry{
			WikiPath: sp.WikiPath,
			Title:    sp.Title,
			Reason:   "undocumented module",
		})
	}

	// Cross-ref count
	totalRefs := 0
	for _, refs := range inbound {
		totalRefs += len(refs)
	}
	report.CrossRefs = totalRefs
	report.GapScore = lint.GapScore

	return report
}

// ToJSON serializes the report to JSON.
func (r *ConversionReport) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// ToMarkdown renders the report as human-readable Markdown.
func (r *ConversionReport) ToMarkdown() string {
	var b strings.Builder

	b.WriteString("# Conversion Report\n\n")
	b.WriteString(fmt.Sprintf("**Generated:** %s\n\n", r.Timestamp))

	b.WriteString("## Sources\n\n")
	b.WriteString(fmt.Sprintf("| Metric | Count |\n"))
	b.WriteString(fmt.Sprintf("|--------|-------|\n"))
	b.WriteString(fmt.Sprintf("| Scanned | %d |\n", r.Sources.Scanned))
	b.WriteString(fmt.Sprintf("| Included | %d |\n", r.Sources.Included))
	b.WriteString(fmt.Sprintf("| Skipped | %d |\n", r.Sources.Skipped))
	b.WriteString("\n")

	if len(r.Sources.SkipReasons) > 0 {
		b.WriteString("### Skip Reasons\n\n")
		for reason, count := range r.Sources.SkipReasons {
			b.WriteString(fmt.Sprintf("- %s: %d\n", reason, count))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Pages\n\n")
	b.WriteString(fmt.Sprintf("- Generated: %d\n", r.Pages.Generated))
	b.WriteString(fmt.Sprintf("- Stubs: %d\n", r.Pages.Stubs))
	b.WriteString(fmt.Sprintf("- Cross-references: %d\n", r.CrossRefs))
	b.WriteString(fmt.Sprintf("- Gap score: %.0f%%\n", r.GapScore*100))
	b.WriteString("\n")

	if len(r.Gaps) > 0 {
		b.WriteString("## Gaps\n\n")
		for _, gap := range r.Gaps {
			b.WriteString(fmt.Sprintf("- **[%s]** %s\n", gap.Type, gap.Detail))
		}
		b.WriteString("\n")
	}

	if len(r.Stubs) > 0 {
		b.WriteString("## Stubs Created\n\n")
		for _, stub := range r.Stubs {
			b.WriteString(fmt.Sprintf("- `%s` — %s (%s)\n", stub.WikiPath, stub.Title, stub.Reason))
		}
		b.WriteString("\n")
	}

	return b.String()
}
