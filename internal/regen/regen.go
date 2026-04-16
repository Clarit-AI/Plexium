// Package regen provides wiki page content regeneration using the LLM provider
// cascade. It is designed to be called by "plexium sync --regenerate" and does
// NOT depend on internal/daemon.
package regen

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/internal/agent"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
)

// PageRegenOptions configures regeneration of a single wiki page.
type PageRegenOptions struct {
	RepoRoot     string
	WikiRoot     string // default ".wiki"
	Page         manifest.PageEntry
	Config       *config.Config
	Cascade      *agent.ProviderCascade
	ContextPages []string // optional wiki pages for context
}

// PageRegenResult captures the outcome of regenerating one page.
type PageRegenResult struct {
	WikiPath   string  `json:"wikiPath"`
	Provider   string  `json:"provider"`
	TokensUsed int     `json:"tokensUsed"`
	CostUSD    float64 `json:"costUSD"`
	LatencyMs  int64   `json:"latencyMs"`
	Written    bool    `json:"written"`
	Error      string  `json:"error,omitempty"`
}

// regenResponse is the expected JSON format from the LLM.
type regenResponse struct {
	Content string `json:"content"`
}

// RegeneratePage regenerates a single wiki page using the LLM provider cascade.
func RegeneratePage(ctx context.Context, opts PageRegenOptions) (*PageRegenResult, error) {
	if opts.Cascade == nil {
		return nil, fmt.Errorf("regen: cascade must not be nil")
	}

	wikiRoot := opts.WikiRoot
	if wikiRoot == "" {
		wikiRoot = configuredWikiRoot(opts.Config)
	}

	// Validate output path safety.
	outRel := filepath.Join(wikiRoot, opts.Page.WikiPath)
	if !isPathWithinRoot(outRel, wikiRoot) {
		return nil, fmt.Errorf("regen: wiki path %q escapes wiki root %q", opts.Page.WikiPath, wikiRoot)
	}

	prompt := BuildRegenPrompt(opts.Config, opts.Page, opts.RepoRoot, wikiRoot, opts.ContextPages)

	start := time.Now()
	completion, err := opts.Cascade.Complete(ctx, prompt)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return &PageRegenResult{
			WikiPath:  opts.Page.WikiPath,
			LatencyMs: latency,
			Error:     err.Error(),
		}, err
	}

	// Parse response: try JSON, fall back to raw markdown.
	var content string
	var resp regenResponse
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(completion.Response)), &resp); jsonErr == nil && resp.Content != "" {
		content = resp.Content
	} else {
		content = strings.TrimSpace(completion.Response)
	}

	// Write the regenerated content.
	absPath := filepath.Join(opts.RepoRoot, wikiRoot, opts.Page.WikiPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, fmt.Errorf("regen: create directory: %w", err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("regen: write file: %w", err)
	}

	return &PageRegenResult{
		WikiPath:   opts.Page.WikiPath,
		Provider:   completion.Provider,
		TokensUsed: completion.TokensUsed,
		CostUSD:    completion.CostUSD,
		LatencyMs:  latency,
		Written:    true,
	}, nil
}

// BuildRegenPrompt constructs the LLM prompt for regenerating a wiki page.
func BuildRegenPrompt(cfg *config.Config, page manifest.PageEntry, repoRoot, wikiRoot string, contextPages []string) string {
	var b strings.Builder

	b.WriteString("You are regenerating a wiki page for a source code repository.\n")
	b.WriteString("Return ONLY valid JSON: {\"content\": \"# Page Title\\nMarkdown content...\"}\n")
	b.WriteString("If you cannot produce JSON, return raw markdown.\n\n")

	fmt.Fprintf(&b, "Wiki path: %s\n", page.WikiPath)
	fmt.Fprintf(&b, "Wiki root: %s\n", wikiRoot)
	b.WriteString("Never write outside the wiki root.\n\n")

	// Include source file contents.
	if len(page.SourceFiles) > 0 {
		b.WriteString("Source files:\n")
		for _, sf := range page.SourceFiles {
			srcPath := filepath.Join(repoRoot, sf.Path)
			data, err := os.ReadFile(srcPath)
			if err != nil {
				fmt.Fprintf(&b, "- %s (unreadable: %v)\n", sf.Path, err)
				continue
			}
			fmt.Fprintf(&b, "- %s:\n```\n%s\n```\n", sf.Path, truncateString(string(data), 3500))
		}
		b.WriteString("\n")
	}

	// Include existing wiki page content if it exists.
	existingPath := filepath.Join(repoRoot, wikiRoot, page.WikiPath)
	if data, err := os.ReadFile(existingPath); err == nil {
		b.WriteString("Existing wiki page content:\n```md\n")
		b.WriteString(truncateString(string(data), 1800))
		b.WriteString("\n```\n\n")
	}

	// Include context pages.
	if len(contextPages) > 0 {
		b.WriteString("Related wiki pages for context:\n")
		for _, cp := range contextPages {
			cpPath := filepath.Join(repoRoot, wikiRoot, cp)
			data, err := os.ReadFile(cpPath)
			if err != nil {
				fmt.Fprintf(&b, "- %s (unreadable)\n", cp)
				continue
			}
			fmt.Fprintf(&b, "- %s:\n```md\n%s\n```\n", cp, truncateString(string(data), 1800))
		}
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// Helpers (duplicated from internal/daemon to avoid import dependency)
// ---------------------------------------------------------------------------

// truncateString truncates a string to at most max runes.
func truncateString(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

// isPathWithinRoot checks that path does not escape root via ".." traversal.
func isPathWithinRoot(path, root string) bool {
	cleanPath := filepath.ToSlash(filepath.Clean(path))
	cleanRoot := filepath.ToSlash(filepath.Clean(root))
	return cleanPath == cleanRoot || strings.HasPrefix(cleanPath, cleanRoot+"/")
}

// configuredWikiRoot resolves the wiki root from config, defaulting to ".wiki".
func configuredWikiRoot(cfg *config.Config) string {
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
