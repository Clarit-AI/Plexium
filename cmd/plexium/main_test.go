package main

import (
	"bytes"
	"testing"

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
