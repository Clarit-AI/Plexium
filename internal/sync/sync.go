package sync

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Clarit-AI/Plexium/internal/agent"
	"github.com/Clarit-AI/Plexium/internal/compile"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/regen"
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
	PagesRegenerated   int      `json:"pagesRegenerated"`
	RegenerationErrors []string `json:"regenerationErrors,omitempty"`
}

// ExitCode returns 1 if stale pages were found, 0 otherwise.
// Used by --ci mode to signal CI pipeline failures.
func (r *SyncResult) ExitCode() int {
	if r.StalePages > 0 {
		return 1
	}
	return 0
}

// Options configures a sync run.
type Options struct {
	RepoRoot   string
	Config     *config.Config
	DryRun     bool
	Regenerate bool
	Cascade    *agent.ProviderCascade // required when Regenerate=true
}

// Run performs an incremental sync: detects changed source files, updates
// manifest hashes for stale pages, and recompiles navigation files.
func Run(opts Options) (*SyncResult, error) {
	if opts.DryRun && opts.Regenerate {
		return nil, fmt.Errorf("cannot regenerate in dry-run mode")
	}

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

	// Collect affected page paths
	for _, p := range stalePages {
		result.PagesAffected = append(result.PagesAffected, p.WikiPath)
	}

	// Always scan for new source files, even when no pages are stale
	if opts.Config != nil {
		newFiles, err := detectNewSources(opts.RepoRoot, opts.Config, m)
		if err != nil {
			return nil, fmt.Errorf("detecting new sources: %w", err)
		}
		for _, f := range newFiles {
			result.PagesAffected = append(result.PagesAffected, fmt.Sprintf("(new source) %s", f))
		}
	}

	// Regenerate stale pages via LLM provider cascade when requested.
	if opts.Regenerate && len(stalePages) > 0 {
		if opts.Cascade == nil {
			return nil, fmt.Errorf("no LLM providers configured; --regenerate requires at least one provider in the cascade")
		}

		wikiRoot := ".wiki"
		if opts.Config != nil && opts.Config.Wiki.Root != "" {
			wikiRoot = opts.Config.Wiki.Root
		}

		for _, page := range stalePages {
			regenResult, regenErr := regen.RegeneratePage(context.Background(), regen.PageRegenOptions{
				RepoRoot: opts.RepoRoot,
				WikiRoot: wikiRoot,
				Page:     page,
				Config:   opts.Config,
				Cascade:  opts.Cascade,
			})
			if regenErr != nil {
				errMsg := fmt.Sprintf("%s: %v", page.WikiPath, regenErr)
				result.RegenerationErrors = append(result.RegenerationErrors, errMsg)
				continue
			}
			if regenResult.Written {
				result.PagesRegenerated++
			}
		}
	}

	if len(stalePages) == 0 {
		return result, nil
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

		if err := mgr.UpsertPage(stalePage); err != nil {
			return nil, fmt.Errorf("updating page %s: %w", stalePage.WikiPath, err)
		}
	}
	result.HashesUpdated = updated

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
