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
