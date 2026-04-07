package sync

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/Clarit-AI/Plexium/internal/compile"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/scanner"
)

// SyncResult captures the outcome of a sync operation.
type SyncResult struct {
	SourceFilesChecked int      `json:"sourceFilesChecked"`
	StalePages         int      `json:"stalePages"`
	HashesUpdated      int      `json:"hashesUpdated"`
	NavRecompiled      bool     `json:"navRecompiled"`
	DryRun             bool     `json:"dryRun"`
	PagesAffected      []string `json:"pagesAffected"`
}

// Options configures a sync run.
type Options struct {
	RepoRoot string
	Config   *config.Config
	DryRun   bool
}

// Run performs an incremental sync: detects changed source files, updates
// manifest hashes for stale pages, and recompiles navigation files.
func Run(opts Options) (*SyncResult, error) {
	result := &SyncResult{DryRun: opts.DryRun}

	mgr, err := manifest.NewManager(manifest.DefaultPath(opts.RepoRoot))
	if err != nil {
		return nil, fmt.Errorf("opening manifest: %w", err)
	}

	m, err := mgr.Load()
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}

	// Count total source files tracked in manifest
	sourceSet := make(map[string]bool)
	for _, page := range m.Pages {
		for _, sf := range page.SourceFiles {
			sourceSet[sf.Path] = true
		}
	}
	result.SourceFilesChecked = len(sourceSet)

	// Detect stale pages by comparing stored hashes to current file hashes
	stalePages, err := mgr.DetectStalePages(func(path string) (string, error) {
		return manifest.ComputeHash(filepath.Join(opts.RepoRoot, path))
	})
	if err != nil {
		return nil, fmt.Errorf("detecting stale pages: %w", err)
	}
	result.StalePages = len(stalePages)

	if len(stalePages) == 0 {
		return result, nil
	}

	// Collect affected page paths
	for _, p := range stalePages {
		result.PagesAffected = append(result.PagesAffected, p.WikiPath)
	}

	if opts.DryRun {
		return result, nil
	}

	// Update hashes for stale pages
	updated := 0
	for _, stalePage := range stalePages {
		newSources := make([]manifest.SourceFile, 0, len(stalePage.SourceFiles))
		for _, sf := range stalePage.SourceFiles {
			newHash, err := manifest.ComputeHash(filepath.Join(opts.RepoRoot, sf.Path))
			if err != nil {
				// Source file may have been deleted — keep old entry
				newSources = append(newSources, sf)
				continue
			}
			newSources = append(newSources, manifest.SourceFile{
				Path:                sf.Path,
				Hash:                newHash,
				LastProcessedCommit: sf.LastProcessedCommit,
			})
			updated++
		}
		stalePage.SourceFiles = newSources
		stalePage.LastUpdated = time.Now().UTC().Format(time.RFC3339)

		if err := mgr.UpsertPage(stalePage); err != nil {
			return nil, fmt.Errorf("updating page %s: %w", stalePage.WikiPath, err)
		}
	}
	result.HashesUpdated = updated

	// Scan for new source files not yet in the manifest
	if opts.Config != nil {
		newFiles, err := detectNewSources(opts.RepoRoot, opts.Config, m)
		if err == nil && len(newFiles) > 0 {
			// New source files are reported but not auto-ingested —
			// that requires `plexium convert`. This keeps sync fast and safe.
			for _, f := range newFiles {
				result.PagesAffected = append(result.PagesAffected, fmt.Sprintf("(new source) %s", f))
			}
		}
	}

	// Recompile navigation files
	compiler := compile.NewCompiler(opts.RepoRoot, false)
	if _, err := compiler.Compile(); err != nil {
		return nil, fmt.Errorf("recompiling navigation: %w", err)
	}
	result.NavRecompiled = true

	return result, nil
}

// detectNewSources finds source files matching config globs that aren't tracked in the manifest.
func detectNewSources(repoRoot string, cfg *config.Config, m *manifest.Manifest) ([]string, error) {
	s, err := scanner.New(cfg.Sources.Include, cfg.Sources.Exclude)
	if err != nil {
		return nil, err
	}

	files, err := s.Scan(repoRoot)
	if err != nil {
		return nil, err
	}

	// Build set of all tracked source paths
	tracked := make(map[string]bool)
	for _, page := range m.Pages {
		for _, sf := range page.SourceFiles {
			tracked[sf.Path] = true
		}
	}

	var newFiles []string
	for _, f := range files {
		if !tracked[f.Path] {
			newFiles = append(newFiles, f.Path)
		}
	}
	return newFiles, nil
}
