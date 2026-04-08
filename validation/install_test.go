package validation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshInstall_BuiltInPluginsWork(t *testing.T) {
	repoRoot := currentRepoRoot(t)

	binDir := t.TempDir()
	binaryPath := filepath.Join(binDir, "plexium")

	build := exec.Command("go", "build", "-o", binaryPath, "./cmd/plexium")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v: %s", err, string(output))
	}

	repoDir := filepath.Join(t.TempDir(), "sample")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir sample repo: %v", err)
	}

	gitInit := exec.Command("git", "init")
	gitInit.Dir = repoDir
	if output, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, string(output))
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(binaryPath, args...)
		cmd.Dir = repoDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("plexium %s failed: %v: %s", strings.Join(args, " "), err, string(output))
		}
		return string(output)
	}

	run("init")
	listOutput := run("plugin", "list")
	if !strings.Contains(listOutput, "claude") || !strings.Contains(listOutput, "codex") {
		t.Fatalf("expected bundled plugins in list output, got: %s", listOutput)
	}

	run("plugin", "add", "claude")
	run("plugin", "add", "codex")

	if _, err := os.Stat(filepath.Join(repoDir, "CLAUDE.md")); err != nil {
		t.Fatalf("expected CLAUDE.md to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "AGENTS.md")); err != nil {
		t.Fatalf("expected AGENTS.md to exist: %v", err)
	}
}

func currentRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = wd
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve repo root: %v: %s", err, string(output))
	}

	return strings.TrimSpace(string(output))
}
