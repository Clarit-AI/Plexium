package reports

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- BootstrapReport tests ---

func TestBootstrapReport_New(t *testing.T) {
	r := NewBootstrapReport()
	assert.Equal(t, "bootstrap", r.Type)
	assert.NotEmpty(t, r.Timestamp)
	assert.NotNil(t, r.Sources.SkipReasons)
}

func TestBootstrapReport_ToJSON(t *testing.T) {
	r := NewBootstrapReport()
	r.Sources.Scanned = 10
	r.Sources.Included = 8
	r.Pages.Generated = 5

	data, err := r.ToJSON()
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "bootstrap", parsed["type"])

	sources := parsed["sources"].(map[string]interface{})
	assert.Equal(t, float64(10), sources["scanned"])
	assert.Equal(t, float64(8), sources["included"])
}

func TestBootstrapReport_ToMarkdown(t *testing.T) {
	r := NewBootstrapReport()
	r.Sources.Scanned = 10
	r.Sources.Included = 8
	r.Sources.Skipped = 2
	r.Sources.SkipReasons = map[string]int{"vendor": 2}
	r.Pages.Generated = 5
	r.Navigation.HomeGenerated = true
	r.Navigation.SidebarGenerated = true
	r.Publish.Status = "success"
	r.Publish.PagesPublished = 5
	r.Publish.Commit = "abc123"

	md := r.ToMarkdown()
	assert.Contains(t, md, "# Bootstrap Report")
	assert.Contains(t, md, "Scanned: 10")
	assert.Contains(t, md, "vendor: 2")
	assert.Contains(t, md, "Generated: 5")
	assert.Contains(t, md, "abc123")
}

func TestBootstrapReportBuilder(t *testing.T) {
	r := NewBootstrapReportBuilder().
		SetSources(20, 15, 5, map[string]int{"test": 3, "vendor": 2}).
		SetPages(10, 2, 1).
		SetNavigation(true, true, true).
		SetPublish("success", 10, "def456").
		Build()

	assert.Equal(t, 20, r.Sources.Scanned)
	assert.Equal(t, 15, r.Sources.Included)
	assert.Equal(t, 10, r.Pages.Generated)
	assert.Equal(t, 2, r.Pages.Stubs)
	assert.True(t, r.Navigation.HomeGenerated)
	assert.Equal(t, "success", r.Publish.Status)
	assert.Equal(t, "def456", r.Publish.Commit)
}

// --- SyncReport tests ---

func TestSyncReport_New(t *testing.T) {
	r := NewSyncReport()
	assert.Equal(t, "sync", r.Type)
	assert.NotEmpty(t, r.Timestamp)
	assert.True(t, r.Idempotent) // default
}

func TestSyncReport_ToJSON(t *testing.T) {
	r := NewSyncReport()
	r.Trigger = TriggerManual
	r.Changes.SourceFilesChanged = 3
	r.Changes.PagesRewritten = 2

	data, err := r.ToJSON()
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "sync", parsed["type"])
	assert.Equal(t, "manual", parsed["trigger"])
}

func TestSyncReport_ToMarkdown(t *testing.T) {
	r := NewSyncReport()
	r.Trigger = TriggerCI
	r.Commit = "abc123"
	r.Changes.SourceFilesChanged = 5
	r.Changes.PagesImpacted = 3
	r.Changes.PagesRewritten = 2
	r.Changes.PagesSkipped = 1
	r.Changes.SkipReason = "human-authored"
	r.Navigation.SidebarUpdated = true
	r.Publish.Status = "skipped"

	md := r.ToMarkdown()
	assert.Contains(t, md, "# Sync Report")
	assert.Contains(t, md, "ci")
	assert.Contains(t, md, "abc123")
	assert.Contains(t, md, "Source Files Changed: 5")
	assert.Contains(t, md, "human-authored")
}

