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
	if !bytes.Contains(stdout.Bytes(), []byte("Install git-memento now?")) {
		t.Fatalf("expected install prompt in stdout")
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
