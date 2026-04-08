package plugins

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestListAdapters_IncludesBundledBuiltins(t *testing.T) {
	dir := t.TempDir()

	adapters, err := ListAdapters(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(adapters) < 4 {
		t.Fatalf("expected bundled adapters, got %d", len(adapters))
	}

	names := map[string]bool{}
	for _, adapter := range adapters {
		names[adapter.Name] = true
		if adapter.Name == "claude" && !adapter.BuiltIn {
			t.Fatalf("expected claude adapter to be built-in")
		}
	}

	for _, expected := range []string{"claude", "codex", "cursor", "gemini"} {
		if !names[expected] {
			t.Fatalf("expected adapter %q to be listed", expected)
		}
	}
}

func TestInstallAdapter_BuiltInGeneratesInstructionFile(t *testing.T) {
	dir := t.TempDir()
	writeSchemaFixture(t, dir)

	result, err := InstallAdapter(dir, "codex", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.BuiltIn {
		t.Fatalf("expected built-in install result")
	}

	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatalf("expected AGENTS.md to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".plexium", "plugins", "codex", "plugin.sh")); err != nil {
		t.Fatalf("expected installed plugin script to exist: %v", err)
	}
}

func TestRunAdapter_UsesBundledFallback(t *testing.T) {
	dir := t.TempDir()
	writeSchemaFixture(t, dir)

	if err := RunAdapter(dir, "claude"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatalf("expected CLAUDE.md to exist: %v", err)
	}
}

func TestInstalledScript_UsesRepoRootWithoutPLEXIUMDIR(t *testing.T) {
	dir := t.TempDir()
	writeSchemaFixture(t, dir)

	if _, err := InstallAdapter(dir, "claude", ""); err != nil {
		t.Fatalf("unexpected install error: %v", err)
	}

	scriptPath := filepath.Join(dir, ".plexium", "plugins", "claude", "plugin.sh")
	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("manual script run failed: %v: %s", err, string(output))
	}

	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatalf("expected CLAUDE.md to exist after manual run: %v", err)
	}
}

func writeSchemaFixture(t *testing.T, dir string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(dir, ".wiki"), 0o755); err != nil {
		t.Fatalf("mkdir .wiki: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".plexium"), 0o755); err != nil {
		t.Fatalf("mkdir .plexium: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".wiki", "_schema.md"), []byte("# schema\n"), 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
}
