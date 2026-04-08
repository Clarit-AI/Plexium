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
