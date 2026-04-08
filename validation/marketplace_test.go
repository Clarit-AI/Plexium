package validation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMarketplaceArtifacts_AreValidJSON(t *testing.T) {
	repoRoot := currentRepoRoot(t)

	files := []string{
		".claude-plugin/marketplace.json",
		".agents/plugins/marketplace.json",
		"distribution/claude-plugin/.claude-plugin/plugin.json",
		"distribution/claude-plugin/.claude-plugin/marketplace.json",
		"distribution/codex-plugin/.codex-plugin/plugin.json",
	}

	for _, rel := range files {
		path := filepath.Join(repoRoot, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}

		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}

		switch rel {
		case ".claude-plugin/marketplace.json", ".agents/plugins/marketplace.json":
			validateLocalMarketplacePaths(t, repoRoot, rel, payload)
		}
	}
}

func TestMarketplacePluginRootsExist(t *testing.T) {
	repoRoot := currentRepoRoot(t)

	paths := []string{
		"distribution/claude-plugin/commands/plexium-setup.md",
		"distribution/claude-plugin/skills/plexium-workflows/SKILL.md",
		"distribution/codex-plugin/skills/install-plexium/SKILL.md",
		"distribution/codex-plugin/skills/setup-plexium/SKILL.md",
		"distribution/codex-plugin/skills/query-plexium-wiki/SKILL.md",
	}

	for _, rel := range paths {
		if _, err := os.Stat(filepath.Join(repoRoot, rel)); err != nil {
			t.Fatalf("expected %s to exist: %v", rel, err)
		}
	}
}

func validateLocalMarketplacePaths(t *testing.T, repoRoot, rel string, payload map[string]any) {
	t.Helper()

	plugins, ok := payload["plugins"].([]any)
	if !ok {
		t.Fatalf("%s: expected plugins array", rel)
	}

	for _, entry := range plugins {
		plugin, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("%s: expected plugin entry object", rel)
		}

		var path string
		switch source := plugin["source"].(type) {
		case string:
			path = source
		case map[string]any:
			if kind, _ := source["type"].(string); kind != "" && kind != "local" {
				continue
			}
			if kind, _ := source["source"].(string); kind != "" && kind != "local" {
				continue
			}
			path, _ = source["path"].(string)
		}

		if path == "" {
			continue
		}

		target := filepath.Join(repoRoot, path)
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("%s: local source path %q does not exist: %v", rel, path, err)
		}
	}
}
