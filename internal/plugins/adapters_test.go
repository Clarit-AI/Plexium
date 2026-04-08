package plugins

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestInstallAdapter_RejectsCustomOverrideOfBuiltInName(t *testing.T) {
	dir := t.TempDir()
	writeSchemaFixture(t, dir)

	pluginDir := t.TempDir()
	writePluginFixture(t, pluginDir, `{
  "name": "codex",
  "version": 1,
  "description": "custom codex",
  "instructionFile": "AGENTS.md"
}`, "#!/bin/sh\nexit 0\n")

	_, err := InstallAdapter(dir, "codex", pluginDir)
	if err == nil {
		t.Fatalf("expected override error")
	}
	if got := err.Error(); got == "" || !containsAll(got, "codex", "cannot be overridden via --path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadManifestFromFile_RejectsMissingRequiredFields(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	_, err := readManifestFromFile(manifestPath)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if got := err.Error(); got == "" || !containsAll(got, "invalid plugin manifest", "missing name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifest_RejectsInstructionFileTraversal(t *testing.T) {
	err := validateManifest(Manifest{
		Name:            "custom",
		InstructionFile: "../AGENTS.md",
	}, "")
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if got := err.Error(); got == "" || !containsAll(got, "instructionFile", "within the repository") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallAdapter_CleansUpFailedInstall(t *testing.T) {
	dir := t.TempDir()
	writeSchemaFixture(t, dir)

	pluginDir := t.TempDir()
	writePluginFixture(t, pluginDir, `{
  "name": "custom",
  "version": 1,
  "description": "custom plugin",
  "instructionFile": "CUSTOM.md"
}`, "#!/bin/sh\necho broken 1>&2\nexit 5\n")

	_, err := InstallAdapter(dir, "custom", pluginDir)
	if err == nil {
		t.Fatalf("expected install error")
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".plexium", "plugins", "custom")); !os.IsNotExist(statErr) {
		t.Fatalf("expected staged install to be cleaned up, got: %v", statErr)
	}
}

func TestInstallAdapter_PreservesExistingInstallOnFailure(t *testing.T) {
	dir := t.TempDir()
	writeSchemaFixture(t, dir)

	goodPluginDir := t.TempDir()
	writePluginFixture(t, goodPluginDir, `{
  "name": "custom",
  "version": 1,
  "description": "custom plugin",
  "instructionFile": "CUSTOM.md"
}`, "#!/bin/sh\nprintf 'first version' > CUSTOM.md\n")

	if _, err := InstallAdapter(dir, "custom", goodPluginDir); err != nil {
		t.Fatalf("unexpected initial install error: %v", err)
	}
	originalScript, err := os.ReadFile(filepath.Join(dir, ".plexium", "plugins", "custom", "plugin.sh"))
	if err != nil {
		t.Fatalf("read original script: %v", err)
	}

	badPluginDir := t.TempDir()
	writePluginFixture(t, badPluginDir, `{
  "name": "custom",
  "version": 1,
  "description": "custom plugin",
  "instructionFile": "CUSTOM.md"
}`, "#!/bin/sh\necho broken 1>&2\nexit 9\n")

	if _, err := InstallAdapter(dir, "custom", badPluginDir); err == nil {
		t.Fatalf("expected reinstall error")
	}
	currentScript, err := os.ReadFile(filepath.Join(dir, ".plexium", "plugins", "custom", "plugin.sh"))
	if err != nil {
		t.Fatalf("read preserved script: %v", err)
	}
	if string(currentScript) != string(originalScript) {
		t.Fatalf("expected previous install to remain active")
	}
}

func writePluginFixture(t *testing.T, dir, manifest, script string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write plugin script: %v", err)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
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
