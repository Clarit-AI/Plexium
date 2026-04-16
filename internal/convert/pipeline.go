package convert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/plugins"
	"github.com/Clarit-AI/Plexium/internal/scanner"
)

// Pipeline orchestrates the full convert workflow.
type Pipeline struct {
	repoRoot string
	cfg      *config.Config
	dryRun   bool
	depth    string
	agent    string
	registry *plugins.Registry
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
	Depth    string            // "shallow" or "deep"
	Agent    string            // Optional: run specific adapter after conversion
	Registry *plugins.Registry // Optional: plugin registry for pipeline hooks
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
		registry: opts.Registry,
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
	exclude = append(exclude, loadRepoIgnorePatterns(p.repoRoot)...)

	filter, err := NewFilter(include, exclude)
	if err != nil {
		return nil, fmt.Errorf("filter init: %w", err)
	}

	// Scan all files for filtering
	s, err := scanner.New(
		[]string{"**/*"},
		append([]string{"**/.git/**", "**/.wiki/**", "**/.plexium/**"}, loadRepoIgnorePatterns(p.repoRoot)...),
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

	// Plugin hook: after-ingest
	if err := p.runPlugins(plugins.StageAfterIngest, ingestResult.Pages); err != nil {
		return nil, fmt.Errorf("plugins after-ingest: %w", err)
	}

	// 4. Link
	linker := NewLinker()
	linker.AddPages(ingestResult.Pages)
	linkedPages := linker.GenerateCrossReferences(ingestResult.Pages)
	inbound, _ := linker.ComputeLinks(linkedPages)

	// Plugin hook: after-link
	if err := p.runPlugins(plugins.StageAfterLink, linkedPages); err != nil {
		return nil, fmt.Errorf("plugins after-link: %w", err)
	}

	// 5. Lint
	linter := NewConvertLinter(linker)
	lintResult := linter.Analyze(linkedPages, filterResult.Eligible)

	// Add stub pages from lint to the page list
	allPages := append(linkedPages, lintResult.StubPages...)

	// Plugin hook: after-lint (runs after stub pages are merged so plugins
	// see the complete lint output).
	if err := p.runPlugins(plugins.StageAfterLint, allPages); err != nil {
		return nil, fmt.Errorf("plugins after-lint: %w", err)
	}

	// 6. Report
	reportGen := NewReportGenerator()
	report := reportGen.Generate(allPages, filterResult, lintResult, inbound)

	result.Pages = allPages
	result.Report = report

	// 7. Write (if not dry-run)
	if err := p.writeOutput(result); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	// 8. Plugin hook: after-write
	if !p.dryRun {
		if err := p.runPlugins(plugins.StageAfterWrite, allPages); err != nil {
			return nil, fmt.Errorf("plugins after-write: %w", err)
		}
	}

	// 9. Run agent adapter if specified
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

// runPlugins executes all pipeline plugins registered for the given stage.
// Mutations made by plugins to data.Pages are copied back into the caller's
// pages slice so subsequent pipeline stages see them.
//
// NOTE: uses context.Background() for now. Threading a caller context through
// Pipeline.Run() is tracked as a follow-up since it touches many call sites.
func (p *Pipeline) runPlugins(stage plugins.PipelineStage, pages []PageData) error {
	if p.registry == nil {
		return nil
	}
	pps := p.registry.PipelinePlugins(stage)
	if len(pps) == 0 {
		return nil
	}

	wikiRoot := filepath.Join(p.repoRoot, ".wiki")
	if p.cfg != nil && p.cfg.Wiki.Root != "" {
		wikiRoot = filepath.Join(p.repoRoot, p.cfg.Wiki.Root)
	}

	pipelinePages := make([]plugins.PipelinePage, len(pages))
	for i, pg := range pages {
		// Deep-copy SourceFiles so plugin mutations don't alias the caller.
		srcCopy := append([]string(nil), pg.SourceFiles...)
		pipelinePages[i] = plugins.PipelinePage{
			WikiPath:    pg.WikiPath,
			Title:       pg.Title,
			Section:     pg.Section,
			Content:     pg.Content,
			SourceFiles: srcCopy,
		}
	}

	data := &plugins.PipelineData{
		RepoRoot: p.repoRoot,
		WikiRoot: wikiRoot,
		Pages:    pipelinePages,
	}

	ctx := context.Background()
	for _, pp := range pps {
		if err := pp.Process(ctx, data); err != nil {
			return fmt.Errorf("plugin %s: %w", pp.Name(), err)
		}
	}

	// Copy plugin mutations back into the caller's pages slice. We only
	// propagate positions that existed before the hook — plugins cannot
	// currently add or remove pages via the pipeline data.
	n := len(pages)
	if len(data.Pages) < n {
		n = len(data.Pages)
	}
	for i := 0; i < n; i++ {
		pages[i].WikiPath = data.Pages[i].WikiPath
		pages[i].Title = data.Pages[i].Title
		pages[i].Section = data.Pages[i].Section
		pages[i].Content = data.Pages[i].Content
		pages[i].SourceFiles = append([]string(nil), data.Pages[i].SourceFiles...)
	}
	return nil
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

	m, err := mgr.Load()
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	generatedEntries := make(map[string]manifest.PageEntry, len(pages))

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

		generatedEntries[page.WikiPath] = entry
	}

	existing := make([]manifest.PageEntry, 0, len(m.Pages)+len(generatedEntries))
	wikiRoot := configuredManifestWikiRoot(p.cfg)
	for _, page := range m.Pages {
		if entry, ok := generatedEntries[page.WikiPath]; ok {
			if page.Ownership == "human-authored" && entry.Ownership == "managed" {
				return fmt.Errorf("cannot overwrite human-authored page: %s", page.WikiPath)
			}
			continue
		}
		if page.Ownership == "managed" && manifestPathIncludesWikiRoot(page.WikiPath, wikiRoot) {
			continue
		}
		if page.Ownership == "managed" && page.UpdatedBy == "plexium-convert" {
			continue
		}
		existing = append(existing, page)
	}

	for _, entry := range generatedEntries {
		existing = append(existing, entry)
	}

	m.Pages = existing
	return mgr.Save(m)
}

func configuredManifestWikiRoot(cfg *config.Config) string {
	if cfg != nil {
		if root := strings.TrimSpace(cfg.Wiki.Root); root != "" {
			clean := filepath.ToSlash(filepath.Clean(root))
			if clean != "." {
				return clean
			}
		}
	}
	return ".wiki"
}

func manifestPathIncludesWikiRoot(wikiPath, wikiRoot string) bool {
	cleanPath := filepath.ToSlash(filepath.Clean(strings.TrimSpace(wikiPath)))
	cleanRoot := filepath.ToSlash(filepath.Clean(strings.TrimSpace(wikiRoot)))
	return cleanPath == cleanRoot || strings.HasPrefix(cleanPath, cleanRoot+"/")
}
