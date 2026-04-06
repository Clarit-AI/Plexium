package reports

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SyncTrigger represents what triggered the sync.
type SyncTrigger string

const (
	TriggerPushToMain SyncTrigger = "push_to_main"
	TriggerManual     SyncTrigger = "manual"
	TriggerScheduled  SyncTrigger = "scheduled"
	TriggerCI         SyncTrigger = "ci"
)

// SyncReport is generated after plexium sync.
type SyncReport struct {
	Type        string          `json:"type"`
	Timestamp   string          `json:"timestamp"`
	Trigger     SyncTrigger     `json:"trigger"`
	Commit      string          `json:"commit"`
	Changes     ChangesSummary  `json:"changes"`
	Navigation  SyncNavSummary  `json:"navigation"`
	Idempotent  bool            `json:"idempotent"`
	Publish     PublishSummary  `json:"publish"`
}

// SyncNavSummary is the navigation summary for sync reports.
type SyncNavSummary struct {
	SidebarUpdated bool `json:"sidebarUpdated"`
	HomeUpdated   bool `json:"homeUpdated"`
}

// ChangesSummary summarizes changes detected during sync.
type ChangesSummary struct {
	SourceFilesChanged int    `json:"sourceFilesChanged"`
	WikiRelevant       int    `json:"wikiRelevant"`
	PagesImpacted      int    `json:"pagesImpacted"`
	PagesRewritten     int    `json:"pagesRewritten"`
	PagesSkipped       int    `json:"pagesSkipped"`
	SkipReason         string `json:"skipReason,omitempty"`
}

// NewSyncReport creates a new SyncReport.
func NewSyncReport() *SyncReport {
	return &SyncReport{
		Type:      "sync",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Idempotent: true,
	}
}

// ToJSON formats the sync report as JSON.
func (r *SyncReport) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// ToMarkdown formats the sync report as human-readable Markdown.
func (r *SyncReport) ToMarkdown() string {
	var sb strings.Builder

	sb.WriteString("# Sync Report\n\n")
	sb.WriteString(fmt.Sprintf("**Generated:** %s\n", r.Timestamp))
	sb.WriteString(fmt.Sprintf("**Trigger:** %s\n", r.Trigger))
	sb.WriteString(fmt.Sprintf("**Commit:** `%s`\n\n", r.Commit))

	sb.WriteString("## Changes\n\n")
	sb.WriteString(fmt.Sprintf("- Source Files Changed: %d\n", r.Changes.SourceFilesChanged))
	sb.WriteString(fmt.Sprintf("- Wiki Relevant: %d\n", r.Changes.WikiRelevant))
	sb.WriteString(fmt.Sprintf("- Pages Impacted: %d\n", r.Changes.PagesImpacted))
	sb.WriteString(fmt.Sprintf("- Pages Rewritten: %d\n", r.Changes.PagesRewritten))
	sb.WriteString(fmt.Sprintf("- Pages Skipped: %d\n", r.Changes.PagesSkipped))
	if r.Changes.SkipReason != "" {
		sb.WriteString(fmt.Sprintf("- Skip Reason: %s\n", r.Changes.SkipReason))
	}

	sb.WriteString("\n## Navigation\n\n")
	sb.WriteString(fmt.Sprintf("- Sidebar Updated: %v\n", boolToEmoji(r.Navigation.SidebarUpdated)))
	sb.WriteString(fmt.Sprintf("- Home Updated: %v\n", boolToEmoji(r.Navigation.HomeUpdated)))

	sb.WriteString("\n## Idempotent\n\n")
	sb.WriteString(fmt.Sprintf("- %v\n", boolToEmoji(r.Idempotent)))

	sb.WriteString("\n## Publish\n\n")
	sb.WriteString(fmt.Sprintf("- Status: %s\n", r.Publish.Status))
	sb.WriteString(fmt.Sprintf("- Pages Published: %d\n", r.Publish.PagesPublished))

	return sb.String()
}

// SyncReportBuilder helps build a SyncReport incrementally.
type SyncReportBuilder struct {
	report *SyncReport
}

// NewSyncReportBuilder creates a new SyncReportBuilder.
func NewSyncReportBuilder() *SyncReportBuilder {
	return &SyncReportBuilder{
		report: NewSyncReport(),
	}
}

// SetTrigger sets the sync trigger.
func (b *SyncReportBuilder) SetTrigger(trigger SyncTrigger) *SyncReportBuilder {
	b.report.Trigger = trigger
	return b
}

// SetCommit sets the commit SHA.
func (b *SyncReportBuilder) SetCommit(commit string) *SyncReportBuilder {
	b.report.Commit = commit
	return b
}

// SetChanges sets the changes summary.
func (b *SyncReportBuilder) SetChanges(sourceFilesChanged, wikiRelevant, pagesImpacted, pagesRewritten, pagesSkipped int, skipReason string) *SyncReportBuilder {
	b.report.Changes = ChangesSummary{
		SourceFilesChanged: sourceFilesChanged,
		WikiRelevant:       wikiRelevant,
		PagesImpacted:      pagesImpacted,
		PagesRewritten:     pagesRewritten,
		PagesSkipped:       pagesSkipped,
		SkipReason:         skipReason,
	}
	return b
}

// SetNavigation sets the navigation update summary.
func (b *SyncReportBuilder) SetNavigation(sidebarUpdated, homeUpdated bool) *SyncReportBuilder {
	b.report.Navigation = SyncNavSummary{
		SidebarUpdated: sidebarUpdated,
		HomeUpdated:   homeUpdated,
	}
	return b
}

// SetIdempotent sets whether the sync was idempotent.
func (b *SyncReportBuilder) SetIdempotent(idempotent bool) *SyncReportBuilder {
	b.report.Idempotent = idempotent
	return b
}

// SetPublish sets the publish summary.
func (b *SyncReportBuilder) SetPublish(status string, pagesPublished int) *SyncReportBuilder {
	b.report.Publish = PublishSummary{
		Status:         status,
		PagesPublished: pagesPublished,
	}
	return b
}

// Build returns the final SyncReport.
func (b *SyncReportBuilder) Build() *SyncReport {
	return b.report
}

// Emit saves the report to both JSON and Markdown files.
func (b *SyncReportBuilder) Emit(outputDir string) (jsonPath, mdPath string, err error) {
	formatter := NewReportFormatter(outputDir)
	return formatter.EmitBoth(b.report, func(r any) string {
		return r.(*SyncReport).ToMarkdown()
	}, "sync")
}
