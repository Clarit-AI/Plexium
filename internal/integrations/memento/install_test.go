package memento

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnsureCLI_ReturnsAvailableWhenGitMementoExists(t *testing.T) {
	skipOnWindows(t)

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
	skipOnWindows(t)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", "/usr/bin:/bin")

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
	if result.ReleaseURL == "" {
		t.Fatalf("expected release guidance")
	}
	if !bytes.Contains(stdout.Bytes(), []byte("Install git-memento now? [y/N]: ")) {
		t.Fatalf("expected install prompt in stdout")
	}
}

func TestEnsureCLI_EOFDoesNotCountAsConsent(t *testing.T) {
	skipOnWindows(t)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", "/usr/bin:/bin")

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

func TestEnsureCLI_DetectsExistingLocalInstallOutsidePath(t *testing.T) {
	skipOnWindows(t)

	homeDir := t.TempDir()
	localBin := filepath.Join(homeDir, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatalf("create local bin: %v", err)
	}
	writeExecutable(t, filepath.Join(localBin, "git-memento"), "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  exit 0\nfi\nexit 1\n")

	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", "/usr/bin:/bin")

	result, err := EnsureCLI(EnsureCLIOptions{
		Stdin:  bytes.NewBufferString(""),
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("EnsureCLI returned error: %v", err)
	}
	if !result.Available {
		t.Fatalf("expected git-memento to be detected from ~/.local/bin")
	}
	if !strings.Contains(result.Message, "added to PATH") {
		t.Fatalf("expected message to mention PATH update, got %q", result.Message)
	}
	if !strings.Contains(os.Getenv("PATH"), localBin) {
		t.Fatalf("expected PATH to include local bin after detection")
	}
}

func TestPromptForInstall_ProcessesEOFAnswer(t *testing.T) {
	confirmed, err := promptForInstall(bytes.NewBufferString("yes"), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("promptForInstall returned error: %v", err)
	}
	if !confirmed {
		t.Fatalf("expected explicit yes without newline to be accepted")
	}
}

func TestIsInitializedChecksLocalGitConfig(t *testing.T) {
	skipOnWindows(t)

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
	skipOnWindows(t)

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
	skipOnWindows(t)

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
	want := filepath.Join(repoRoot, ".plexium", "bin", "claude-memento-bridge.cjs")
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

func TestConfigureCodexShimWritesLocalGitConfig(t *testing.T) {
	skipOnWindows(t)

	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init")

	if err := ConfigureCodexShim(repoRoot); err != nil {
		t.Fatalf("ConfigureCodexShim returned error: %v", err)
	}

	cmd := exec.Command("git", "config", "--local", "--get", "memento.codex.bin")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("read memento.codex.bin: %v\n%s", err, output)
	}
	got := string(bytes.TrimSpace(output))
	want := filepath.Join(repoRoot, ".plexium", "bin", "codex-memento-bridge.cjs")
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

func TestCodexBridgeHandlesSessionsListAndGet(t *testing.T) {
	skipOnWindows(t)
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to exercise the Codex bridge")
	}

	homeDir := t.TempDir()
	codexHome := filepath.Join(homeDir, ".codex")
	sessionID := "019d6a76-f56e-75c0-8623-962656784a7c"
	indexPath := filepath.Join(codexHome, "session_index.jsonl")
	sessionDir := filepath.Join(codexHome, "sessions", "2026", "04", "08")
	sessionPath := filepath.Join(sessionDir, "rollout-2026-04-08T00-21-14-"+sessionID+".jsonl")

	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("create fake codex session dir: %v", err)
	}
	indexLine := `{"id":"` + sessionID + `","thread_name":"Audit dual-service app","updated_at":"2026-04-08T00:21:29.193203Z"}` + "\n"
	if err := os.WriteFile(indexPath, []byte(indexLine), 0o644); err != nil {
		t.Fatalf("write session index: %v", err)
	}
	sessionLog := strings.Join([]string{
		`{"timestamp":"2026-04-08T00:21:25.399Z","type":"session_meta","payload":{"id":"` + sessionID + `","timestamp":"2026-04-08T00:21:14.254Z","cwd":"/tmp/project","originator":"Codex Desktop"}}`,
		`{"timestamp":"2026-04-08T00:21:30.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"` + strings.Repeat("Document the repo. ", 5000) + `"}]}}`,
		`{"timestamp":"2026-04-08T00:21:40.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I will build the wiki in passes."}]}}`,
	}, "\n")
	if err := os.WriteFile(sessionPath, []byte(sessionLog), 0o644); err != nil {
		t.Fatalf("write fake session log: %v", err)
	}

	bridgePath := filepath.Join(t.TempDir(), "codex-memento-bridge.cjs")
	if err := os.WriteFile(bridgePath, []byte(codexBridgeScript), 0o755); err != nil {
		t.Fatalf("write bridge: %v", err)
	}

	listCmd := exec.Command(nodePath, bridgePath, "sessions", "list", "--json")
	listCmd.Env = append(os.Environ(), "HOME="+homeDir, "CODEX_HOME="+codexHome)
	listOutput, err := listCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bridge sessions list failed: %v\n%s", err, listOutput)
	}

	var sessions []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(listOutput, &sessions); err != nil {
		t.Fatalf("parse sessions list JSON: %v\n%s", err, listOutput)
	}
	if len(sessions) != 1 || sessions[0].ID != sessionID || sessions[0].Title != "Audit dual-service app" {
		t.Fatalf("unexpected sessions list payload: %s", listOutput)
	}

	getCmd := exec.Command(nodePath, bridgePath, "sessions", "get", sessionID, "--json")
	getCmd.Env = append(os.Environ(), "HOME="+homeDir, "CODEX_HOME="+codexHome)
	getOutput, err := getCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bridge sessions get failed: %v\n%s", err, getOutput)
	}

	var session struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Messages []struct {
			Role string `json:"role"`
			Text string `json:"text"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(getOutput, &session); err != nil {
		t.Fatalf("parse sessions get JSON: %v\n%s", err, getOutput)
	}
	if session.ID != sessionID || session.Title != "Audit dual-service app" {
		t.Fatalf("unexpected session payload: %s", getOutput)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", len(session.Messages), getOutput)
	}
	if session.Messages[0].Role != "user" || !strings.HasPrefix(session.Messages[0].Text, "Document the repo.") || len(session.Messages[0].Text) < 1000 {
		t.Fatalf("unexpected first message: %+v", session.Messages[0])
	}
	if session.Messages[1].Role != "assistant" || session.Messages[1].Text != "I will build the wiki in passes." {
		t.Fatalf("unexpected second message: %+v", session.Messages[1])
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

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell stubs are not supported on Windows")
	}
}
