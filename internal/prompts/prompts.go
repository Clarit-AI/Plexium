package prompts

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/Clarit-AI/Plexium/internal/config"
)

const (
	ProfileConstrainedLocal     = "constrained-local"
	ProfileBalanced             = "balanced"
	ProfileFrontierLargeContext = "frontier-large-context"
	DefaultProfile              = ProfileBalanced
)

const (
	PromptInitialWikiPopulation = "assistive/initial-wiki-population.md"
	PromptRetriever             = "assistive/retriever.md"
	PromptDocumenter            = "assistive/documenter.md"
	PromptMaintenance           = "assistive/maintenance.md"
	PromptContradiction         = "assistive/contradiction.md"
	PromptMissingConcepts       = "assistive/missing-concepts.md"
	PromptCrossReference        = "assistive/cross-reference.md"
	PromptStaleness             = "assistive/staleness.md"
)

//go:embed assets/**
var embeddedAssets embed.FS

func RepoRoot(repoRoot string) string {
	return filepath.Join(repoRoot, ".plexium", "prompts")
}

func RepoPath(repoRoot, name string) string {
	return filepath.Join(RepoRoot(repoRoot), filepath.FromSlash(name))
}

func EnsureRepoPack(repoRoot string) ([]string, error) {
	root := RepoRoot(repoRoot)
	var created []string

	err := fs.WalkDir(embeddedAssets, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "assets" {
			return nil
		}

		rel := strings.TrimPrefix(path, "assets/")
		dest := filepath.Join(root, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("create prompt directory %s: %w", filepath.Dir(dest), err)
		}
		if _, err := os.Stat(dest); err == nil {
			return nil
		}

		data, err := embeddedAssets.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded prompt %s: %w", path, err)
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("write prompt %s: %w", dest, err)
		}
		created = append(created, dest)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(created)
	return created, nil
}

func Render(repoRoot, name, profile string, data any) (string, error) {
	raw, err := loadPrompt(repoRoot, name)
	if err != nil {
		return "", err
	}

	profile = NormalizeProfile(profile)
	if profile == "" {
		profile = DefaultProfile
	}

	overlay, err := loadPrompt(repoRoot, filepath.ToSlash(filepath.Join("profiles", profile+".md")))
	if err != nil {
		return "", err
	}

	body := stripFrontmatter(raw)
	overlayBody := strings.TrimSpace(stripFrontmatter(overlay))
	if overlayBody != "" {
		body = strings.TrimSpace(body) + "\n\nCapability profile guidance:\n" + overlayBody + "\n"
	}

	tpl, err := template.New(name).Option("missingkey=error").Parse(body)
	if err != nil {
		return "", fmt.Errorf("parse prompt template %s: %w", name, err)
	}

	var out strings.Builder
	if err := tpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render prompt template %s: %w", name, err)
	}
	return strings.TrimSpace(out.String()), nil
}

func NormalizeProfile(profile string) string {
	switch strings.TrimSpace(strings.ToLower(profile)) {
	case ProfileConstrainedLocal:
		return ProfileConstrainedLocal
	case ProfileFrontierLargeContext:
		return ProfileFrontierLargeContext
	case ProfileBalanced, "":
		return ProfileBalanced
	default:
		return ProfileBalanced
	}
}

func ProfileFromConfig(cfg *config.Config) string {
	if cfg == nil {
		return DefaultProfile
	}
	for _, provider := range cfg.AssistiveAgent.Providers {
		if !provider.Enabled {
			continue
		}
		if provider.CapabilityProfile != "" {
			if normalized := NormalizeProfile(provider.CapabilityProfile); normalized != "" {
				return normalized
			}
		}
		switch strings.ToLower(provider.Type) {
		case "ollama":
			return ProfileConstrainedLocal
		case "openai-compatible", "inherit":
			return ProfileBalanced
		}
	}
	return DefaultProfile
}

func loadPrompt(repoRoot, name string) (string, error) {
	repoPath := RepoPath(repoRoot, name)
	if data, err := os.ReadFile(repoPath); err == nil {
		return string(data), nil
	}
	data, err := embeddedAssets.ReadFile(filepath.ToSlash(filepath.Join("assets", name)))
	if err != nil {
		return "", fmt.Errorf("load prompt %s: %w", name, err)
	}
	return string(data), nil
}

func stripFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---\n") {
		return strings.TrimSpace(content)
	}
	rest := content[4:]
	idx := strings.Index(rest, "\n---\n")
	if idx == -1 {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(rest[idx+5:])
}
