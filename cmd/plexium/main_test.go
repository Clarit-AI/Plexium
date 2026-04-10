package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSetupAPIKey_FromInjectedStdin(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("api-key-file", "", "")
	cmd.Flags().Bool("api-key-stdin", false, "")
	require.NoError(t, cmd.Flags().Set("api-key-stdin", "true"))
	cmd.SetIn(bytes.NewBufferString("sk-or-v1-test\n"))

	key, err := resolveSetupAPIKey(cmd, cmd.InOrStdin())
	require.NoError(t, err)
	assert.Equal(t, "sk-or-v1-test", key)
}

func TestResolveSetupBudget_UnsetReturnsNil(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Float64("daily-budget-usd", 0, "")

	budget := resolveSetupBudget(cmd)
	assert.Nil(t, budget)
}

func TestResolveSetupBudget_ReturnsConfiguredValue(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Float64("daily-budget-usd", 0, "")
	require.NoError(t, cmd.Flags().Set("daily-budget-usd", "4.25"))

	budget := resolveSetupBudget(cmd)
	require.NotNil(t, budget)
	assert.Equal(t, 4.25, *budget)
}

func TestApplySetupExecutionMode_UpdatesConfig(t *testing.T) {
	repoRoot := t.TempDir()
	_, err := setupAgent(repoRoot, "claude", setupAgentOptions{})
	require.NoError(t, err)
	cfg, err := config.LoadFromDir(repoRoot)
	require.NoError(t, err)
	cfg.AssistiveAgent.Providers = []config.ProviderConfig{{
		Name:              "openrouter",
		Type:              "openai-compatible",
		Enabled:           true,
		Endpoint:          "https://openrouter.ai/api/v1",
		Model:             "nvidia/nemotron-3-super-120b-a12b",
		CapabilityProfile: "balanced",
		APIKeyEnv:         "OPENROUTER_API_KEY",
	}}
	require.NoError(t, config.SaveToDir(repoRoot, cfg))

	require.NoError(t, applySetupExecutionMode(repoRoot, "provider-primary"))

	updated, err := config.LoadFromDir(repoRoot)
	require.NoError(t, err)
	assert.Equal(t, "provider-primary", updated.Daemon.ExecutionMode)
}

func TestApplySetupExecutionMode_RejectsProviderPrimaryWithoutProvider(t *testing.T) {
	repoRoot := t.TempDir()
	_, err := setupAgent(repoRoot, "claude", setupAgentOptions{})
	require.NoError(t, err)
	cfg, err := config.LoadFromDir(repoRoot)
	require.NoError(t, err)
	cfg.AssistiveAgent.Providers = nil
	require.NoError(t, config.SaveToDir(repoRoot, cfg))

	err = applySetupExecutionMode(repoRoot, "provider-primary")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires at least one enabled assistive provider")
	assert.FileExists(t, filepath.Join(repoRoot, ".plexium", "config.yml"))
}
