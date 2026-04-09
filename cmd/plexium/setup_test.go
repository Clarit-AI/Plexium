package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/config"
)

func TestSetupAgent_WriteConfigClaude(t *testing.T) {
	repoRoot := t.TempDir()
	binDir := t.TempDir()

	stub := "#!/bin/sh\ncat > \"$PWD/.mcp.json\" <<'EOF'\n{\"mcpServers\":{\"plexium-wiki\":{\"command\":\"plexium\",\"args\":[\"pageindex\",\"serve\"]}}}\nEOF\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write claude stub: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := setupAgent(repoRoot, "claude", setupAgentOptions{WriteConfig: true})
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

	result, err := setupAgent(repoRoot, "codex", setupAgentOptions{WriteConfig: true})
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

func TestEnableMementoInConfig(t *testing.T) {
	repoRoot := t.TempDir()
	if _, err := setupAgent(repoRoot, "claude", setupAgentOptions{}); err != nil {
		t.Fatalf("setupAgent returned error: %v", err)
	}

	if err := enableMementoInConfig(repoRoot); err != nil {
		t.Fatalf("enableMementoInConfig returned error: %v", err)
	}

	cfg, err := config.LoadFromDir(repoRoot)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.Integrations.Memento {
		t.Fatalf("expected integrations.memento to be enabled")
	}
	if !cfg.Enforcement.MementoGate {
		t.Fatalf("expected enforcement.mementoGate to be enabled")
	}
}

func TestSetupAgent_WithMementoDeclined_DoesNotPersistConfigFlags(t *testing.T) {
	repoRoot := t.TempDir()
	binDir := t.TempDir()

	stub := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "curl"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write curl stub: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+"/usr/bin:/bin:/opt/homebrew/bin")

	result, err := setupAgent(repoRoot, "claude", setupAgentOptions{
		WithMemento: true,
		Stdin:       bytes.NewBufferString("n\n"),
		Stdout:      &bytes.Buffer{},
		Stderr:      &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("setupAgent returned error: %v", err)
	}

	foundWarning := false
	for _, step := range result.Steps {
		if step.Name == "memento" && step.Status == "warning" {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Fatalf("expected a warning memento step when installation is declined")
	}

	cfg, err := config.LoadFromDir(repoRoot)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Integrations.Memento {
		t.Fatalf("expected integrations.memento to remain disabled")
	}
	if cfg.Enforcement.MementoGate {
		t.Fatalf("expected enforcement.mementoGate to remain disabled")
	}
}

func TestSetupAgent_WithMementoClaude_ConfiguresShimAndProvider(t *testing.T) {
	repoRoot := t.TempDir()
	binDir := t.TempDir()

	initCmd := exec.Command("/usr/bin/git", "init")
	initCmd.Dir = repoRoot
	if output, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init temp repo: %v\n%s", err, output)
	}

	gitStub := `#!/bin/sh
if [ "$1" = "memento" ] && [ "$2" = "--version" ]; then
  exit 0
fi
if [ "$1" = "config" ] && [ "$2" = "--local" ] && [ "$3" = "--get-regexp" ]; then
  /usr/bin/git "$@"
  exit $?
fi
if [ "$1" = "config" ] && [ "$2" = "--local" ] && [ "$3" = "--get" ]; then
  /usr/bin/git "$@"
  exit $?
fi
if [ "$1" = "config" ] && [ "$2" = "--local" ]; then
  /usr/bin/git "$@"
  exit $?
fi
if [ "$1" = "memento" ] && [ "$2" = "init" ]; then
  provider="$3"
  if [ -z "$provider" ]; then
    provider="codex"
  fi
  /usr/bin/git config --local memento.provider "$provider"
  exit 0
fi
exit 1
`

	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(gitStub), 0o755); err != nil {
		t.Fatalf("write git stub: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+"/usr/bin:/bin:/opt/homebrew/bin")
	setupStdout := &bytes.Buffer{}

	result, err := setupAgent(repoRoot, "claude", setupAgentOptions{
		WithMemento: true,
		Stdin:       bytes.NewBufferString(""),
		Stdout:      setupStdout,
		Stderr:      &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("setupAgent returned error: %v", err)
	}

	foundPass := false
	for _, step := range result.Steps {
		if step.Name == "memento" && step.Status == "pass" {
			foundPass = true
			break
		}
	}
	if !foundPass {
		t.Fatalf("expected successful memento setup step")
	}

	cfg, err := config.LoadFromDir(repoRoot)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.Integrations.Memento || !cfg.Enforcement.MementoGate {
		t.Fatalf("expected memento integration and gate to be enabled")
	}

	cmd := exec.Command("/usr/bin/git", "config", "--local", "--get", "memento.provider")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("read memento.provider: %v\n%s", err, output)
	}
	if got := string(bytes.TrimSpace(output)); got != "claude" {
		t.Fatalf("expected provider claude, got %q", got)
	}

	cmd = exec.Command("/usr/bin/git", "config", "--local", "--get", "memento.claude.bin")
	cmd.Dir = repoRoot
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("read memento.claude.bin: %v\n%s", err, output)
	}
	expectedShim := filepath.Join(repoRoot, ".plexium", "bin", "claude-memento-bridge.js")
	if got := string(bytes.TrimSpace(output)); got != expectedShim {
		t.Fatalf("expected shim path %q, got %q", expectedShim, got)
	}

	if _, err := os.Stat(expectedShim); err != nil {
		t.Fatalf("expected shim file to exist: %v", err)
	}
}
