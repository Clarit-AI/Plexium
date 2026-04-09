package main

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

func TestResolveSetupAPIKey_UsesInjectedReader(t *testing.T) {
	cmd := &cobra.Command{Use: "setup"}
	cmd.Flags().String("api-key-file", "", "")
	cmd.Flags().Bool("api-key-stdin", false, "")
	if err := cmd.Flags().Set("api-key-stdin", "true"); err != nil {
		t.Fatalf("set api-key-stdin flag: %v", err)
	}

	key, err := resolveSetupAPIKey(cmd, bytes.NewBufferString("sk-or-v1-test\n"))
	if err != nil {
		t.Fatalf("resolveSetupAPIKey returned error: %v", err)
	}
	if key != "sk-or-v1-test" {
		t.Fatalf("expected injected key, got %q", key)
	}
}
