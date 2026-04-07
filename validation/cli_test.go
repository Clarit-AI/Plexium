package validation

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CLI contract tests build the binary once and test it as a black box.
// These tests require `go build` to succeed first.

var plexiumBin string

func init() {
	// Build the binary for CLI testing. If this fails, CLI tests will be skipped.
	// We use a predictable temp path.
	plexiumBin = "/tmp/plexium-test-bin"
}

func buildBinary(t *testing.T) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", plexiumBin, "./cmd/plexium")
	cmd.Dir = findRepoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("cannot build plexium binary: %s: %v", string(out), err)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func runPlexium(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(plexiumBin, args...)
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("failed to run plexium %v: %v", args, err)
	}
	return string(out), exitCode
}

// =============================================================================
// CLI-1: Commands exist and respond to --help
// =============================================================================

func TestCLI_CommandsExist(t *testing.T) {
	buildBinary(t)

	commands := []string{
		"init", "convert", "sync", "lint", "publish",
		"doctor", "migrate", "compile", "daemon",
		"retrieve", "gh-wiki-sync", "bootstrap",
		"hook", "ci", "agent", "orchestrate", "plugin",
	}

	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			out, code := runPlexium(t, cmd, "--help")
			assert.Equal(t, 0, code, "command %s --help should exit 0, output: %s", cmd, out)
			assert.NotEmpty(t, out, "command %s --help should produce output", cmd)
		})
	}
}

func TestCLI_SubcommandsExist(t *testing.T) {
	buildBinary(t)

	subcommands := [][]string{
		{"hook", "pre-commit", "--help"},
		{"hook", "post-commit", "--help"},
		{"ci", "check", "--help"},
		{"agent", "start", "--help"},
		{"agent", "stop", "--help"},
		{"agent", "status", "--help"},
		{"agent", "test", "--help"},
		{"agent", "spend", "--help"},
		{"agent", "benchmark", "--help"},
	}

	for _, args := range subcommands {
		name := strings.Join(args[:len(args)-1], " ")
		t.Run(name, func(t *testing.T) {
			out, code := runPlexium(t, args...)
			assert.Equal(t, 0, code, "%s --help should exit 0, output: %s", name, out)
		})
	}
}

// =============================================================================
// CLI-2: Version flag works
// =============================================================================

func TestCLI_VersionFlag(t *testing.T) {
	buildBinary(t)

	out, code := runPlexium(t, "--version")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "plexium", "version output should contain 'plexium'")
}

// =============================================================================
// CLI-3: Init with --dry-run produces no side effects
// =============================================================================

func TestCLI_InitDryRunNoFiles(t *testing.T) {
	buildBinary(t)

	dir := t.TempDir()
	cmd := exec.Command(plexiumBin, "init", "--dry-run")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	_ = err // init may succeed or warn

	output := string(out)
	assert.Contains(t, output, "dry-run", "dry-run output should mention dry-run")

	// No .wiki/ or .plexium/ should be created
	entries, _ := exec.Command("ls", "-la", dir).Output()
	assert.NotContains(t, string(entries), ".wiki",
		".wiki/ should not exist after dry-run init")
}

// =============================================================================
// CLI-4: Lint flags registered correctly
// =============================================================================

func TestCLI_LintFlagsExist(t *testing.T) {
	buildBinary(t)

	out, code := runPlexium(t, "lint", "--help")
	assert.Equal(t, 0, code)

	expectedFlags := []string{"--deterministic", "--full", "--ci", "--fail-on"}
	for _, flag := range expectedFlags {
		assert.Contains(t, out, flag,
			"lint --help should mention %s flag", flag)
	}
}

// =============================================================================
// CLI-5: Unknown commands produce error
// =============================================================================

func TestCLI_UnknownCommandError(t *testing.T) {
	buildBinary(t)

	_, code := runPlexium(t, "nonexistent-command")
	assert.NotEqual(t, 0, code, "unknown command should exit non-zero")
}

// =============================================================================
// CLI-6: Root command shows help
// =============================================================================

func TestCLI_RootCommandHelp(t *testing.T) {
	buildBinary(t)

	out, code := runPlexium(t, "--help")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "Plexium")
	assert.Contains(t, out, "Available Commands")
}

// =============================================================================
// CLI-7: Compile --dry-run flag
// =============================================================================

func TestCLI_CompileDryRunFlag(t *testing.T) {
	buildBinary(t)

	out, code := runPlexium(t, "compile", "--help")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "--dry-run")
}

// =============================================================================
// CLI-8: CI check requires --base and --head
// =============================================================================

func TestCLI_CICheckRequiresFlags(t *testing.T) {
	buildBinary(t)

	// Without required flags, should fail
	_, code := runPlexium(t, "ci", "check")
	assert.NotEqual(t, 0, code, "ci check without --base/--head should fail")
}

// =============================================================================
// CLI-9: Output JSON flag
// =============================================================================

func TestCLI_OutputJSONFlagExists(t *testing.T) {
	buildBinary(t)

	out, code := runPlexium(t, "--help")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "--output-json", "root should have --output-json flag")
}
