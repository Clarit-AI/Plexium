package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetupAgent_WriteConfigClaude(t *testing.T) {
	repoRoot := t.TempDir()
	binDir := t.TempDir()

	stub := "#!/bin/sh\ncat > \"$PWD/.mcp.json\" <<'EOF'\n{\"mcpServers\":{\"plexium-wiki\":{\"command\":\"plexium\",\"args\":[\"pageindex\",\"serve\"]}}}\nEOF\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write claude stub: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := setupAgent(repoRoot, "claude", true)
	if err != nil {
		t.Fatalf("setupAgent returned error: %v", err)
	}
	if !result.Verify.Configured {
		t.Fatalf("expected Claude setup to be configured")
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".mcp.json")); err != nil {
		t.Fatalf("expected .mcp.json to exist: %v", err)
	}
}

func TestSetupAgent_WriteConfigCodex(t *testing.T) {
	repoRoot := t.TempDir()
	binDir := t.TempDir()
	homeDir := t.TempDir()

	stub := "#!/bin/sh\nmkdir -p \"$HOME/.codex\"\ncat > \"$HOME/.codex/config.toml\" <<'EOF'\n[mcp_servers.plexium-wiki]\ncommand = \"plexium\"\nargs = [\"pageindex\", \"serve\"]\nEOF\n"
	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write codex stub: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", homeDir)

	result, err := setupAgent(repoRoot, "codex", true)
	if err != nil {
		t.Fatalf("setupAgent returned error: %v", err)
	}
	if !result.Verify.Configured {
		t.Fatalf("expected Codex setup to be configured")
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".codex", "config.toml")); err != nil {
		t.Fatalf("expected config.toml to exist: %v", err)
	}
}
