package prompts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureRepoPack(t *testing.T) {
	repoRoot := t.TempDir()

	created, err := EnsureRepoPack(repoRoot)
	require.NoError(t, err)
	require.NotEmpty(t, created)

	assert.FileExists(t, filepath.Join(repoRoot, ".plexium", "prompts", "assistive", "initial-wiki-population.md"))
	assert.FileExists(t, filepath.Join(repoRoot, ".plexium", "prompts", "profiles", "balanced.md"))
}

func TestRender_UsesRepoOverride(t *testing.T) {
	repoRoot := t.TempDir()
	_, err := EnsureRepoPack(repoRoot)
	require.NoError(t, err)

	overridePath := filepath.Join(repoRoot, ".plexium", "prompts", "assistive", "staleness.md")
	require.NoError(t, os.WriteFile(overridePath, []byte("---\n---\nCustom {{ .PageTitle }}"), 0o644))

	rendered, err := Render(repoRoot, PromptStaleness, ProfileBalanced, map[string]string{"PageTitle": "Auth"})
	require.NoError(t, err)
	assert.Contains(t, rendered, "Custom Auth")
}

func TestProfileFromConfig(t *testing.T) {
	cfg := &config.Config{
		AssistiveAgent: config.AssistiveAgent{
			Providers: []config.ProviderConfig{
				{Name: "local-ollama", Enabled: true, Type: "ollama"},
			},
		},
	}
	assert.Equal(t, ProfileConstrainedLocal, ProfileFromConfig(cfg))

	cfg.AssistiveAgent.Providers[0].CapabilityProfile = ProfileFrontierLargeContext
	assert.Equal(t, ProfileFrontierLargeContext, ProfileFromConfig(cfg))
}
