package memento

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEnsureCLI_ReturnsAvailableWhenGitMementoExists(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "git"), "#!/bin/sh\nif [ \"$1\" = \"memento\" ] && [ \"$2\" = \"--version\" ]; then\n  exit 0\nfi\nexit 1\n")
	t.Setenv("PATH", binDir)

	result, err := EnsureCLI(EnsureCLIOptions{
		Stdin:  bytes.NewBufferString("\n"),
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("EnsureCLI returned error: %v", err)
	}
	if !result.Available {
		t.Fatalf("expected git-memento to be available")
	}
	if result.Installed {
		t.Fatalf("did not expect installation to run")
	}
}

func TestEnsureCLI_DeclineInstallLeavesToolUnavailable(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "curl"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir)

	stdout := &bytes.Buffer{}
	result, err := EnsureCLI(EnsureCLIOptions{
		Stdin:  bytes.NewBufferString("n\n"),
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("EnsureCLI returned error: %v", err)
	}
	if result.Available {
		t.Fatalf("expected git-memento to remain unavailable")
	}
	if result.InstallCommand == "" {
		t.Fatalf("expected install command guidance")
	}
	if !bytes.Contains(stdout.Bytes(), []byte("Install git-memento now? [y/N]: ")) {
		t.Fatalf("expected install prompt in stdout")
	}
}

func TestEnsureCLI_EOFDoesNotCountAsConsent(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "curl"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir)

	result, err := EnsureCLI(EnsureCLIOptions{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("EnsureCLI returned error: %v", err)
	}
	if result.Available {
		t.Fatalf("expected EOF to leave git-memento unavailable")
	}
	if result.Installed {
		t.Fatalf("did not expect installation to run on EOF")
	}
}

func TestIsInitializedChecksLocalGitConfig(t *testing.T) {
	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test User")

	initialized, err := IsInitialized(repoRoot)
	if err != nil {
		t.Fatalf("IsInitialized returned error: %v", err)
	}
	if initialized {
		t.Fatalf("expected repo to start uninitialized")
	}

	runGit(t, repoRoot, "config", "--local", "memento.provider", "codex")
	initialized, err = IsInitialized(repoRoot)
	if err != nil {
		t.Fatalf("IsInitialized returned error after config: %v", err)
	}
	if !initialized {
		t.Fatalf("expected repo to be initialized after local config write")
	}
}

func TestConfiguredProviderReadsLocalGitConfig(t *testing.T) {
	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init")

	provider, err := ConfiguredProvider(repoRoot)
	if err != nil {
		t.Fatalf("ConfiguredProvider returned error: %v", err)
	}
	if provider != "" {
		t.Fatalf("expected empty provider before configuration, got %q", provider)
	}

	runGit(t, repoRoot, "config", "--local", "memento.provider", "claude")
	provider, err = ConfiguredProvider(repoRoot)
	if err != nil {
		t.Fatalf("ConfiguredProvider returned error after config: %v", err)
	}
	if provider != "claude" {
		t.Fatalf("expected provider claude, got %q", provider)
	}
}

func TestConfigureClaudeShimWritesLocalGitConfig(t *testing.T) {
	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init")

	if err := ConfigureClaudeShim(repoRoot); err != nil {
		t.Fatalf("ConfigureClaudeShim returned error: %v", err)
	}

	cmd := exec.Command("git", "config", "--local", "--get", "memento.claude.bin")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("read memento.claude.bin: %v\n%s", err, output)
	}
	got := string(bytes.TrimSpace(output))
	want := filepath.Join(repoRoot, ".plexium", "bin", "claude-memento-bridge.js")
	if got != want {
		t.Fatalf("expected shim path %q, got %q", want, got)
	}

	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read shim file: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected shim file to be written")
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func runGit(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
