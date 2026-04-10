package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// buildPrompt
// ---------------------------------------------------------------------------

func TestBuildPrompt_WithContextPages(t *testing.T) {
	got := buildPrompt("coder", "fix the bug", []string{"modules/auth.md", "decisions/adr-001.md"})
	expected := "Role: coder\n\nContext pages:\n- modules/auth.md\n- decisions/adr-001.md\n\nfix the bug"
	assert.Equal(t, expected, got)
}

func TestBuildPrompt_NoContextPages(t *testing.T) {
	got := buildPrompt("retriever", "find references", nil)
	expected := "Role: retriever\n\nfind references"
	assert.Equal(t, expected, got)
}

func TestBuildPrompt_EmptyContextPages(t *testing.T) {
	got := buildPrompt("documenter", "update docs", []string{})
	expected := "Role: documenter\n\nupdate docs"
	assert.Equal(t, expected, got)
}

func TestBuildPrompt_EmptyRole(t *testing.T) {
	got := buildPrompt("", "hello", nil)
	expected := "Role: \n\nhello"
	assert.Equal(t, expected, got)
}

// ---------------------------------------------------------------------------
// NoOpRunner
// ---------------------------------------------------------------------------

func TestNoOpRunner_ReturnsEmptyResult(t *testing.T) {
	runner := NewNoOpRunner()
	result, err := runner.Run(context.Background(), "coder", "do something", []string{"page.md"}, "")

	require.NoError(t, err)
	assert.Equal(t, "", result.Output)
	assert.Equal(t, 0, result.TokensUsed)
	assert.Equal(t, float64(0), result.CostUSD)
	assert.Equal(t, int64(0), result.LatencyMs)
}

func TestNoOpRunner_ImplementsRunnerAdapter(t *testing.T) {
	var _ RunnerAdapter = (*NoOpRunner)(nil)
}

// ---------------------------------------------------------------------------
// Constructor / struct tests
// ---------------------------------------------------------------------------

func TestNewClaudeRunner(t *testing.T) {
	r := NewClaudeRunner("opus-4")
	assert.Equal(t, "opus-4", r.modelFlag)
}

func TestNewClaudeRunner_EmptyModel(t *testing.T) {
	r := NewClaudeRunner("")
	assert.Equal(t, "", r.modelFlag)
}

func TestNewCodexRunner(t *testing.T) {
	r := NewCodexRunner("o3")
	assert.Equal(t, "o3", r.modelFlag)
}

func TestNewCodexRunner_EmptyModel(t *testing.T) {
	r := NewCodexRunner("")
	assert.Equal(t, "", r.modelFlag)
}

func TestNewGeminiRunner(t *testing.T) {
	r := NewGeminiRunner("gemini-2.5-pro")
	assert.Equal(t, "gemini-2.5-pro", r.modelFlag)
}

func TestNewGeminiRunner_EmptyModel(t *testing.T) {
	r := NewGeminiRunner("")
	assert.Equal(t, "", r.modelFlag)
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestClaudeRunner_ImplementsRunnerAdapter(t *testing.T) {
	var _ RunnerAdapter = (*ClaudeRunner)(nil)
}

func TestCodexRunner_ImplementsRunnerAdapter(t *testing.T) {
	var _ RunnerAdapter = (*CodexRunner)(nil)
}

func TestCodexRunner_RunUsesExecAndOutputFile(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex-args.txt")

	script := `#!/bin/sh
printf '%s\n' "$@" > "$CODEX_TEST_LOG"
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--output-last-message" ]; then
    out="$arg"
  fi
  prev="$arg"
done
printf 'codex final output' > "$out"
`
	scriptPath := filepath.Join(binDir, "codex")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEX_TEST_LOG", logPath)

	runner := NewCodexRunner("o3")
	result, err := runner.Run(context.Background(), "coder", "fix bug", []string{"modules/auth.md"}, "")
	require.NoError(t, err)
	assert.Equal(t, "codex final output", result.Output)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	got := string(data)
	assert.Contains(t, got, "exec")
	assert.Contains(t, got, "--full-auto")
	assert.Contains(t, got, "--output-last-message")
	assert.Contains(t, got, "--model")
	assert.Contains(t, got, "o3")
}

func TestCodexRunner_RunSetsWorkingDirectory(t *testing.T) {
	binDir := t.TempDir()
	workdir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex-pwd.txt")

	script := `#!/bin/sh
pwd > "$CODEX_TEST_PWD"
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--output-last-message" ]; then
    out="$arg"
  fi
  prev="$arg"
done
printf 'codex final output' > "$out"
`
	scriptPath := filepath.Join(binDir, "codex")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEX_TEST_PWD", logPath)

	runner := NewCodexRunner("o3")
	_, err := runner.Run(context.Background(), "coder", "fix bug", []string{"modules/auth.md"}, workdir)
	require.NoError(t, err)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Equal(t, workdir, strings.TrimSpace(string(data)))
}

func TestCodexRunner_RunReturnsResultWhenCLIIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	runner := NewCodexRunner("o3")
	result, err := runner.Run(context.Background(), "coder", "fix bug", nil, "")
	require.Error(t, err)
	require.NotNil(t, result)
	assert.Contains(t, err.Error(), "codex CLI not found")
	assert.Equal(t, "", result.Output)
	assert.GreaterOrEqual(t, result.LatencyMs, int64(0))
}

