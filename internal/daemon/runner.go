// Package daemon provides the agent-neutral dispatch interface for running
// LLM CLI tools (claude, codex, gemini) with a unified RunnerAdapter contract.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// RunnerAdapter is the agent-neutral dispatch interface. Implementations shell
// out to specific CLI tools (claude, codex, gemini) or return canned results
// (noop). Every implementation measures wall-clock latency and captures stdout.
type RunnerAdapter interface {
	Run(ctx context.Context, role string, prompt string, contextPages []string, workdir string) (*RunResult, error)
}

// RunResult holds the output and telemetry from a single runner invocation.
type RunResult struct {
	Output     string  `json:"output"`
	TokensUsed int     `json:"tokensUsed"`
	CostUSD    float64 `json:"costUSD"`
	LatencyMs  int64   `json:"latencyMs"`
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// buildPrompt assembles a full prompt string from a role, user prompt, and
// optional context pages. The format is:
//
//	Role: <role>
//
//	Context pages:
//	- page1
//	- page2
//
//	<prompt>
func buildPrompt(role, prompt string, contextPages []string) string {
	var b strings.Builder

	b.WriteString("Role: ")
	b.WriteString(role)
	b.WriteString("\n")

	if len(contextPages) > 0 {
		b.WriteString("\nContext pages:\n")
		for _, p := range contextPages {
			b.WriteString("- ")
			b.WriteString(p)
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(prompt)

	return b.String()
}

// ---------------------------------------------------------------------------
// ClaudeRunner
// ---------------------------------------------------------------------------

// TokensUsed and CostUSD are -1 to indicate that the underlying CLI tools
// (claude/codex/gemini) do not expose structured per-call token or cost
// telemetry in their stdout output. A follow-up issue should track adding
// structured output parsing or --json flag support to these runners.
const tokensUnknown = -1
const costUnknown = -1

func newRunResult(output string, started time.Time) *RunResult {
	return &RunResult{
		Output:     strings.TrimSpace(output),
		TokensUsed: tokensUnknown,
		CostUSD:    costUnknown,
		LatencyMs:  time.Since(started).Milliseconds(),
	}
}

// ClaudeRunner shells out to the `claude` CLI.
type ClaudeRunner struct {
	modelFlag string
}

// NewClaudeRunner creates a ClaudeRunner. If model is empty the CLI default is
// used (no --model flag).
func NewClaudeRunner(model string) *ClaudeRunner {
	return &ClaudeRunner{modelFlag: model}
}

// Run executes `claude --print [--model <model>] <prompt>` and returns the
// captured stdout along with wall-clock latency. TokensUsed and CostUSD are
// -1 (unknown) as the CLI does not emit structured usage data.
func (r *ClaudeRunner) Run(ctx context.Context, role, prompt string, contextPages []string, workdir string) (*RunResult, error) {
	start := time.Now()
	fullPrompt := buildPrompt(role, prompt, contextPages)

	args := []string{
		"--print",
		"--permission-mode", "acceptEdits",
		"--allow-dangerously-skip-permissions",
	}
	if r.modelFlag != "" {
		args = append(args, "--model", r.modelFlag)
	}
	args = append(args, fullPrompt)

	cmd := exec.CommandContext(ctx, "claude", args...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	out, err := cmd.Output()

	return &RunResult{
		Output:     string(out),
		TokensUsed: tokensUnknown,
		CostUSD:    costUnknown,
		LatencyMs:  time.Since(start).Milliseconds(),
	}, err
}

// ---------------------------------------------------------------------------
// CodexRunner
// ---------------------------------------------------------------------------

// CodexRunner shells out to the `codex` CLI.
type CodexRunner struct {
	modelFlag string
}

// NewCodexRunner creates a CodexRunner. If model is empty the CLI default is
// used.
func NewCodexRunner(model string) *CodexRunner {
	return &CodexRunner{modelFlag: model}
}

// Run executes `codex exec --full-auto [--model <model>] --output-last-message <file> <prompt>`.
// TokensUsed and CostUSD are -1 (unknown).
func (r *CodexRunner) Run(ctx context.Context, role, prompt string, contextPages []string, workdir string) (*RunResult, error) {
	start := time.Now()
	fullPrompt := buildPrompt(role, prompt, contextPages)

	outputFile, err := os.CreateTemp("", "plexium-codex-output-*.txt")
	if err != nil {
		return nil, fmt.Errorf("creating Codex output file: %w", err)
	}
	outputPath := outputFile.Name()
	defer os.Remove(outputPath)
	if err := outputFile.Close(); err != nil {
		return nil, fmt.Errorf("closing Codex output file: %w", err)
	}

	args := []string{"exec", "--full-auto", "--output-last-message", outputPath}
	if r.modelFlag != "" {
		args = append(args, "--model", r.modelFlag)
	}
	args = append(args, fullPrompt)

	cmd := exec.CommandContext(ctx, "codex", args...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		result := newRunResult(string(out), start)
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return result, fmt.Errorf("codex CLI not found in PATH: %w", err)
		}
		if result.Output != "" {
			return result, fmt.Errorf("codex exec failed: %w: %s", err, result.Output)
		}
		return result, fmt.Errorf("codex exec failed: %w", err)
	}

	finalOutput, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		return newRunResult(string(out), start), fmt.Errorf("reading Codex final output: %w", readErr)
	}

	output := strings.TrimSpace(string(finalOutput))
	if output == "" {
		log.Printf("warning: empty codex output file, falling back to combined stdout/stderr: %s", outputPath)
		output = strings.TrimSpace(string(out))
	}

	return newRunResult(output, start), nil
}

// ---------------------------------------------------------------------------
// GeminiRunner
// ---------------------------------------------------------------------------

// GeminiRunner shells out to the `gemini` CLI.
type GeminiRunner struct {
	modelFlag string
}

// NewGeminiRunner creates a GeminiRunner. If model is empty the CLI default is
// used.
func NewGeminiRunner(model string) *GeminiRunner {
	return &GeminiRunner{modelFlag: model}
}

// Run executes `gemini [--model <model>] <prompt>`.
// TokensUsed and CostUSD are -1 (unknown).
func (r *GeminiRunner) Run(ctx context.Context, role, prompt string, contextPages []string, workdir string) (*RunResult, error) {
	start := time.Now()
	fullPrompt := buildPrompt(role, prompt, contextPages)

	args := []string{"--approval-mode", "auto_edit", "--yolo"}
	if r.modelFlag != "" {
		args = append(args, "--model", r.modelFlag)
	}
	args = append(args, "--prompt", fullPrompt)

	cmd := exec.CommandContext(ctx, "gemini", args...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	out, err := cmd.Output()

	return &RunResult{
		Output:     string(out),
		TokensUsed: tokensUnknown,
		CostUSD:    costUnknown,
		LatencyMs:  time.Since(start).Milliseconds(),
	}, err
}

// ---------------------------------------------------------------------------
// NoOpRunner
// ---------------------------------------------------------------------------

// NoOpRunner returns an empty RunResult without executing anything. Useful for
// testing and dry-run modes.
type NoOpRunner struct{}

// NewNoOpRunner creates a NoOpRunner.
func NewNoOpRunner() *NoOpRunner {
	return &NoOpRunner{}
}

// Run returns an empty result immediately.
func (r *NoOpRunner) Run(_ context.Context, _, _ string, _ []string, _ string) (*RunResult, error) {
	return &RunResult{}, nil
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// NewRunner returns a RunnerAdapter for the given runner type. Recognised types
// are "claude", "codex", "gemini", and "noop" (or empty string). An unknown
// type returns an error.
func NewRunner(runnerType, model string) (RunnerAdapter, error) {
	switch strings.ToLower(runnerType) {
	case "claude":
		return NewClaudeRunner(model), nil
	case "codex":
		return NewCodexRunner(model), nil
	case "gemini":
		return NewGeminiRunner(model), nil
	case "noop", "":
		return NewNoOpRunner(), nil
	default:
		return nil, fmt.Errorf("unknown runner type: %q", runnerType)
	}
}
