package memento

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	projectURL         = "https://github.com/mandel-macaque/memento"
	releaseTag         = "1.2.0-8543d652"
	releaseURL         = projectURL + "/releases/tag/" + releaseTag
	releaseDownloadURL = projectURL + "/releases/download/" + releaseTag
)

type releaseAsset struct {
	name   string
	sha256 string
}

type installPlan struct {
	assetName   string
	assetSHA256 string
	assetURL    string
	installDir  string
	installPath string
	releaseURL  string
}

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
	ReleaseURL     string
	Message        string
}

// EnsureCLI checks whether git-memento is available and optionally offers to install it.
func EnsureCLI(opts EnsureCLIOptions) (*EnsureCLIResult, error) {
	result := &EnsureCLIResult{ProjectURL: projectURL, ReleaseURL: releaseURL}
	stdout, stderr := resolveInstallWriters(opts)

	localInstallPath, addedToPath := addInstalledPathToEnv()
	if cliAvailable() {
		result.Available = true
		switch {
		case addedToPath:
			result.Message = fmt.Sprintf("git-memento is already installed at %s and was added to PATH", localInstallPath)
		case localInstallPath != "":
			result.Message = fmt.Sprintf("git-memento is already installed at %s", localInstallPath)
		default:
			result.Message = "git-memento is already installed"
		}
		return result, nil
	}

	plan, ok := detectInstallPlan()
	if !ok {
		result.Message = "git-memento is not installed and Plexium could not find a supported installer"
		return result, nil
	}

	fmt.Fprintln(stdout, "git-memento is not installed.")
	fmt.Fprintf(stdout, "Plexium can install git-memento from the pinned GitHub release %s:\n", releaseTag)
	fmt.Fprintf(stdout, "  %s\n\n", plan.releaseURL)

	confirmed, err := promptForInstall(opts.Stdin, stdout)
	if err != nil {
		return nil, err
	}
	if !confirmed {
		result.Message = "git-memento installation skipped"
		return result, nil
	}

	if err := runInstallPlan(*plan, stdout, stderr); err != nil {
		result.Message = "git-memento installation failed"
		return result, err
	}

	localInstallPath, addedToPath = addInstalledPathToEnv()
	if !cliAvailable() {
		result.Message = "git-memento installer finished, but the command is still unavailable on PATH"
		return result, fmt.Errorf("git-memento is still unavailable after installation")
	}

	result.Available = true
	result.Installed = true
	if addedToPath {
		result.Message = fmt.Sprintf("git-memento installed successfully and added to PATH from %s", localInstallPath)
	} else {
		result.Message = "git-memento installed successfully"
	}
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

// ConfiguredProvider returns the currently configured git-memento provider, if any.
func ConfiguredProvider(repoRoot string) (string, error) {
	cmd := exec.Command("git", "config", "--local", "--get", "memento.provider")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return "", nil
	}
	return "", fmt.Errorf("read memento.provider: %w", err)
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

// ConfigureClaudeShim installs the repo-local Claude compatibility shim config.
func ConfigureClaudeShim(repoRoot string) error {
	bridgePath := filepath.Join(repoRoot, ".plexium", "bin", "claude-memento-bridge.cjs")
	if err := os.MkdirAll(filepath.Dir(bridgePath), 0o755); err != nil {
		return fmt.Errorf("create shim directory: %w", err)
	}
	if err := os.WriteFile(bridgePath, []byte(claudeBridgeScript), 0o755); err != nil {
		return fmt.Errorf("write Claude compatibility bridge: %w", err)
	}

	cmd := exec.Command("git", "config", "--local", "memento.claude.bin", bridgePath)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text != "" {
			return fmt.Errorf("configure Claude compatibility bridge: %s", text)
		}
		return fmt.Errorf("configure Claude compatibility bridge: %w", err)
	}
	return nil
}

func cliAvailable() bool {
	cmd := exec.Command("git", "memento", "--version")
	return cmd.Run() == nil
}

func detectInstallPlan() (*installPlan, bool) {
	asset, ok := releaseAssetForPlatform()
	if !ok {
		return nil, false
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, false
	}

	binaryName := "git-memento"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	installDir := filepath.Join(homeDir, ".local", "bin")
	return &installPlan{
		assetName:   asset.name,
		assetSHA256: asset.sha256,
		assetURL:    fmt.Sprintf("%s/%s", releaseDownloadURL, asset.name),
		installDir:  installDir,
		installPath: filepath.Join(installDir, binaryName),
		releaseURL:  releaseURL,
	}, true
}

func releaseAssetForPlatform() (releaseAsset, bool) {
	switch {
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return releaseAsset{name: "git-memento-osx-arm64.tar.gz", sha256: "0baad28c2d8b71efc0ce7947578418abe1fad2e4017c8ed1423ec7f28a19d4e1"}, true
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		return releaseAsset{name: "git-memento-osx-x64.tar.gz", sha256: "3fb61e8e0e5c75bdbcd10788bbce6fe0c15f4ce530a2d27859cf36cc82132c1c"}, true
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		return releaseAsset{name: "git-memento-linux-x64.tar.gz", sha256: "75bdc2802ef562f7915497ab68226771365d12053c482c572d93d0f27ebe00a9"}, true
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		return releaseAsset{name: "git-memento-win-x64.zip", sha256: "8934c3f66dca8d469629a958bb20165abdb488ce2520587f137a3d6ff2edbb70"}, true
	default:
		return releaseAsset{}, false
	}
}

