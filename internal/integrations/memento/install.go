package memento

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	installScriptURL = "https://raw.githubusercontent.com/mandel-macaque/memento/main/install.sh"
	projectURL       = "https://github.com/mandel-macaque/memento"
)

// EnsureCLIOptions controls interactive git-memento installation.
type EnsureCLIOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// EnsureCLIResult reports whether git-memento is ready for use.
type EnsureCLIResult struct {
	Available      bool
	Installed      bool
	InstallCommand string
	ProjectURL     string
	Message        string
}

// EnsureCLI checks whether git-memento is available and optionally offers to install it.
func EnsureCLI(opts EnsureCLIOptions) (*EnsureCLIResult, error) {
	result := &EnsureCLIResult{ProjectURL: projectURL}
	if cliAvailable() {
		result.Available = true
		result.Message = "git-memento is already installed"
		return result, nil
	}

	installCommand, ok := detectInstallCommand()
	result.InstallCommand = installCommand
	if !ok {
		result.Message = "git-memento is not installed and Plexium could not find a supported installer"
		return result, nil
	}

	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}

	fmt.Fprintln(stdout, "git-memento is not installed.")
	fmt.Fprintln(stdout, "Plexium can install it using the official installer:")
	fmt.Fprintf(stdout, "  %s\n\n", installCommand)

	confirmed, err := promptForInstall(opts.Stdin, stdout)
	if err != nil {
		return nil, err
	}
	if !confirmed {
		result.Message = "git-memento installation skipped"
		return result, nil
	}

	if err := runInstallCommand(installCommand, opts.Stdout, opts.Stderr); err != nil {
		result.Message = "git-memento installation failed"
		return result, err
	}

	addInstalledPathToEnv()
	if !cliAvailable() {
		result.Message = "git-memento installer finished, but the command is still unavailable on PATH"
		return result, fmt.Errorf("git-memento is still unavailable after installation")
	}

	result.Available = true
	result.Installed = true
	result.Message = "git-memento installed successfully"
	return result, nil
}

// IsInitialized reports whether the repository already has local git-memento configuration.
func IsInitialized(repoRoot string) (bool, error) {
	cmd := exec.Command("git", "config", "--local", "--get-regexp", "^memento\\.")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(output)) != "", nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("check git-memento repo config: %w", err)
}

// InitRepo initializes git-memento for the repository, optionally pinning a provider.
func InitRepo(repoRoot, provider string) error {
	args := []string{"memento", "init"}
	if provider != "" {
		args = append(args, provider)
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text != "" {
			return fmt.Errorf("run `%s`: %s", strings.Join(args, " "), text)
		}
		return fmt.Errorf("run `%s`: %w", strings.Join(args, " "), err)
	}
	return nil
}

func cliAvailable() bool {
	cmd := exec.Command("git", "memento", "--version")
	return cmd.Run() == nil
}

func detectInstallCommand() (string, bool) {
	if runtime.GOOS == "windows" {
		return "", false
	}
	if _, err := exec.LookPath("curl"); err == nil {
		return fmt.Sprintf("curl -fsSL %s | sh", installScriptURL), true
	}
	if _, err := exec.LookPath("wget"); err == nil {
		return fmt.Sprintf("wget -qO- %s | sh", installScriptURL), true
	}
	return "", false
}

func promptForInstall(stdin io.Reader, stdout io.Writer) (bool, error) {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}

	fmt.Fprint(stdout, "Install git-memento now? [Y/n]: ")
	reader := bufio.NewReader(stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read install confirmation: %w", err)
	}

	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "" || answer == "y" || answer == "yes", nil
}

func runInstallCommand(command string, stdout, stderr io.Writer) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func addInstalledPathToEnv() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}

	binDir := filepath.Join(homeDir, ".local", "bin")
	binaryPath := filepath.Join(binDir, "git-memento")
	if _, err := os.Stat(binaryPath); err != nil {
		return
	}

	pathEnv := os.Getenv("PATH")
	for _, entry := range filepath.SplitList(pathEnv) {
		if entry == binDir {
			return
		}
	}
	if pathEnv == "" {
		_ = os.Setenv("PATH", binDir)
		return
	}
	_ = os.Setenv("PATH", binDir+string(os.PathListSeparator)+pathEnv)
}