func TestCodexRunner_RunReturnsPartialOutputOnExecFailure(t *testing.T) {
	binDir := t.TempDir()
	script := `#!/bin/sh
echo "partial codex output"
echo "stderr detail" 1>&2
exit 7
`
	scriptPath := filepath.Join(binDir, "codex")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	runner := NewCodexRunner("")
	result, err := runner.Run(context.Background(), "coder", "fix bug", nil, "")
	require.Error(t, err)
	require.NotNil(t, result)
	assert.Contains(t, err.Error(), "codex exec failed")
	assert.Contains(t, err.Error(), "partial codex output")
	assert.Contains(t, result.Output, "partial codex output")
	assert.Contains(t, result.Output, "stderr detail")
}

func TestCodexRunner_RunReturnsPartialOutputWhenFinalReadFails(t *testing.T) {
	binDir := t.TempDir()
	script := `#!/bin/sh
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--output-last-message" ]; then
    out="$arg"
  fi
  prev="$arg"
done
echo "combined output fallback"
rm -f "$out"
`
	scriptPath := filepath.Join(binDir, "codex")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	runner := NewCodexRunner("")
	result, err := runner.Run(context.Background(), "coder", "fix bug", nil, "")
	require.Error(t, err)
	require.NotNil(t, result)
	assert.Contains(t, err.Error(), "reading Codex final output")
	assert.Contains(t, result.Output, "combined output fallback")
}

func TestGeminiRunner_ImplementsRunnerAdapter(t *testing.T) {
	var _ RunnerAdapter = (*GeminiRunner)(nil)
}

// ---------------------------------------------------------------------------
// Factory: NewRunner
// ---------------------------------------------------------------------------

func TestNewRunner_Claude(t *testing.T) {
	r, err := NewRunner("claude", "opus-4")
	require.NoError(t, err)
	assert.IsType(t, &ClaudeRunner{}, r)
}

func TestNewRunner_Codex(t *testing.T) {
	r, err := NewRunner("codex", "o3")
	require.NoError(t, err)
	assert.IsType(t, &CodexRunner{}, r)
}

func TestNewRunner_Gemini(t *testing.T) {
	r, err := NewRunner("gemini", "gemini-2.5-pro")
	require.NoError(t, err)
	assert.IsType(t, &GeminiRunner{}, r)
}

func TestNewRunner_Noop(t *testing.T) {
	r, err := NewRunner("noop", "")
	require.NoError(t, err)
	assert.IsType(t, &NoOpRunner{}, r)
}

func TestNewRunner_EmptyStringIsNoop(t *testing.T) {
	r, err := NewRunner("", "")
	require.NoError(t, err)
	assert.IsType(t, &NoOpRunner{}, r)
}

func TestNewRunner_CaseInsensitive(t *testing.T) {
	r, err := NewRunner("CLAUDE", "opus-4")
	require.NoError(t, err)
	assert.IsType(t, &ClaudeRunner{}, r)
}

func TestNewRunner_UnknownType(t *testing.T) {
	r, err := NewRunner("gpt", "gpt-4")
	assert.Nil(t, r)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown runner type")
}