func TestSyncReportBuilder(t *testing.T) {
	r := NewSyncReportBuilder().
		SetTrigger(TriggerPushToMain).
		SetCommit("sha1").
		SetChanges(10, 5, 3, 2, 1, "").
		SetNavigation(true, false).
		SetIdempotent(false).
		SetPublish("success", 8).
		Build()

	assert.Equal(t, TriggerPushToMain, r.Trigger)
	assert.Equal(t, "sha1", r.Commit)
	assert.Equal(t, 10, r.Changes.SourceFilesChanged)
	assert.False(t, r.Idempotent)
	assert.Equal(t, 8, r.Publish.PagesPublished)
}

// --- ReportFormatter tests ---

func TestReportFormatter_EmitJSON(t *testing.T) {
	dir := t.TempDir()
	f := NewReportFormatter(dir)

	report := NewBootstrapReport()
	report.Sources.Scanned = 42

	err := f.EmitJSON(report, "test-report.json")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "test-report.json"))
	require.NoError(t, err)

	var parsed BootstrapReport
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, 42, parsed.Sources.Scanned)
}

func TestReportFormatter_EmitMarkdown(t *testing.T) {
	dir := t.TempDir()
	f := NewReportFormatter(dir)

	err := f.EmitMarkdown("# Test\n\nHello", "test-report.md")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "test-report.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "# Test")
}

func TestReportFormatter_EmitBoth(t *testing.T) {
	dir := t.TempDir()
	f := NewReportFormatter(dir)

	report := NewBootstrapReport()
	report.Sources.Scanned = 7

	jsonPath, mdPath, err := f.EmitBoth(report, func(r any) string {
		return r.(*BootstrapReport).ToMarkdown()
	}, "bootstrap")
	require.NoError(t, err)

	assert.FileExists(t, jsonPath)
	assert.FileExists(t, mdPath)
}

func TestReportFormatter_DefaultDir(t *testing.T) {
	f := NewReportFormatter("")
	assert.Equal(t, ".plexium/reports", f.outputDir)
}

func TestBootstrapReportBuilder_Emit(t *testing.T) {
	dir := t.TempDir()

	b := NewBootstrapReportBuilder().
		SetSources(5, 3, 2, nil).
		SetPages(3, 0, 0).
		SetNavigation(true, true, true).
		SetPublish("done", 3, "")

	jsonPath, mdPath, err := b.Emit(dir)
	require.NoError(t, err)
	assert.FileExists(t, jsonPath)
	assert.FileExists(t, mdPath)
}

func TestSyncReportBuilder_Emit(t *testing.T) {
	dir := t.TempDir()

	b := NewSyncReportBuilder().
		SetTrigger(TriggerManual).
		SetCommit("abc").
		SetChanges(1, 1, 1, 1, 0, "")

	jsonPath, mdPath, err := b.Emit(dir)
	require.NoError(t, err)
	assert.FileExists(t, jsonPath)
	assert.FileExists(t, mdPath)
}

// --- Utility tests ---

func TestParseReportType(t *testing.T) {
	assert.Equal(t, "bootstrap", ParseReportType("bootstrap-2026-04-06.json"))
	assert.Equal(t, "sync", ParseReportType("sync-2026-04-06T12-00-00Z.md"))
	assert.Equal(t, "lint", ParseReportType("/some/path/lint-report.json"))
}

func TestFormatTimestamp(t *testing.T) {
	ts := FormatTimestamp()
	assert.NotEmpty(t, ts)
	assert.Contains(t, ts, "T") // ISO-like format with T separator
}

func TestFormatTimestampRFC3339(t *testing.T) {
	ts := FormatTimestampRFC3339()
	assert.NotEmpty(t, ts)
	assert.Contains(t, ts, "T")
	assert.Contains(t, ts, "Z")
}

func TestBoolToEmoji(t *testing.T) {
	assert.Contains(t, boolToEmoji(true), "✅")
	assert.Contains(t, boolToEmoji(false), "❌")
}
