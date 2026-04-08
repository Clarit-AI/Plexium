package convert

import (
	"fmt"
	"os"
	"time"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/plugins"
	"github.com/Clarit-AI/Plexium/internal/scanner"
	"path/filepath"
)

// Pipeline orchestrates the full convert workflow.
type Pipeline struct {
	repoRoot string
	cfg      *config.Config
	dryRun   bool
	depth    string
	agent    string
}

// PipelineResult holds the output of a full conversion run.
type PipelineResult struct {
	Pages          []PageData
	Report         *ConversionReport
	FilesWritten   []string
	ReportPath     string
	ReportJSONPath string
	AdapterRan     string
}

// PipelineOptions configures the pipeline.
type PipelineOptions struct {
	RepoRoot string
	Config   *config.Config
	DryRun   bool
	Depth    string // "shallow" or "deep"
	Agent    string // Optional: run specific adapter after conversion
}

// NewPipeline creates a new Pipeline.
func NewPipeline(opts PipelineOptions) *Pipeline {
	if opts.Depth == "" {
		opts.Depth = "shallow"
	}
	return &Pipeline{
		repoRoot: opts.RepoRoot,
		cfg:      opts.Config,
		dryRun:   opts.DryRun,
		depth:    opts.Depth,
		agent:    opts.Agent,
	}
}

// Run executes the full conversion pipeline.
func (p *Pipeline) Run() (*PipelineResult, error) {
	result := &PipelineResult{}

	// 1. Scour
	scourer, err := NewScourer(p.repoRoot)
	if err != nil {
		return nil, fmt.Errorf("scour init: %w", err)
	}

	findings, err := scourer.Scour(ScourOptions{Depth: p.depth})
	if err != nil {
		return nil, fmt.Errorf("scour: %w", err)
	}

	// 2. Filter
	var include, exclude []string
	if p.cfg != nil && len(p.cfg.Sources.Include) > 0 {
		include = p.cfg.Sources.Include
	}
	if p.cfg != nil && len(p.cfg.Sources.Exclude) > 0 {
		exclude = p.cfg.Sources.Exclude
	}

	filter, err := NewFilter(include, exclude)
	if err != nil {
		return nil, fmt.Errorf("filter init: %w", err)
	}

	// Scan all files for filtering
	s, err := scanner.New(
		[]string{"**/*"},
		[]string{"**/.git/**", "**/.wiki/**", "**/.plexium/**"},
	)
	if err != nil {
		return nil, fmt.Errorf("filter scanner: %w", err)
	}
	allFiles, err := s.Scan(p.repoRoot)
	if err != nil {
		return nil, fmt.Errorf("filter scan: %w", err)
	}

	filterResult := filter.Apply(allFiles)

	// 3. Ingest
	ingestor := NewIngestor()
	ingestResult, err := ingestor.Ingest(findings, filterResult)
	if err != nil {
		return nil, fmt.Errorf("ingest: %w", err)
	}

	// 4. Link
	linker := NewLinker()
	linker.AddPages(ingestResult.Pages)
	linkedPages := linker.GenerateCrossReferences(ingestResult.Pages)
	inbound, _ := linker.ComputeLinks(linkedPages)

	// 5. Lint
	linter := NewConvertLinter(linker)
	lintResult := linter.Analyze(linkedPages, filterResult.Eligible)

	// Add stub pages from lint to the page list
	allPages := append(linkedPages, lintResult.StubPages...)

	// 6. Report
	reportGen := NewReportGenerator()
	report := reportGen.Generate(allPages, filterResult, lintResult, inbound)

	result.Pages = allPages
	result.Report = report

	// 7. Write (if not dry-run)
	if err := p.writeOutput(result); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	// 8. Run agent adapter if specified
	if !p.dryRun && p.agent != "" {
		if err := p.runAdapter(p.agent); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: adapter %q failed: %v\n", p.agent, err)
		} else {
			result.AdapterRan = p.agent
		}
	}

	return result, nil
}

func (p *Pipeline) runAdapter(agent string) error {
	return plugins.RunAdapter(p.repoRoot, agent)
}

func (p *Pipeline) writeOutput(result *PipelineResult) error {
	wikiRoot := filepath.Join(p.repoRoot, ".wiki")
	plexiumDir := filepath.Join(p.repoRoot, ".plexium")
	outputDir := filepath.Join(plexiumDir, "output")

	targetDir := wikiRoot
	if p.dryRun {
		targetDir = outputDir
	}

	dr := manifest.NewDryRunner(p.dryRun, outputDir, os.Stdout)

	// Write wiki pages
	for _, page := range result.Pages {
		pagePath := filepath.Join(targetDir, page.WikiPath)
		if err := dr.WriteFile(pagePath, []byte(page.Content)); err != nil {
			return fmt.Errorf("writing page %s: %w", page.WikiPath, err)
		}
		result.FilesWritten = append(result.FilesWritten, page.WikiPath)
	}

	// Write reports
	ts := time.Now().Format("2006-01-02T150405")
	reportDir := filepath.Join(plexiumDir, "reports")
	if p.dryRun {
		reportDir = filepath.Join(outputDir, ".plexium", "reports")
	}

	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return fmt.Errorf("creating report dir: %w", err)
	}

	// JSON report
	jsonPath := filepath.Join(reportDir, fmt.Sprintf("conversion-%s.json", ts))
	jsonData, err := result.Report.ToJSON()
	if err != nil {
		return fmt.Errorf("serializing report: %w", err)
	}
	if err := os.WriteFile(jsonPath, jsonData, 0644); err != nil {
		return fmt.Errorf("writing JSON report: %w", err)
	}
	result.ReportJSONPath = jsonPath

	// Markdown report
	mdPath := filepath.Join(reportDir, fmt.Sprintf("conversion-%s.md", ts))
	mdData := result.Report.ToMarkdown()
	if err := os.WriteFile(mdPath, []byte(mdData), 0644); err != nil {
		return fmt.Errorf("writing markdown report: %w", err)
	}
	result.ReportPath = mdPath

	// Update manifest (if not dry-run)
	if !p.dryRun {
		if err := p.updateManifest(result.Pages); err != nil {
			return fmt.Errorf("updating manifest: %w", err)
		}
	}

	return nil
}

func (p *Pipeline) updateManifest(pages []PageData) error {
	manifestPath := manifest.DefaultPath(p.repoRoot)
	mgr, err := manifest.NewManager(manifestPath)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	for _, page := range pages {
		sourceFiles := make([]manifest.SourceFile, len(page.SourceFiles))
		for i, sf := range page.SourceFiles {
			hash, _ := manifest.ComputeHash(filepath.Join(p.repoRoot, sf))
			sourceFiles[i] = manifest.SourceFile{
				Path: sf,
				Hash: hash,
			}
		}

		entry := manifest.PageEntry{
			WikiPath:    page.WikiPath,
			Title:       page.Title,
			Ownership:   "managed",
			Section:     page.Section,
			SourceFiles: sourceFiles,
			LastUpdated: now,
			UpdatedBy:   "plexium-convert",
		}

		if err := mgr.UpsertPage(entry); err != nil {
			return fmt.Errorf("upserting page %s: %w", page.WikiPath, err)
		}
	}

	return nil
}