func resolveInstallWriters(opts EnsureCLIOptions) (io.Writer, io.Writer) {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	return stdout, stderr
}

func promptForInstall(stdin io.Reader, stdout io.Writer) (bool, error) {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}

	fmt.Fprint(stdout, "Install git-memento now? [y/N]: ")
	reader := bufio.NewReader(stdin)
	answer, err := reader.ReadString('\n')
	if err == io.EOF && answer == "" {
		return false, nil
	}
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read install confirmation: %w", err)
	}

	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes", nil
}

func runInstallPlan(plan installPlan, stdout, stderr io.Writer) error {
	tmpDir, err := os.MkdirTemp("", "plexium-memento-install-*")
	if err != nil {
		return fmt.Errorf("create install temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, plan.assetName)
	if err := downloadReleaseAsset(plan.assetURL, archivePath); err != nil {
		return fmt.Errorf("download git-memento release asset from %s: %w", plan.assetURL, err)
	}
	if err := verifyAssetChecksum(archivePath, plan.assetSHA256); err != nil {
		return fmt.Errorf("verify git-memento release asset from %s (expected release %s): %w", plan.releaseURL, releaseTag, err)
	}

	binaryPath, err := extractBinary(archivePath, tmpDir)
	if err != nil {
		return fmt.Errorf("extract git-memento release asset: %w", err)
	}
	if err := installBinary(binaryPath, plan.installDir, plan.installPath); err != nil {
		return fmt.Errorf("install git-memento into %s: %w", plan.installDir, err)
	}

	fmt.Fprintf(stdout, "Installed git-memento to %s\n", plan.installDir)
	if !pathContainsDir(plan.installDir) {
		fmt.Fprintf(stderr, "%s is not currently in your PATH.\n", plan.installDir)
		if runtime.GOOS == "windows" {
			fmt.Fprintf(stderr, "Add it for this PowerShell session:\n  $env:Path = \"%s;$env:Path\"\n", plan.installDir)
			fmt.Fprintf(stderr, "Temporary cmd.exe session alternative:\n  set PATH=%s;%%PATH%%\n", plan.installDir)
			fmt.Fprintf(stderr, "Persist it for future sessions:\n  setx PATH \"%%PATH%%;%s\"\n", plan.installDir)
		} else {
			fmt.Fprintf(stderr, "Add it for this shell session:\n  export PATH=\"%s:$PATH\"\n", plan.installDir)
		}
	}
	return nil
}

func downloadReleaseAsset(url, destination string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", response.Status)
	}

	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, response.Body)
	return err
}

func verifyAssetChecksum(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}

	actual := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch: got %s, expected %s", actual, expected)
	}
	return nil
}

func extractBinary(archivePath, tmpDir string) (string, error) {
	switch {
	case strings.HasSuffix(archivePath, ".tar.gz"):
		return extractTarGzBinary(archivePath, tmpDir)
	case strings.HasSuffix(archivePath, ".zip"):
		return extractZipBinary(archivePath, tmpDir)
	default:
		return "", fmt.Errorf("unsupported archive format %s", filepath.Base(archivePath))
	}
}

func extractTarGzBinary(archivePath, tmpDir string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gzipReader.Close()

	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(header.Name) != "git-memento" {
			continue
		}

		outputPath := filepath.Join(tmpDir, "git-memento")
		output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(output, reader); err != nil {
			output.Close()
			return "", err
		}
		if err := output.Close(); err != nil {
			return "", err
		}
		return outputPath, nil
	}

	return "", fmt.Errorf("git-memento binary not found in archive")
}

func extractZipBinary(archivePath, tmpDir string) (string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	for _, file := range reader.File {
		if filepath.Base(file.Name) != "git-memento.exe" {
			continue
		}
		source, err := file.Open()
		if err != nil {
			return "", err
		}

		outputPath := filepath.Join(tmpDir, "git-memento.exe")
		output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			source.Close()
			return "", err
		}
		if _, err := io.Copy(output, source); err != nil {
			source.Close()
			output.Close()
			return "", err
		}
		if err := source.Close(); err != nil {
			output.Close()
			return "", err
		}
		if err := output.Close(); err != nil {
			return "", err
		}
		return outputPath, nil
	}

	return "", fmt.Errorf("git-memento executable not found in archive")
}

func installBinary(sourcePath, installDir, installPath string) error {
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return err
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	tempPath := installPath + ".tmp"
	destination, err := os.OpenFile(tempPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(destination, source); err != nil {
		destination.Close()
		return err
	}
	if err := destination.Close(); err != nil {
		return err
	}

	return os.Rename(tempPath, installPath)
}

func addInstalledPathToEnv() (string, bool) {
	binaryPath, ok := localBinaryPath()
	if !ok {
		return "", false
	}

	binDir := filepath.Dir(binaryPath)
	if pathContainsDir(binDir) {
		return binaryPath, false
	}

	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		_ = os.Setenv("PATH", binDir)
		return binaryPath, true
	}
	_ = os.Setenv("PATH", binDir+string(os.PathListSeparator)+pathEnv)
	return binaryPath, true
}

func localBinaryPath() (string, bool) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}

	binaryName := "git-memento"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	binaryPath := filepath.Join(homeDir, ".local", "bin", binaryName)
	info, err := os.Stat(binaryPath)
	if err != nil || info.IsDir() {
		return "", false
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", false
	}
	return binaryPath, true
}

func pathContainsDir(target string) bool {
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry == target {
			return true
		}
	}
	return false
}
