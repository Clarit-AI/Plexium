package reports

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReportFormatter handles emitting reports in multiple formats.
type ReportFormatter struct {
	outputDir string
}

// NewReportFormatter creates a new ReportFormatter.
func NewReportFormatter(outputDir string) *ReportFormatter {
	if outputDir == "" {
		outputDir = ".plexium/reports"
	}
	return &ReportFormatter{outputDir: outputDir}
}

// EmitJSON writes a report to a JSON file.
func (f *ReportFormatter) EmitJSON(report any, filename string) error {
	if err := os.MkdirAll(f.outputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report to JSON: %w", err)
	}

	path := filepath.Join(f.outputDir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing JSON report: %w", err)
	}

	return nil
}

// EmitMarkdown writes a report to a Markdown file.
func (f *ReportFormatter) EmitMarkdown(content string, filename string) error {
	if err := os.MkdirAll(f.outputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	path := filepath.Join(f.outputDir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing Markdown report: %w", err)
	}

	return nil
}

// EmitBoth emits a report in both JSON and Markdown formats.
// The report parameter is used for JSON emission.
// The markdownFn parameter generates the Markdown content from the report.
func (f *ReportFormatter) EmitBoth(report any, markdownFn func(any) string, reportType string) (jsonPath string, mdPath string, err error) {
	timestamp := time.Now().UTC().Format("2006-01-02T15-04-05Z")

	// Emit JSON
	jsonFilename := fmt.Sprintf("%s-%s.json", reportType, timestamp)
	if err := f.EmitJSON(report, jsonFilename); err != nil {
		return "", "", err
	}
	jsonPath = filepath.Join(f.outputDir, jsonFilename)

	// Emit Markdown
	mdContent := markdownFn(report)
	mdFilename := fmt.Sprintf("%s-%s.md", reportType, timestamp)
	if err := f.EmitMarkdown(mdContent, mdFilename); err != nil {
		return "", "", err
	}
	mdPath = filepath.Join(f.outputDir, mdFilename)

	return jsonPath, mdPath, nil
}

// FormatTimestamp returns a formatted timestamp for report filenames.
func FormatTimestamp() string {
	return time.Now().UTC().Format("2006-01-02T15-04-05Z")
}

// FormatTimestampRFC3339 returns an RFC3339 formatted timestamp.
func FormatTimestampRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// Paths returns common report paths.
type Paths struct {
	JSON string
	MD   string
}

// ParseReportType extracts the report type from a filename.
func ParseReportType(filename string) string {
	filename = filepath.Base(filename)
	filename = strings.TrimSuffix(filename, filepath.Ext(filename))
	parts := strings.Split(filename, "-")
	if len(parts) >= 1 {
		return parts[0]
	}
	return ""
}
